package friendsvc

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"
)

type personView struct {
	authorView
	Bio       string `json:"bio"`
	Followers int    `json:"followers"`
	Following int    `json:"following"`
	Posts     int    `json:"posts"`
	Followed  bool   `json:"followed"`
	Blocked   bool   `json:"blocked"`
	Mine      bool   `json:"mine"`
	CreatedAt int64  `json:"created_at"`
}

// blockedEitherWay reports whether a block exists between two people.
func (s *Service) blockedEitherWay(ctx context.Context, a, b string) (bool, error) {
	var one int
	err := s.config.Store.db.QueryRowContext(ctx,
		`SELECT 1 FROM blocks WHERE (blocker_id = ?1 AND blocked_id = ?2) OR (blocker_id = ?2 AND blocked_id = ?1)`, a, b).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

// personFor loads a person the viewer may deal with. A person who blocked the
// viewer, or who is hidden, does not exist for them. Someone the viewer
// blocked is still returned (so the block can be lifted) unless forAction.
func (s *Service) personFor(ctx context.Context, me personRecord, ref string, forAction bool) (personRecord, error) {
	target, err := s.config.Store.personByID(ctx, ref)
	if errors.Is(err, errNotFound) {
		target, err = scanPerson(s.config.Store.db.QueryRowContext(ctx, `SELECT `+personColumns+` FROM people WHERE handle = ?`, strings.ToLower(strings.TrimPrefix(ref, "@"))))
	}
	if err != nil {
		return personRecord{}, err
	}
	if target.ID == me.ID {
		return target, nil
	}
	if target.Hidden {
		return personRecord{}, errNotFound
	}
	var theyBlockedMe, iBlocked int
	if err := s.config.Store.db.QueryRowContext(ctx,
		`SELECT EXISTS (SELECT 1 FROM blocks WHERE blocker_id = ?2 AND blocked_id = ?1),
		        EXISTS (SELECT 1 FROM blocks WHERE blocker_id = ?1 AND blocked_id = ?2)`, me.ID, target.ID).Scan(&theyBlockedMe, &iBlocked); err != nil {
		return personRecord{}, err
	}
	if theyBlockedMe != 0 || (forAction && iBlocked != 0) {
		return personRecord{}, errNotFound
	}
	return target, nil
}

func (s *Service) personView(ctx context.Context, me, p personRecord) (personView, error) {
	v := personView{authorView: authorView{p.ID, p.Handle, p.AgentName, p.PetID}, Bio: p.Bio, Mine: p.ID == me.ID, CreatedAt: p.CreatedAt}
	var followed, blocked int
	err := s.config.Store.db.QueryRowContext(ctx,
		`SELECT (SELECT COUNT(*) FROM follows WHERE followee_id = ?2),
		        (SELECT COUNT(*) FROM follows WHERE follower_id = ?2),
		        (SELECT COUNT(*) FROM posts WHERE person_id = ?2 AND hidden = 0),
		        EXISTS (SELECT 1 FROM follows WHERE follower_id = ?1 AND followee_id = ?2),
		        EXISTS (SELECT 1 FROM blocks WHERE blocker_id = ?1 AND blocked_id = ?2)`, me.ID, p.ID).
		Scan(&v.Followers, &v.Following, &v.Posts, &followed, &blocked)
	v.Followed, v.Blocked = followed != 0, blocked != 0
	return v, err
}

func (s *Service) handleGetPerson(w http.ResponseWriter, r *http.Request, me personRecord) {
	target, err := s.personFor(r.Context(), me, r.PathValue("id"), false)
	if s.notFoundOrFail(w, "person", err) {
		return
	}
	view, err := s.personView(r.Context(), me, target)
	if err != nil {
		s.internalError(w, "person view", err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (s *Service) handlePersonPosts(w http.ResponseWriter, r *http.Request, me personRecord) {
	target, err := s.personFor(r.Context(), me, r.PathValue("id"), false)
	if s.notFoundOrFail(w, "person", err) {
		return
	}
	posts, next, err := s.listPosts(r.Context(), me, `p.person_id = ?2`, []any{target.ID}, r.URL.Query().Get("before"))
	if err != nil {
		s.internalError(w, "person posts", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"posts": posts, "next": next})
}

func (s *Service) handleFollow(w http.ResponseWriter, r *http.Request, me personRecord) {
	if !s.follows.Allow("f:"+me.ID, s.now()) {
		writeError(w, http.StatusTooManyRequests, "slow down")
		return
	}
	target, err := s.personFor(r.Context(), me, r.PathValue("id"), true)
	if s.notFoundOrFail(w, "person", err) {
		return
	}
	if target.ID == me.ID {
		writeError(w, http.StatusBadRequest, "cannot_follow_self")
		return
	}
	result, err := s.config.Store.db.ExecContext(r.Context(), `INSERT OR IGNORE INTO follows (follower_id, followee_id, created_at) VALUES (?, ?, ?)`, me.ID, target.ID, s.now().UnixMilli())
	if err != nil {
		s.internalError(w, "follow", err)
		return
	}
	if n, _ := result.RowsAffected(); n > 0 {
		s.notify(r.Context(), target.ID, "follow", me.ID, "")
	}
	s.writePerson(w, r, me, target.ID)
}

func (s *Service) handleUnfollow(w http.ResponseWriter, r *http.Request, me personRecord) {
	target, err := s.personFor(r.Context(), me, r.PathValue("id"), false)
	if s.notFoundOrFail(w, "person", err) {
		return
	}
	if _, err := s.config.Store.db.ExecContext(r.Context(), `DELETE FROM follows WHERE follower_id = ? AND followee_id = ?`, me.ID, target.ID); err != nil {
		s.internalError(w, "unfollow", err)
		return
	}
	s.writePerson(w, r, me, target.ID)
}

func (s *Service) writePerson(w http.ResponseWriter, r *http.Request, me personRecord, id string) {
	target, err := s.config.Store.personByID(r.Context(), id)
	if s.notFoundOrFail(w, "person", err) {
		return
	}
	view, err := s.personView(r.Context(), me, target)
	if err != nil {
		s.internalError(w, "person view", err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

// handleBlock hides both people from each other and drops any follow between
// them, both ways.
func (s *Service) handleBlock(w http.ResponseWriter, r *http.Request, me personRecord) {
	target, err := s.personFor(r.Context(), me, r.PathValue("id"), false)
	if s.notFoundOrFail(w, "person", err) {
		return
	}
	if target.ID == me.ID {
		writeError(w, http.StatusBadRequest, "cannot_block_self")
		return
	}
	tx, err := s.config.Store.db.BeginTx(r.Context(), nil)
	if err != nil {
		s.internalError(w, "block", err)
		return
	}
	defer tx.Rollback()
	for _, statement := range []string{
		`INSERT OR IGNORE INTO blocks (blocker_id, blocked_id, created_at) VALUES (?1, ?2, ?3)`,
		`DELETE FROM follows WHERE (follower_id = ?1 AND followee_id = ?2) OR (follower_id = ?2 AND followee_id = ?1)`,
	} {
		if _, err := tx.ExecContext(r.Context(), statement, me.ID, target.ID, s.now().UnixMilli()); err != nil {
			s.internalError(w, "block", err)
			return
		}
	}
	if err := tx.Commit(); err != nil {
		s.internalError(w, "block", err)
		return
	}
	s.writePerson(w, r, me, target.ID)
}

func (s *Service) handleUnblock(w http.ResponseWriter, r *http.Request, me personRecord) {
	target, err := s.config.Store.personByID(r.Context(), r.PathValue("id"))
	if s.notFoundOrFail(w, "person", err) {
		return
	}
	if _, err := s.config.Store.db.ExecContext(r.Context(), `DELETE FROM blocks WHERE blocker_id = ? AND blocked_id = ?`, me.ID, target.ID); err != nil {
		s.internalError(w, "unblock", err)
		return
	}
	s.writePerson(w, r, me, target.ID)
}

func (s *Service) handleBlocks(w http.ResponseWriter, r *http.Request, me personRecord) {
	rows, err := s.config.Store.db.QueryContext(r.Context(),
		`SELECT a.id, a.handle, a.agent_name, a.pet_id FROM blocks b JOIN people a ON a.id = b.blocked_id WHERE b.blocker_id = ? ORDER BY b.created_at DESC`, me.ID)
	if err != nil {
		s.internalError(w, "blocks", err)
		return
	}
	defer rows.Close()
	people := []authorView{}
	for rows.Next() {
		var v authorView
		if err := rows.Scan(&v.ID, &v.Handle, &v.AgentName, &v.PetID); err != nil {
			s.internalError(w, "blocks", err)
			return
		}
		people = append(people, v)
	}
	writeJSON(w, http.StatusOK, map[string]any{"people": people})
}

// ---- reports --------------------------------------------------------------

func (s *Service) handleReport(w http.ResponseWriter, r *http.Request, me personRecord) {
	if !s.reports.Allow("x:"+me.ID, s.now()) {
		writeError(w, http.StatusTooManyRequests, "slow down")
		return
	}
	var request struct {
		Kind     string `json:"kind"`
		TargetID string `json:"target_id"`
		Reason   string `json:"reason"`
	}
	if !decodeJSON(w, r, &request) {
		return
	}
	table, ok := map[string]string{"post": "posts", "comment": "comments", "person": "people"}[request.Kind]
	if !ok || request.TargetID == "" {
		writeError(w, http.StatusBadRequest, "invalid_report")
		return
	}
	ctx := r.Context()
	// Who owns the target, so a person cannot report themselves.
	var owner string
	ownerColumn := "person_id"
	if request.Kind == "person" {
		ownerColumn = "id"
	}
	err := s.config.Store.db.QueryRowContext(ctx, `SELECT `+ownerColumn+` FROM `+table+` WHERE id = ?`, request.TargetID).Scan(&owner)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	if err != nil {
		s.internalError(w, "report", err)
		return
	}
	if owner == me.ID {
		writeError(w, http.StatusBadRequest, "cannot_report_self")
		return
	}
	id, err := randomHex(12)
	if err != nil {
		s.internalError(w, "report id", err)
		return
	}
	if _, err := s.config.Store.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO reports (id, reporter_id, kind, target_id, reason, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		id, me.ID, request.Kind, request.TargetID, clip(strings.TrimSpace(request.Reason), 200), s.now().UnixMilli()); err != nil {
		s.internalError(w, "report", err)
		return
	}
	// Reports from different people are what count: the unique key means one
	// person reporting twice is still one.
	var count int
	if err := s.config.Store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM reports WHERE kind = ? AND target_id = ?`, request.Kind, request.TargetID).Scan(&count); err != nil {
		s.internalError(w, "report", err)
		return
	}
	if count >= reportsToHide {
		if _, err := s.config.Store.db.ExecContext(ctx, `UPDATE `+table+` SET hidden = 1 WHERE id = ?`, request.TargetID); err != nil {
			s.internalError(w, "hide", err)
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]bool{"reported": true})
}

// ---- notifications --------------------------------------------------------

type notificationView struct {
	Seq       int64      `json:"seq"`
	Kind      string     `json:"kind"`
	Actor     authorView `json:"actor"`
	PostID    string     `json:"post_id,omitempty"`
	PostText  string     `json:"post_text,omitempty"`
	CreatedAt int64      `json:"created_at"`
	Read      bool       `json:"read"`
}

func (s *Service) handleNotifications(w http.ResponseWriter, r *http.Request, me personRecord) {
	after, _ := strconv.ParseInt(r.URL.Query().Get("after"), 10, 64)
	rows, err := s.config.Store.db.QueryContext(r.Context(),
		`SELECT n.seq, n.kind, n.post_id, n.created_at, n.read_at IS NOT NULL,
		        a.id, a.handle, a.agent_name, a.pet_id, COALESCE(p.text, '')
		 FROM notifications n
		 JOIN people a ON a.id = n.actor_id
		 LEFT JOIN posts p ON p.id = n.post_id
		 WHERE n.person_id = ?1 AND n.seq > ?2
		   AND NOT EXISTS (SELECT 1 FROM blocks b WHERE (b.blocker_id = ?1 AND b.blocked_id = n.actor_id)
		        OR (b.blocker_id = n.actor_id AND b.blocked_id = ?1))
		 ORDER BY n.seq DESC LIMIT 100`, me.ID, after)
	if err != nil {
		s.internalError(w, "notifications", err)
		return
	}
	defer rows.Close()
	list := []notificationView{}
	for rows.Next() {
		var n notificationView
		var read bool
		if err := rows.Scan(&n.Seq, &n.Kind, &n.PostID, &n.CreatedAt, &read, &n.Actor.ID, &n.Actor.Handle, &n.Actor.AgentName, &n.Actor.PetID, &n.PostText); err != nil {
			s.internalError(w, "notifications", err)
			return
		}
		n.Read = read
		n.PostText = clip(n.PostText, 120)
		list = append(list, n)
	}
	view, err := s.meView(r.Context(), me)
	if err != nil {
		s.internalError(w, "notifications", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"notifications": list, "unread": view.Unread})
}

func (s *Service) handleNotificationsRead(w http.ResponseWriter, r *http.Request, me personRecord) {
	if _, err := s.config.Store.db.ExecContext(r.Context(), `UPDATE notifications SET read_at = ? WHERE person_id = ? AND read_at IS NULL`, s.now().UnixMilli(), me.ID); err != nil {
		s.internalError(w, "mark read", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
