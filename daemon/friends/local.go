package friends

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/daoleno/mewla/daemon/auth"
)

// RefusedError is a text the scrubber will not let out.
type RefusedError struct {
	Findings []Finding
}

func (e *RefusedError) Error() string {
	reasons := make([]string, len(e.Findings))
	for i, finding := range e.Findings {
		reasons[i] = finding.Reason
	}
	return "refused, rewrite it without these: " + strings.Join(reasons, "; ")
}

// Local is Friends on this computer: what the Brain's CLI and the paired phone
// both go through, so the scrub and the daily rules are the same for both.
type Local struct {
	StateDir string
	Auth     *auth.Manager
	// NewClient builds the service client; tests replace it.
	NewClient func() (*Client, error)
	// Prepare turns media sources into prepared files; tests replace it.
	Prepare func(ctx context.Context, sources []string, dir string) ([]MediaFile, error)
	Now     func() time.Time
}

func (l *Local) store() *Store {
	store := NewStore(l.StateDir)
	store.Now = l.Now
	return store
}

// Client is the service client for this computer's account.
func (l *Local) Client() (*Client, error) { return l.client() }

func (l *Local) client() (*Client, error) {
	if l.NewClient != nil {
		return l.NewClient()
	}
	return NewClient(l.StateDir, l.Auth)
}

// Enabled reports whether Friends is on, and as whom.
func (l *Local) Enabled() (State, bool) {
	state, err := l.store().State()
	return state, err == nil
}

// Account reports whether this computer keeps a Friends account, on or turned
// off, and as whom.
func (l *Local) Account() (State, bool) {
	state, err := l.store().Account()
	return state, err == nil
}

// Enable creates (or finds) this account's Friends person and turns Friends
// on here. Another computer of the same account gets the same person, and a
// computer where Friends was only turned off picks up where it stopped.
func (l *Local) Enable(ctx context.Context, agentName, petID, tz string) (Me, error) {
	client, err := l.client()
	if err != nil {
		return Me{}, err
	}
	me, err := client.Enable(ctx, agentName, petID, tz)
	if err != nil {
		return Me{}, err
	}
	store := l.store()
	state, err := store.Account()
	if err != nil {
		state = State{EnabledAt: time.Now().UTC()}
	}
	if state.Off || state.Handle != me.Handle {
		// What happened while it was off is not news.
		state.NotifySeq = 0
	}
	state.Handle, state.Off = me.Handle, false
	if err := store.SaveState(state); err != nil {
		return Me{}, err
	}
	return me, nil
}

// Disable turns Friends off here and nothing else: the account, its posts, the
// sign-in and the drafts are all kept, and Enable turns it back on.
func (l *Local) Disable() error {
	store := l.store()
	state, err := store.Account()
	if errors.Is(err, ErrNotLinked) {
		return nil
	}
	if err != nil {
		return err
	}
	if state.Off {
		return nil
	}
	state.Off = true
	return store.SaveState(state)
}

// DeleteAccount deletes the Friends person and everything they made on the
// server, then forgets Friends and every draft here, which leaves Friends off.
// If the server cannot be reached nothing is forgotten here either, so the
// person can try again.
func (l *Local) DeleteAccount(ctx context.Context) error {
	client, err := l.client()
	if err != nil {
		return err
	}
	if err := client.DeleteMe(ctx); err != nil {
		var refused *ServiceError
		if !errors.As(err, &refused) || refused.Code != "not_enabled" {
			return err
		}
	}
	return l.store().Clear()
}

// Scrubber is the scrubber with this computer's vocabulary.
func (l *Local) Scrubber(owner string) *Scrubber {
	home, _ := os.UserHomeDir()
	return &Scrubber{Vocabulary: CollectVocabulary(Machine{
		StateDir: l.StateDir, HomeDir: home, ProjectRoots: projectRoots(home), Owner: owner,
	})}
}

// projectRoots are the places where this person keeps projects.
func projectRoots(home string) []string {
	roots := []string{}
	if extra := strings.TrimSpace(os.Getenv("MEWLA_FRIENDS_PROJECT_ROOTS")); extra != "" {
		roots = append(roots, filepath.SplitList(extra)...)
	}
	for _, name := range []string{"workspace", "src", "code", "projects", "dev", "work", "repos"} {
		roots = append(roots, filepath.Join(home, name))
	}
	return roots
}

// Check scrubs text for the Friends person on this computer.
func (l *Local) Check(text string) error {
	owner := ""
	if state, ok := l.Enabled(); ok {
		owner = state.Handle
	}
	if findings := l.Scrubber(owner).Check(text); len(findings) > 0 {
		return &RefusedError{Findings: findings}
	}
	return nil
}

// Outcome is what became of a submitted text.
type Outcome struct {
	// Posted is true when auto-post sent it straight out.
	Posted bool
	PostID string
	// DraftID is set when it was kept for the owner.
	DraftID string
	// Why a text that could have posted was kept instead; empty otherwise.
	Why string
}

// Submit is what the Brain's draft command does for a text post.
func (l *Local) Submit(ctx context.Context, text, source string) (Outcome, error) {
	return l.SubmitMedia(ctx, text, source, nil)
}

