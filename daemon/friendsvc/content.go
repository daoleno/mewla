package friendsvc

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/daoleno/mewla/daemon/friends"
)

// canSee is the SQL for "viewer ?1 may see this post": not hidden, author not
// hidden, no block either way. A person always sees their own posts.
const canSee = `(p.person_id = ?1 OR (p.hidden = 0 AND a.hidden = 0 AND NOT EXISTS (
	SELECT 1 FROM blocks b WHERE (b.blocker_id = ?1 AND b.blocked_id = p.person_id)
		OR (b.blocker_id = p.person_id AND b.blocked_id = ?1))))`

const postSelect = `SELECT p.id, p.person_id, p.text, p.via, p.created_at, a.handle, a.agent_name, a.pet_id,
	(SELECT COUNT(*) FROM likes l WHERE l.post_id = p.id),
	(SELECT COUNT(*) FROM comments c WHERE c.post_id = p.id AND c.hidden = 0),
	EXISTS (SELECT 1 FROM likes l WHERE l.post_id = p.id AND l.person_id = ?1),
	EXISTS (SELECT 1 FROM bookmarks k WHERE k.post_id = p.id AND k.person_id = ?1)
	FROM posts p JOIN people a ON a.id = p.person_id`

type authorView struct {
	ID        string `json:"id"`
	Handle    string `json:"handle"`
	AgentName string `json:"agent_name"`
	PetID     string `json:"pet_id"`
}

type postView struct {
	ID         string     `json:"id"`
	Text       string     `json:"text"`
	Via        string     `json:"via"`
	CreatedAt  int64      `json:"created_at"`
	Author     authorView `json:"author"`
	Likes      int        `json:"likes"`
	Comments   int        `json:"comments"`
	Liked      bool       `json:"liked"`
	Bookmarked bool       `json:"bookmarked"`
	Mine       bool       `json:"mine"`
}

func scanPost(row interface{ Scan(...any) error }, me string) (postView, error) {
	var v postView
	var liked, bookmarked int
	err := row.Scan(&v.ID, &v.Author.ID, &v.Text, &v.Via, &v.CreatedAt, &v.Author.Handle, &v.Author.AgentName, &v.Author.PetID,
		&v.Likes, &v.Comments, &liked, &bookmarked)
	if errors.Is(err, sql.ErrNoRows) {
		return postView{}, errNotFound
	}
	v.Liked, v.Bookmarked, v.Mine = liked != 0, bookmarked != 0, v.Author.ID == me
	return v, err
}

// visiblePost loads a post the viewer may see.
func (s *Service) visiblePost(ctx context.Context, me personRecord, id string) (postView, error) {
	return scanPost(s.config.Store.db.QueryRowContext(ctx, postSelect+` WHERE p.id = ?2 AND `+canSee, me.ID, id), me.ID)
}

func (s *Service) listPosts(ctx context.Context, me personRecord, where string, args []any, cursor string) ([]postView, string, error) {
	query := postSelect + ` WHERE ` + canSee + ` AND ` + where
	all := append([]any{me.ID}, args...)
	if at, id, ok := parseCursor(cursor); ok {
		query += fmt.Sprintf(` AND (p.created_at < ?%d OR (p.created_at = ?%d AND p.id < ?%d))`, len(all)+1, len(all)+1, len(all)+2)
		all = append(all, at, id)
	}
	query += fmt.Sprintf(` ORDER BY p.created_at DESC, p.id DESC LIMIT %d`, pageSize+1)
	rows, err := s.config.Store.db.QueryContext(ctx, query, all...)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()
	posts := []postView{}
	for rows.Next() {
		v, err := scanPost(rows, me.ID)
		if err != nil {
			return nil, "", err
		}
		posts = append(posts, v)
	}
	if err := rows.Err(); err != nil {
		return nil, "", err
	}
	next := ""
	if len(posts) > pageSize {
		posts = posts[:pageSize]
		last := posts[len(posts)-1]
		next = strconv.FormatInt(last.CreatedAt, 10) + "_" + last.ID
	}
	return posts, next, nil
}

func parseCursor(cursor string) (int64, string, bool) {
	at, id, ok := strings.Cut(cursor, "_")
	if !ok {
		return 0, "", false
	}
	n, err := strconv.ParseInt(at, 10, 64)
	return n, id, err == nil && id != ""
}

