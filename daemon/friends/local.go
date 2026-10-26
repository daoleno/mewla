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
	Now       func() time.Time
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

// Enable creates (or finds) this account's Friends person and turns Friends
// on here. Another computer of the same account gets the same person.
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
	state, err := store.State()
	if err != nil {
		state = State{EnabledAt: time.Now().UTC()}
	}
	state.Handle = me.Handle
	if err := store.SaveState(state); err != nil {
		return Me{}, err
	}
	return me, nil
}

// Disable deletes the Friends person and everything they made on the server,
// then forgets Friends and every draft here. If the server cannot be reached
// nothing is forgotten here either, so the person can try again.
func (l *Local) Disable(ctx context.Context) error {
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

// Submit is what the Brain's draft command does: scrub the text, and then
// either post it (the owner turned auto-post on and today's cap has room) or
// keep it for the owner to read. When auto-post cannot go through, the text is
// kept rather than lost.
func (l *Local) Submit(ctx context.Context, text, source string) (Outcome, error) {
	store := l.store()
	if _, err := store.State(); err != nil {
		return Outcome{}, err
	}
	text = strings.TrimSpace(text)
	if err := l.Check(text); err != nil {
		return Outcome{}, err
	}
	why := ""
	if client, err := l.client(); err == nil {
		me, err := client.Me(ctx)
		switch {
		case err != nil:
			why = "the service could not be reached"
		case me.AutoPost && me.AutoToday >= me.DailyCap:
			why = "today's posting limit is used up"
		case me.AutoPost:
			ref, err := randomRef()
			if err != nil {
				return Outcome{}, err
			}
			post, err := client.CreatePost(ctx, text, "auto", ref)
			if err == nil {
				return Outcome{Posted: true, PostID: post.ID}, nil
			}
			why = err.Error()
		}
	}
	draft, err := store.AddDraft(text, source)
	if err != nil {
		return Outcome{}, err
	}
	return Outcome{DraftID: draft.ID, Why: why}, nil
}

// PostDraft posts a waiting draft with the owner's approval. text is the
// owner's edit and is scrubbed again; empty text posts the draft as written.
func (l *Local) PostDraft(ctx context.Context, id, text string) (Post, error) {
	store := l.store()
	draft, err := store.Draft(id)
	if err != nil {
		return Post{}, err
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
	// post that went out instead of making a second one.
	post, err := client.CreatePost(ctx, text, "approved", draft.ID)
	if err != nil {
		return Post{}, err
	}
	return post, store.RemoveDraft(id)
}

// Discard throws a draft away.
func (l *Local) Discard(id string) error {
	store := l.store()
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
