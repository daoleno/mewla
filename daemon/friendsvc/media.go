package friendsvc

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/daoleno/mewla/daemon/friends"
	_ "golang.org/x/image/webp"
)

const (
	// A post carries up to maxImagesPerPost images, or one video.
	maxImagesPerPost = 4
	maxImageBytes    = 10 << 20
	maxVideoBytes    = 50 << 20
	maxPosterBytes   = 2 << 20
	// maxVideoDuration is "about 60 s": a little slack for container rounding.
	maxVideoDuration = 62 * time.Second
	maxMediaSide     = 4096
	// Uploads in any 24 hours, in count and bytes. They line up with the daily
	// post cap: hardDailyPosts posts of up to four images.
	maxUploadsPerDay     = hardDailyPosts * (maxImagesPerPost + 1)
	maxUploadBytesPerDay = 400 << 20
	// maxStoredBytes is what one person may keep in all.
	maxStoredBytes = 1 << 30
	// uploadTTL is how long an upload that never became part of a post is kept.
	uploadTTL = 24 * time.Hour
	// uploadWindow bounds the time one upload may take to arrive.
	uploadWindow = 10 * time.Minute
)

const mediaSchema = `
CREATE TABLE IF NOT EXISTS media (
	id TEXT PRIMARY KEY,
	person_id TEXT NOT NULL REFERENCES people(id) ON DELETE CASCADE,
	post_id TEXT REFERENCES posts(id) ON DELETE CASCADE,
	position INTEGER NOT NULL DEFAULT 0,
	kind TEXT NOT NULL CHECK (kind IN ('image', 'video', 'poster')),
	key TEXT NOT NULL UNIQUE,
	mime TEXT NOT NULL,
	bytes INTEGER NOT NULL,
	width INTEGER NOT NULL,
	height INTEGER NOT NULL,
	duration_ms INTEGER NOT NULL DEFAULT 0,
	poster_id TEXT NOT NULL DEFAULT '',
	sha256 TEXT NOT NULL,
	created_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS media_post ON media(post_id, position);
CREATE INDEX IF NOT EXISTS media_person ON media(person_id, created_at);
-- Every row that goes (a deleted post, person or stale upload, by any path)
-- queues its object for deletion in the same transaction, so a crash between
-- the commit and the object store never leaves a file behind forever.
CREATE TABLE IF NOT EXISTS media_trash (
	key TEXT PRIMARY KEY,
	created_at INTEGER NOT NULL
);
CREATE TRIGGER IF NOT EXISTS media_to_trash AFTER DELETE ON media
BEGIN
	INSERT OR IGNORE INTO media_trash (key, created_at) VALUES (old.key, CAST(strftime('%s', 'now') AS INTEGER) * 1000);
END;
`

type mediaRecord struct {
	ID         string
	PersonID   string
	PostID     string
	Position   int
	Kind       string
	Key        string
	Mime       string
	Bytes      int64
	Width      int
	Height     int
	DurationMS int
	PosterID   string
	SHA256     string
	CreatedAt  int64
}

const mediaColumns = `id, person_id, COALESCE(post_id, ''), position, kind, key, mime, bytes, width, height, duration_ms, poster_id, sha256, created_at`

func scanMedia(row interface{ Scan(...any) error }) (mediaRecord, error) {
	var m mediaRecord
	err := row.Scan(&m.ID, &m.PersonID, &m.PostID, &m.Position, &m.Kind, &m.Key, &m.Mime, &m.Bytes, &m.Width, &m.Height, &m.DurationMS, &m.PosterID, &m.SHA256, &m.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return mediaRecord{}, errNotFound
	}
	return m, err
}

// ---- views ----------------------------------------------------------------