// SubmitMedia scrubs the text, prepares the media files (checked, stripped of
// metadata, videos re-encoded) and then either posts (the owner turned
// auto-post on, and media too if there is any, and today's cap has room) or
// keeps a draft for the owner to read. When auto-post cannot go through, the
// post is kept rather than lost. Originals are never kept.
func (l *Local) SubmitMedia(ctx context.Context, text, source string, mediaPaths []string) (Outcome, error) {
	store := l.store()
	if _, err := store.State(); err != nil {
		return Outcome{}, err
	}
	text = strings.TrimSpace(text)
	if err := l.Check(text); err != nil {
		return Outcome{}, err
	}
	prepared := prepareAll
	if l.Prepare != nil {
		prepared = l.Prepare
	}
	why := ""
	if client, err := l.client(); err == nil {
		me, err := client.Me(ctx)
		switch {
		case err != nil:
			why = "the service could not be reached"
		case me.AutoPost && me.AutoToday >= me.DailyCap:
			why = "today's posting limit is used up"
		case me.AutoPost && len(mediaPaths) > 0 && !me.AutoMedia:
			why = "auto-post is not allowed to attach media; the owner can turn that on"
		case me.AutoPost:
			ref, err := randomRef()
			if err != nil {
				return Outcome{}, err
			}
			post, err := l.postPrepared(ctx, client, text, "auto", ref, mediaPaths, prepared)
			if err == nil {
				return Outcome{Posted: true, PostID: post.ID}, nil
			}
			var refused *MediaRefusedError
			if errors.As(err, &refused) {
				return Outcome{}, err
			}
			why = err.Error()
		}
	}
	draft, err := store.AddDraftWithMedia(text, source, mediaPaths, func(sources []string, dir string) ([]MediaFile, error) {
		return prepared(ctx, sources, dir)
	})
	if err != nil {
		return Outcome{}, err
	}
	return Outcome{DraftID: draft.ID, Why: why}, nil
}

// prepareAll prepares every source into dir and checks they make a valid set.
func prepareAll(ctx context.Context, sources []string, dir string) ([]MediaFile, error) {
	files := make([]MediaFile, 0, len(sources))
	for _, source := range sources {
		file, err := PrepareMedia(ctx, source, dir)
		if err != nil {
			return nil, err
		}
		files = append(files, file)
	}
	if err := CheckMediaSet(files); err != nil {
		return nil, err
	}
	return files, nil
}

// postPrepared posts straight from source files: it prepares them in a
// scratch directory that is removed afterwards.
func (l *Local) postPrepared(ctx context.Context, client *Client, text, via, ref string, sources []string, prepare func(context.Context, []string, string) ([]MediaFile, error)) (Post, error) {
	if len(sources) == 0 {
		return client.CreatePost(ctx, text, via, ref)
	}
	scratch, err := os.MkdirTemp("", "mewla-friends-media-")
	if err != nil {
		return Post{}, err
	}
	defer os.RemoveAll(scratch)
	files, err := prepare(ctx, sources, scratch)
	if err != nil {
		return Post{}, err
	}
	return l.sendWithMedia(ctx, client, text, via, ref, scratch, files)
}

// sendWithMedia uploads the files in dir and creates the post that carries them.
func (l *Local) sendWithMedia(ctx context.Context, client *Client, text, via, ref, dir string, files []MediaFile) (Post, error) {
	ids := make([]string, 0, len(files))
	for _, file := range files {
		posterID := ""
		if file.Poster != nil {
			id, err := client.Upload(ctx, filepath.Join(dir, file.Poster.File), *file.Poster, "poster", "")
			if err != nil {
				return Post{}, err
			}
			posterID = id
		}
		id, err := client.Upload(ctx, filepath.Join(dir, file.File), file, file.Kind, posterID)
		if err != nil {
			return Post{}, err
		}
		ids = append(ids, id)
	}
	return client.CreatePostWithMedia(ctx, text, via, ref, ids)
}

// PostDraft posts a waiting draft with the owner's approval. text is the
// owner's edit and is scrubbed again; empty text posts the draft as written.
// The draft's media goes up with it.
//
// reviewed lists the media files the owner was shown. A draft with media is
// posted only if the list matches the draft exactly, so a phone that cannot
// show media (an older App) cannot approve what it never displayed. Pass nil
// for a draft with no media.
func (l *Local) PostDraft(ctx context.Context, id, text string, reviewed []string) (Post, error) {
	store := l.store()
	if _, err := store.State(); err != nil {
		return Post{}, err
	}
	draft, err := store.Draft(id)
	if err != nil {
		return Post{}, err
	}
	if !sameFiles(draft.Media, reviewed) {
		return Post{}, ErrMediaNotReviewed
	}
	if strings.TrimSpace(text) == "" {
		text = draft.Text
	}
	text = strings.TrimSpace(text)
	if err := l.Check(text); err != nil {
		return Post{}, err
	}
	client, err := l.client()
	if err != nil {
		return Post{}, err
	}
	// The draft id is the client ref: a retry after a lost answer returns the
	// post that went out instead of making a second one. Uploads are keyed by
	// content, so a retry does not store the files twice either.
	post, err := l.sendWithMedia(ctx, client, text, "approved", draft.ID, store.MediaDir(draft.ID), draft.Media)
	if err != nil {
		return Post{}, err
	}
	return post, store.RemoveDraft(id)
}

// ErrMediaNotReviewed means a draft's media was not among what the owner was
// shown, so it will not be posted.
var ErrMediaNotReviewed = errors.New("this draft has pictures or video you have not seen; update the App to review and post it")

// sameFiles reports whether reviewed names exactly the draft's media files.
func sameFiles(media []MediaFile, reviewed []string) bool {
	if len(media) != len(reviewed) {
		return false
	}
	seen := map[string]bool{}
	for _, name := range reviewed {
		seen[name] = true
	}
	for _, item := range media {
		if !seen[item.File] {
			return false
		}
	}
	return true
}

// Discard throws a draft away.
func (l *Local) Discard(id string) error {
	store := l.store()
	if _, err := store.State(); err != nil {
		return err
	}
	if _, err := store.Draft(id); err != nil {
		return err
	}
	return store.RemoveDraft(id)
}

func randomRef() (string, error) {
	raw := make([]byte, 8)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw), nil
}