func (s *Service) handleFeed(w http.ResponseWriter, r *http.Request, me personRecord) {
	where, args := "1 = 1", []any(nil)
	if r.URL.Query().Get("scope") == "following" {
		where = `p.person_id IN (SELECT followee_id FROM follows WHERE follower_id = ?1)`
	}
	posts, next, err := s.listPosts(r.Context(), me, where, args, r.URL.Query().Get("before"))
	if err != nil {
		s.internalError(w, "feed", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"posts": posts, "next": next})
}

func (s *Service) handleBookmarks(w http.ResponseWriter, r *http.Request, me personRecord) {
	posts, next, err := s.listPosts(r.Context(), me, `p.id IN (SELECT post_id FROM bookmarks WHERE person_id = ?1)`, nil, r.URL.Query().Get("before"))
	if err != nil {
		s.internalError(w, "bookmarks", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"posts": posts, "next": next})
}

// ---- posting --------------------------------------------------------------

// dayStart is the start of the person's local day, falling back to UTC.
func (s *Service) dayStart(p personRecord) int64 {
	loc := time.UTC
	if p.TZ != "" {
		if l, err := time.LoadLocation(p.TZ); err == nil {
			loc = l
		}
	}
	now := s.now().In(loc)
	return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc).UnixMilli()
}

func (s *Service) countToday(ctx context.Context, p personRecord, autoOnly bool) (int, error) {
	query := `SELECT COUNT(*) FROM posts WHERE person_id = ? AND created_at >= ?`
	if autoOnly {
		query += ` AND via = 'auto'`
	}
	var n int
	err := s.config.Store.db.QueryRowContext(ctx, query, p.ID, s.dayStart(p)).Scan(&n)
	return n, err
}