type posterView struct {
	URL    string `json:"url"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

// mediaView is one image or video of a post. Clients that predate media never
// ask for it and ignore the field.
type mediaView struct {
	Kind       string      `json:"kind"`
	URL        string      `json:"url"`
	Width      int         `json:"width"`
	Height     int         `json:"height"`
	Bytes      int64       `json:"bytes"`
	DurationMS int         `json:"duration_ms,omitempty"`
	Poster     *posterView `json:"poster,omitempty"`
}

// attachMedia fills the media of posts, with one query for the whole page.
func (s *Service) attachMedia(ctx context.Context, posts []postView) error {
	if len(posts) == 0 || s.config.Media == nil {
		return nil
	}
	index := make(map[string]int, len(posts))
	args := make([]any, 0, len(posts))
	marks := make([]string, 0, len(posts))
	for i, post := range posts {
		index[post.ID] = i
		args = append(args, post.ID)
		marks = append(marks, "?")
	}
	rows, err := s.config.Store.db.QueryContext(ctx,
		`SELECT `+mediaColumns+` FROM media WHERE post_id IN (`+strings.Join(marks, ",")+`) ORDER BY post_id, position, created_at`, args...)
	if err != nil {
		return err
	}
	var records []mediaRecord
	for rows.Next() {
		m, err := scanMedia(rows)
		if err != nil {
			rows.Close()
			return err
		}
		records = append(records, m)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	posters := map[string]mediaRecord{}
	for _, m := range records {
		if m.Kind == "poster" {
			posters[m.ID] = m
		}
	}
	for _, m := range records {
		if m.Kind == "poster" {
			continue
		}
		view := mediaView{Kind: m.Kind, URL: s.config.Media.URL(m.Key), Width: m.Width, Height: m.Height, Bytes: m.Bytes, DurationMS: m.DurationMS}
		if poster, ok := posters[m.PosterID]; ok {
			view.Poster = &posterView{URL: s.config.Media.URL(poster.Key), Width: poster.Width, Height: poster.Height}
		}
		posts[index[m.PostID]].Media = append(posts[index[m.PostID]].Media, view)
	}
	return nil
}

// ---- upload ---------------------------------------------------------------

type uploadView struct {
	ID         string `json:"id"`
	Kind       string `json:"kind"`
	URL        string `json:"url"`
	Bytes      int64  `json:"bytes"`
	Width      int    `json:"width"`
	Height     int    `json:"height"`
	DurationMS int    `json:"duration_ms,omitempty"`
}

// uploadClaim is what an upload request says about its body. The signature
// covers all of it, and the hash binds the bytes, so a signed upload cannot
// be given other content or be replayed under another kind.
type uploadClaim struct {
	SHA256   string
	Kind     string
	Mime     string
	Width    int
	Height   int
	PosterID string
}

func parseClaim(r *http.Request) (uploadClaim, bool) {
	claim := uploadClaim{
		SHA256:   strings.ToLower(r.Header.Get("X-Mewla-Sha256")),
		Kind:     r.Header.Get("X-Mewla-Kind"),
		Mime:     r.Header.Get("Content-Type"),
		PosterID: r.Header.Get("X-Mewla-Poster"),
	}
	if dims := r.Header.Get("X-Mewla-Dims"); dims != "" {
		w, h, ok := strings.Cut(dims, "x")
		var err1, err2 error
		claim.Width, err1 = strconv.Atoi(w)
		claim.Height, err2 = strconv.Atoi(h)
		if !ok || err1 != nil || err2 != nil {
			return claim, false
		}
	}
	if _, err := hex.DecodeString(claim.SHA256); err != nil || len(claim.SHA256) != 64 {
		return claim, false
	}
	return claim, true
}

// signedBody is the byte string the daemon signs in place of a body it would
// not want to buffer: every header that decides what the upload is.
func (c uploadClaim) signedBody() []byte {
	return friends.UploadSignedBody(c.SHA256, c.Kind, c.Mime, c.Width, c.Height, c.PosterID)
}

func (s *Service) handleUpload(w http.ResponseWriter, r *http.Request) {
	if s.config.Media == nil {
		writeError(w, http.StatusServiceUnavailable, "media_off")
		return
	}
	claim, ok := parseClaim(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid_media")
		return
	}
	// The signature covers the claim, not the (large) body.
	result, rejection := s.verifier.AuthenticateBody(r, claim.signedBody())
	if rejection != nil {
		writeError(w, rejection.Status, rejection.Message)
		return
	}
	me, ok := s.personForKey(w, r, result.Key)
	if !ok {
		return
	}
	maxBytes := int64(maxImageBytes)
	switch claim.Kind {
	case "image":
	case "video":
		maxBytes = maxVideoBytes
	case "poster":
		maxBytes = maxPosterBytes
	default:
		writeError(w, http.StatusBadRequest, "invalid_media")
		return
	}
	if r.ContentLength <= 0 || r.ContentLength > maxBytes {
		writeError(w, http.StatusRequestEntityTooLarge, "media_too_large")
		return
	}
	ctx := r.Context()
	if refused := s.uploadAllowed(ctx, me, r.ContentLength); refused != "" {
		writeError(w, http.StatusTooManyRequests, refused)
		return
	}
	// The listeners' timeouts are sized for small JSON.
	controller := http.NewResponseController(w)
	_ = controller.SetReadDeadline(time.Now().Add(uploadWindow))
	_ = controller.SetWriteDeadline(time.Now().Add(uploadWindow + time.Minute))

	staged, err := os.CreateTemp(s.config.TempDir, "upload-*")
	if err != nil {
		s.internalError(w, "stage upload", err)
		return
	}
	defer os.Remove(staged.Name())
	defer staged.Close()
	digest := sha256.New()
	written, err := io.Copy(io.MultiWriter(staged, digest), http.MaxBytesReader(w, r.Body, maxBytes))
	if err != nil {
		writeError(w, http.StatusBadRequest, "upload_failed")
		return
	}
	if written != r.ContentLength || hex.EncodeToString(digest.Sum(nil)) != claim.SHA256 {
		writeError(w, http.StatusBadRequest, "hash_mismatch")
		return
	}
	inspected, problem := inspectMedia(staged, claim.Kind, written)
	if problem != "" {
		writeError(w, http.StatusUnprocessableEntity, problem)
		return
	}
	if claim.Kind == "video" {
		// The container's own dimensions are not parsed; the claim is bounded.
		if claim.Width < 1 || claim.Height < 1 || claim.Width > maxMediaSide || claim.Height > maxMediaSide {
			writeError(w, http.StatusUnprocessableEntity, "invalid_media")
			return
		}
		inspected.Width, inspected.Height = claim.Width, claim.Height
		poster, err := s.ownUnattached(ctx, me, claim.PosterID, "poster")
		if err != nil {
			writeError(w, http.StatusUnprocessableEntity, "invalid_poster")
			return
		}
		inspected.PosterID = poster.ID
	}
	// A retry of an upload that already arrived returns it.
	var existingID string
	err = s.config.Store.db.QueryRowContext(ctx,
		`SELECT id FROM media WHERE person_id = ? AND sha256 = ? AND kind = ? AND post_id IS NULL`, me.ID, claim.SHA256, claim.Kind).Scan(&existingID)
	if err == nil {
		existing, err := scanMedia(s.config.Store.db.QueryRowContext(ctx, `SELECT `+mediaColumns+` FROM media WHERE id = ?`, existingID))
		if err != nil {
			s.internalError(w, "load upload", err)
			return
		}
		writeJSON(w, http.StatusOK, s.uploadView(existing))
		return
	}
	if !errors.Is(err, sql.ErrNoRows) {
		s.internalError(w, "find upload", err)
		return
	}

	id, err1 := randomHex(16)
	if err1 != nil {
		s.internalError(w, "media id", err1)
		return
	}
	record := inspected
	record.ID, record.PersonID, record.Kind = id, me.ID, claim.Kind
	record.Key = id + "." + mediaExtension(record.Mime)
	record.Bytes, record.SHA256, record.CreatedAt = written, claim.SHA256, s.now().UnixMilli()
	if err := s.config.Media.Put(ctx, record.Key, record.Mime, staged.Name(), written, claim.SHA256); err != nil {
		s.internalError(w, "store media", err)
		return
	}
	if _, err := s.config.Store.db.ExecContext(ctx,
		`INSERT INTO media (id, person_id, kind, key, mime, bytes, width, height, duration_ms, poster_id, sha256, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		record.ID, record.PersonID, record.Kind, record.Key, record.Mime, record.Bytes, record.Width, record.Height,
		record.DurationMS, record.PosterID, record.SHA256, record.CreatedAt); err != nil {
		_ = s.config.Media.Delete(ctx, record.Key)
		s.internalError(w, "record media", err)
		return
	}
	writeJSON(w, http.StatusCreated, s.uploadView(record))
}