func (s *Service) handleCreatePost(w http.ResponseWriter, r *http.Request, me personRecord) {
	var request struct {
		Text      string `json:"text"`
		Via       string `json:"via"`
		ClientRef string `json:"client_ref"`
	}
	if !decodeJSON(w, r, &request) {
		return
	}
	text := strings.TrimSpace(request.Text)
	if request.Via != "approved" && request.Via != "auto" {
		writeError(w, http.StatusBadRequest, "invalid_via")
		return
	}
	if text == "" || len([]rune(text)) > maxPostRunes || len(request.ClientRef) > 80 {
		writeError(w, http.StatusBadRequest, "invalid_text")
		return
	}
	ctx := r.Context()
	// A retried request returns the post it already made.
	if request.ClientRef != "" {
		var id string
		err := s.config.Store.db.QueryRowContext(ctx, `SELECT id FROM posts WHERE person_id = ? AND client_ref = ?`, me.ID, request.ClientRef).Scan(&id)
		if err == nil {
			existing, err := s.visiblePost(ctx, me, id)
			if err != nil {
				s.internalError(w, "load post", err)
				return
			}
			writeJSON(w, http.StatusOK, existing)
			return
		}
		if !errors.Is(err, sql.ErrNoRows) {
			s.internalError(w, "client ref", err)
			return
		}
	}
	if findings := friends.CheckBasic(text); len(findings) > 0 {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"error": "text_refused", "kinds": findingKinds(findings)})
		return
	}
	if request.Via == "auto" {
		if !me.AutoPost {
			writeError(w, http.StatusForbidden, "auto_post_off")
			return
		}
		n, err := s.countToday(ctx, me, true)
		if err != nil {
			s.internalError(w, "count", err)
			return
		}
		if n >= me.DailyCap {
			writeError(w, http.StatusTooManyRequests, "daily_cap")
			return
		}
	}
	total, err := s.countToday(ctx, me, false)
	if err != nil {
		s.internalError(w, "count", err)
		return
	}
	if total >= hardDailyPosts {
		writeError(w, http.StatusTooManyRequests, "daily_cap")
		return
	}
	id, err := randomHex(12)
	if err != nil {
		s.internalError(w, "post id", err)
		return
	}
	if _, err := s.config.Store.db.ExecContext(ctx,
		`INSERT INTO posts (id, person_id, text, via, client_ref, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		id, me.ID, text, request.Via, request.ClientRef, s.now().UnixMilli()); err != nil {
		s.internalError(w, "insert post", err)
		return
	}
	created, err := s.visiblePost(ctx, me, id)
	if err != nil {
		s.internalError(w, "load post", err)
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

func findingKinds(findings []friends.Finding) []string {
	kinds := make([]string, 0, len(findings))
	for _, f := range findings {
		kinds = append(kinds, f.Kind)
	}
	return kinds
}

type commentView struct {
	ID        string     `json:"id"`
	Text      string     `json:"text"`
	CreatedAt int64      `json:"created_at"`
	Author    authorView `json:"author"`
	Mine      bool       `json:"mine"`
}

func (s *Service) handleGetPost(w http.ResponseWriter, r *http.Request, me personRecord) {
	ctx := r.Context()
	post, err := s.visiblePost(ctx, me, r.PathValue("id"))
	if s.notFoundOrFail(w, "post", err) {
		return
	}
	rows, err := s.config.Store.db.QueryContext(ctx,
		`SELECT c.id, c.text, c.created_at, a.id, a.handle, a.agent_name, a.pet_id
		 FROM comments c JOIN people a ON a.id = c.person_id
		 WHERE c.post_id = ?2 AND (c.person_id = ?1 OR (c.hidden = 0 AND a.hidden = 0 AND NOT EXISTS (
			SELECT 1 FROM blocks b WHERE (b.blocker_id = ?1 AND b.blocked_id = c.person_id)
				OR (b.blocker_id = c.person_id AND b.blocked_id = ?1))))
		 ORDER BY c.created_at, c.id LIMIT 200`, me.ID, post.ID)
	if err != nil {
		s.internalError(w, "comments", err)
		return
	}
	defer rows.Close()
	comments := []commentView{}
	for rows.Next() {
		var c commentView
		if err := rows.Scan(&c.ID, &c.Text, &c.CreatedAt, &c.Author.ID, &c.Author.Handle, &c.Author.AgentName, &c.Author.PetID); err != nil {
			s.internalError(w, "comments", err)
			return
		}
		c.Mine = c.Author.ID == me.ID
		comments = append(comments, c)
	}
	writeJSON(w, http.StatusOK, map[string]any{"post": post, "comments": comments})
}

// notFoundOrFail answers 404 or 500 for a non-nil err and reports whether it did.
func (s *Service) notFoundOrFail(w http.ResponseWriter, what string, err error) bool {
	switch {
	case err == nil:
		return false
	case errors.Is(err, errNotFound):
		writeError(w, http.StatusNotFound, "not_found")
	default:
		s.internalError(w, what, err)
	}
	return true
}

func (s *Service) handleDeletePost(w http.ResponseWriter, r *http.Request, me personRecord) {
	result, err := s.config.Store.db.ExecContext(r.Context(), `DELETE FROM posts WHERE id = ? AND person_id = ?`, r.PathValue("id"), me.ID)
	if err != nil {
		s.internalError(w, "delete post", err)
		return
	}
	if n, _ := result.RowsAffected(); n == 0 {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	_, _ = s.config.Store.db.ExecContext(r.Context(), `DELETE FROM reports WHERE kind = 'post' AND target_id = ?`, r.PathValue("id"))
	_, _ = s.config.Store.db.ExecContext(r.Context(), `DELETE FROM notifications WHERE post_id = ?`, r.PathValue("id"))
	writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
}

// notify records something for a person. Acting on your own things is silent.
func (s *Service) notify(ctx context.Context, to, kind, actor, postID string) {
	if to == actor {
		return
	}
	if _, err := s.config.Store.db.ExecContext(ctx,
		`INSERT INTO notifications (person_id, kind, actor_id, post_id, created_at) VALUES (?, ?, ?, ?, ?)`,
		to, kind, actor, postID, s.now().UnixMilli()); err != nil {
		s.config.Logf("friends: notify failed: %v", err)
	}
}

func (s *Service) handleLike(w http.ResponseWriter, r *http.Request, me personRecord) {
	if !s.likes.Allow("l:"+me.ID, s.now()) {
		writeError(w, http.StatusTooManyRequests, "slow down")
		return
	}
	post, err := s.visiblePost(r.Context(), me, r.PathValue("id"))
	if s.notFoundOrFail(w, "post", err) {
		return
	}
	result, err := s.config.Store.db.ExecContext(r.Context(), `INSERT OR IGNORE INTO likes (post_id, person_id, created_at) VALUES (?, ?, ?)`, post.ID, me.ID, s.now().UnixMilli())
	if err != nil {
		s.internalError(w, "like", err)
		return
	}
	if n, _ := result.RowsAffected(); n > 0 {
		s.notify(r.Context(), post.Author.ID, "like", me.ID, post.ID)
	}
	s.writePost(w, r, me, post.ID)
}

func (s *Service) handleUnlike(w http.ResponseWriter, r *http.Request, me personRecord) {
	post, err := s.visiblePost(r.Context(), me, r.PathValue("id"))
	if s.notFoundOrFail(w, "post", err) {
		return
	}
	if _, err := s.config.Store.db.ExecContext(r.Context(), `DELETE FROM likes WHERE post_id = ? AND person_id = ?`, post.ID, me.ID); err != nil {
		s.internalError(w, "unlike", err)
		return
	}
	s.writePost(w, r, me, post.ID)
}

func (s *Service) handleBookmark(w http.ResponseWriter, r *http.Request, me personRecord) {
	post, err := s.visiblePost(r.Context(), me, r.PathValue("id"))
	if s.notFoundOrFail(w, "post", err) {
		return
	}
	if _, err := s.config.Store.db.ExecContext(r.Context(), `INSERT OR IGNORE INTO bookmarks (person_id, post_id, created_at) VALUES (?, ?, ?)`, me.ID, post.ID, s.now().UnixMilli()); err != nil {
		s.internalError(w, "bookmark", err)
		return
	}
	s.writePost(w, r, me, post.ID)
}

func (s *Service) handleUnbookmark(w http.ResponseWriter, r *http.Request, me personRecord) {
	// Removing a bookmark works even if the post is gone from view.
	if _, err := s.config.Store.db.ExecContext(r.Context(), `DELETE FROM bookmarks WHERE person_id = ? AND post_id = ?`, me.ID, r.PathValue("id")); err != nil {
		s.internalError(w, "unbookmark", err)
		return
	}
	s.writePost(w, r, me, r.PathValue("id"))
}

func (s *Service) writePost(w http.ResponseWriter, r *http.Request, me personRecord, id string) {
	post, err := s.visiblePost(r.Context(), me, id)
	if errors.Is(err, errNotFound) {
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
		return
	}
	if err != nil {
		s.internalError(w, "load post", err)
		return
	}
	writeJSON(w, http.StatusOK, post)
}

func (s *Service) handleComment(w http.ResponseWriter, r *http.Request, me personRecord) {
	if !s.comments.Allow("c:"+me.ID, s.now()) {
		writeError(w, http.StatusTooManyRequests, "slow down")
		return
	}
	var request struct {
		Text string `json:"text"`
	}
	if !decodeJSON(w, r, &request) {
		return
	}
	text := strings.TrimSpace(request.Text)
	if text == "" || len([]rune(text)) > maxCommentRunes {
		writeError(w, http.StatusBadRequest, "invalid_text")
		return
	}
	if findings := friends.CheckBasic(text); len(findings) > 0 {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"error": "text_refused", "kinds": findingKinds(findings)})
		return
	}
	post, err := s.visiblePost(r.Context(), me, r.PathValue("id"))
	if s.notFoundOrFail(w, "post", err) {
		return
	}
	id, err := randomHex(12)
	if err != nil {
		s.internalError(w, "comment id", err)
		return
	}
	if _, err := s.config.Store.db.ExecContext(r.Context(),
		`INSERT INTO comments (id, post_id, person_id, text, created_at) VALUES (?, ?, ?, ?, ?)`,
		id, post.ID, me.ID, text, s.now().UnixMilli()); err != nil {
		s.internalError(w, "insert comment", err)
		return
	}
	s.notify(r.Context(), post.Author.ID, "comment", me.ID, post.ID)
	writeJSON(w, http.StatusCreated, commentView{
		ID: id, Text: text, CreatedAt: s.now().UnixMilli(),
		Author: authorView{me.ID, me.Handle, me.AgentName, me.PetID}, Mine: true,
	})
}

// handleDeleteComment lets a comment's author, or the owner of the post under
// it, delete it.
func (s *Service) handleDeleteComment(w http.ResponseWriter, r *http.Request, me personRecord) {
	result, err := s.config.Store.db.ExecContext(r.Context(),
		`DELETE FROM comments WHERE id = ?1 AND (person_id = ?2 OR post_id IN (SELECT id FROM posts WHERE person_id = ?2))`,
		r.PathValue("id"), me.ID)
	if err != nil {
		s.internalError(w, "delete comment", err)
		return
	}
	if n, _ := result.RowsAffected(); n == 0 {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	_, _ = s.config.Store.db.ExecContext(r.Context(), `DELETE FROM reports WHERE kind = 'comment' AND target_id = ?`, r.PathValue("id"))
	writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
}