func (s *Service) uploadView(m mediaRecord) uploadView {
	return uploadView{ID: m.ID, Kind: m.Kind, URL: s.config.Media.URL(m.Key), Bytes: m.Bytes, Width: m.Width, Height: m.Height, DurationMS: m.DurationMS}
}

// uploadAllowed returns an error code when one more upload of size bytes goes
// over what a person may send in a day or keep in all.
func (s *Service) uploadAllowed(ctx context.Context, me personRecord, size int64) string {
	var count int
	var sent int64
	if err := s.config.Store.db.QueryRowContext(ctx,
		`SELECT COUNT(*), COALESCE(SUM(bytes), 0) FROM media WHERE person_id = ? AND created_at >= ?`,
		me.ID, s.now().Add(-24*time.Hour).UnixMilli()).Scan(&count, &sent); err != nil {
		return "slow down"
	}
	if count >= maxUploadsPerDay || sent+size > maxUploadBytesPerDay {
		return "daily_media_cap"
	}
	var stored int64
	if err := s.config.Store.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(bytes), 0) FROM media WHERE person_id = ?`, me.ID).Scan(&stored); err != nil || stored+size > maxStoredBytes {
		return "media_quota"
	}
	return ""
}

// ownUnattached loads an upload of this person that no post uses yet.
func (s *Service) ownUnattached(ctx context.Context, me personRecord, id, kind string) (mediaRecord, error) {
	m, err := scanMedia(s.config.Store.db.QueryRowContext(ctx,
		`SELECT `+mediaColumns+` FROM media WHERE id = ? AND person_id = ? AND post_id IS NULL AND kind = ?`, id, me.ID, kind))
	return m, err
}

// inspectMedia checks the bytes are what the kind says, reads image size and
// video length, and returns the mime type and metadata, or a refusal code.
func inspectMedia(file *os.File, kind string, size int64) (mediaRecord, string) {
	head := make([]byte, 16)
	n, _ := file.ReadAt(head, 0)
	head = head[:n]
	var record mediaRecord
	switch format := sniffFormat(head); {
	case format == "jpeg" && (kind == "image" || kind == "poster"):
		record.Mime = "image/jpeg"
	case format == "png" && kind == "image":
		record.Mime = "image/png"
	case format == "webp" && kind == "image":
		record.Mime = "image/webp"
	case format == "mp4" && kind == "video":
		record.Mime = "video/mp4"
	default:
		return record, "unsupported_media"
	}
	if kind == "video" {
		duration, err := mp4Duration(file, size)
		if err != nil || duration <= 0 {
			return record, "unsupported_media"
		}
		if duration > maxVideoDuration {
			return record, "video_too_long"
		}
		record.DurationMS = int(duration.Milliseconds())
		return record, ""
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return record, "unsupported_media"
	}
	config, _, err := image.DecodeConfig(file)
	if err != nil || config.Width < 1 || config.Height < 1 || config.Width > maxMediaSide || config.Height > maxMediaSide {
		return record, "unsupported_media"
	}
	record.Width, record.Height = config.Width, config.Height
	return record, ""
}

func sniffFormat(head []byte) string {
	switch {
	case len(head) >= 3 && head[0] == 0xFF && head[1] == 0xD8 && head[2] == 0xFF:
		return "jpeg"
	case len(head) >= 8 && string(head[:8]) == "\x89PNG\r\n\x1a\n":
		return "png"
	case len(head) >= 12 && string(head[:4]) == "RIFF" && string(head[8:12]) == "WEBP":
		return "webp"
	case len(head) >= 12 && string(head[4:8]) == "ftyp":
		return "mp4"
	}
	return ""
}

func mediaExtension(mime string) string {
	switch mime {
	case "image/png":
		return "png"
	case "image/webp":
		return "webp"
	case "video/mp4":
		return "mp4"
	}
	return "jpg"
}

// mp4Duration reads the movie header's duration from an ISO base media file.
func mp4Duration(file *os.File, size int64) (time.Duration, error) {
	moov, moovEnd, err := findBox(file, 0, size, "moov")
	if err != nil {
		return 0, err
	}
	header, _, err := findBox(file, moov, moovEnd, "mvhd")
	if err != nil {
		return 0, err
	}
	raw := make([]byte, 32)
	n, _ := file.ReadAt(raw, header)
	raw = raw[:n]
	var timescale, duration uint64
	switch {
	case len(raw) >= 20 && raw[0] == 0:
		timescale = uint64(binary.BigEndian.Uint32(raw[12:16]))
		duration = uint64(binary.BigEndian.Uint32(raw[16:20]))
	case len(raw) >= 32 && raw[0] == 1:
		timescale = uint64(binary.BigEndian.Uint32(raw[20:24]))
		duration = binary.BigEndian.Uint64(raw[24:32])
	default:
		return 0, errors.New("unreadable movie header")
	}
	if timescale == 0 {
		return 0, errors.New("no timescale")
	}
	return time.Duration(float64(duration) / float64(timescale) * float64(time.Second)), nil
}

// findBox scans the boxes in [start, end) for kind and returns the offset and
// end of its payload.
func findBox(file *os.File, start, end int64, kind string) (payload, payloadEnd int64, err error) {
	for offset := start; offset+8 <= end; {
		header := make([]byte, 16)
		n, _ := file.ReadAt(header, offset)
		if n < 8 {
			break
		}
		length := int64(binary.BigEndian.Uint32(header[:4]))
		headerLen := int64(8)
		switch length {
		case 0:
			length = end - offset
		case 1:
			if n < 16 {
				return 0, 0, errors.New("short box")
			}
			length = int64(binary.BigEndian.Uint64(header[8:16]))
			headerLen = 16
		}
		if length < headerLen || offset+length > end {
			return 0, 0, errors.New("bad box size")
		}
		if string(header[4:8]) == kind {
			return offset + headerLen, offset + length, nil
		}
		offset += length
	}
	return 0, 0, fmt.Errorf("no %s box", kind)
}

// ---- attaching to a post --------------------------------------------------

type attachment struct {
	media  []mediaRecord
	poster mediaRecord
}

// resolveAttachments checks the ids a post names: all the person's own
// unattached uploads, either one to four images or exactly one video.
func (s *Service) resolveAttachments(ctx context.Context, me personRecord, ids []string) (attachment, string) {
	var out attachment
	if len(ids) == 0 {
		return out, ""
	}
	if s.config.Media == nil {
		return out, "media_off"
	}
	seen := map[string]bool{}
	for _, id := range ids {
		if seen[id] {
			return out, "invalid_media"
		}
		seen[id] = true
		m, err := scanMedia(s.config.Store.db.QueryRowContext(ctx,
			`SELECT `+mediaColumns+` FROM media WHERE id = ? AND person_id = ? AND post_id IS NULL AND kind IN ('image', 'video')`, id, me.ID))
		if err != nil {
			return out, "invalid_media"
		}
		out.media = append(out.media, m)
	}
	switch out.media[0].Kind {
	case "video":
		if len(out.media) != 1 {
			return out, "invalid_media"
		}
		poster, err := s.ownUnattached(ctx, me, out.media[0].PosterID, "poster")
		if err != nil {
			return out, "invalid_media"
		}
		out.poster = poster
	default:
		if len(out.media) > maxImagesPerPost {
			return out, "invalid_media"
		}
		for _, m := range out.media {
			if m.Kind != "image" {
				return out, "invalid_media"
			}
		}
	}
	return out, ""
}

// attach ties the uploads to the post inside the post's transaction.
func (a attachment) attach(ctx context.Context, tx *sql.Tx, postID string) error {
	for i, m := range a.media {
		result, err := tx.ExecContext(ctx, `UPDATE media SET post_id = ?, position = ? WHERE id = ? AND post_id IS NULL`, postID, i, m.ID)
		if err != nil {
			return err
		}
		if n, _ := result.RowsAffected(); n != 1 {
			return errors.New("media is already used")
		}
	}
	if a.poster.ID != "" {
		if _, err := tx.ExecContext(ctx, `UPDATE media SET post_id = ? WHERE id = ? AND post_id IS NULL`, postID, a.poster.ID); err != nil {
			return err
		}
	}
	return nil
}

// ---- reading and removing -------------------------------------------------

// handleMedia serves a stored object when this service holds the bytes itself.
// A key whose row is gone answers 404 even if a file is still waiting to be
// deleted.
func (s *Service) handleMedia(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	if s.config.Media == nil || !validMediaKey(key) {
		http.NotFound(w, r)
		return
	}
	var mime string
	if err := s.config.Store.db.QueryRowContext(r.Context(), `SELECT mime FROM media WHERE key = ?`, key).Scan(&mime); err != nil {
		http.NotFound(w, r)
		return
	}
	file, err := s.config.Media.Open(key)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", mime)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	http.ServeContent(w, r, key, info.ModTime(), file)
}

// Sweep deletes the objects of media that are gone from the database, and
// the uploads no post ever used. It is safe to call at any time.
func (s *Service) Sweep(ctx context.Context) {
	if s.config.Media == nil {
		return
	}
	if _, err := s.config.Store.db.ExecContext(ctx,
		`DELETE FROM media WHERE post_id IS NULL AND created_at < ?`, s.now().Add(-uploadTTL).UnixMilli()); err != nil {
		s.config.Logf("friends: sweep uploads failed: %v", err)
	}
	s.drainTrash(ctx)
}

// drainTrash deletes queued objects. One that cannot be deleted stays queued
// for the next sweep.
func (s *Service) drainTrash(ctx context.Context) {
	if s.config.Media == nil {
		return
	}
	rows, err := s.config.Store.db.QueryContext(ctx, `SELECT key FROM media_trash ORDER BY created_at LIMIT 200`)
	if err != nil {
		s.config.Logf("friends: read media trash failed: %v", err)
		return
	}
	var keys []string
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err == nil {
			keys = append(keys, key)
		}
	}
	rows.Close()
	for _, key := range keys {
		if err := s.config.Media.Delete(ctx, key); err != nil {
			s.config.Logf("friends: delete media %s failed: %v", key, err)
			continue
		}
		if _, err := s.config.Store.db.ExecContext(ctx, `DELETE FROM media_trash WHERE key = ?`, key); err != nil {
			s.config.Logf("friends: clear media trash failed: %v", err)
		}
	}
}

// RunMaintenance sweeps now and then every ten minutes until ctx ends.
func (s *Service) RunMaintenance(ctx context.Context) {
	s.Sweep(ctx)
	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.Sweep(ctx)
		}
	}
}
