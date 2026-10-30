package friendsvc

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/friends"
	"github.com/daoleno/mewla/daemon/reqsign"
)

func jpegBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for x := 0; x < w; x++ {
		for y := 0; y < h; y++ {
			img.Set(x, y, color.RGBA{uint8(x), uint8(y), 90, 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func pngBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, w, h))); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func box(kind string, payload ...[]byte) []byte {
	body := bytes.Join(payload, nil)
	out := make([]byte, 8, 8+len(body))
	binary.BigEndian.PutUint32(out, uint32(8+len(body)))
	copy(out[4:], kind)
	return append(out, body...)
}

// mp4Bytes is the smallest file the service takes for an mp4 of that length:
// a file type box and a movie header. The service reads no frames.
func mp4Bytes(seconds int) []byte {
	mvhd := make([]byte, 100)
	binary.BigEndian.PutUint32(mvhd[12:], 1000) // timescale
	binary.BigEndian.PutUint32(mvhd[16:], uint32(seconds*1000))
	return append(box("ftyp", []byte("isom\x00\x00\x02\x00isomiso2")), box("moov", box("mvhd", mvhd))...)
}

func digest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

type uploaded struct {
	ID         string `json:"id"`
	URL        string `json:"url"`
	DurationMS int    `json:"duration_ms"`
}

// uploadRaw sends a signed media upload whose body hash is claimedSHA.
func (u *user) uploadRaw(kind, mime string, data []byte, claimedSHA string, width, height int, posterID string) (int, []byte) {
	u.h.t.Helper()
	path := apiPrefix + "/media"
	timestamp := strconv.FormatInt(u.h.clock.Now().UnixMilli(), 10)
	nonce, _ := randomHex(16)
	signature := u.manager.CreateLinkSignature(reqsign.AccountScheme.Payload(
		"PUT", path, timestamp, nonce, friends.UploadSignedBody(claimedSHA, kind, mime, width, height, posterID)))
	request, _ := http.NewRequest("PUT", u.h.server.URL+path, bytes.NewReader(data))
	request.Header.Set("Authorization", reqsign.AccountScheme.Header(u.manager.PublicKeyHex(), timestamp, nonce, signature))
	request.Header.Set("Content-Type", mime)
	request.Header.Set("X-Mewla-Sha256", claimedSHA)
	request.Header.Set("X-Mewla-Kind", kind)
	request.Header.Set("X-Mewla-Dims", strconv.Itoa(width)+"x"+strconv.Itoa(height))
	if posterID != "" {
		request.Header.Set("X-Mewla-Poster", posterID)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		u.h.t.Fatal(err)
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(response.Body)
	return response.StatusCode, body
}

func (u *user) uploadOK(kind, mime string, data []byte, width, height int, posterID string) uploaded {
	u.h.t.Helper()
	status, raw := u.uploadRaw(kind, mime, data, digest(data), width, height, posterID)
	if status/100 != 2 {
		u.h.t.Fatalf("upload %s = %d %s", kind, status, raw)
	}
	var out uploaded
	if err := json.Unmarshal(raw, &out); err != nil {
		u.h.t.Fatal(err)
	}
	return out
}

func (u *user) uploadRefused(want int, code, kind, mime string, data []byte, width, height int, posterID string) {
	u.h.t.Helper()
	status, raw := u.uploadRaw(kind, mime, data, digest(data), width, height, posterID)
	if status != want || !strings.Contains(string(raw), code) {
		u.h.t.Fatalf("upload %s = %d %s, want %d %s", kind, status, raw, want, code)
	}
}

func (h *harness) enabled(login string) *user {
	h.t.Helper()
	u := h.signIn(login, 90*24*time.Hour)
	u.ok("POST", "/me/enable", map[string]string{"agent_name": "Mochi"}, &u.me)
	return u
}

type postWithMedia struct {
	ID    string `json:"id"`
	Text  string `json:"text"`
	Media []struct {
		Kind       string `json:"kind"`
		URL        string `json:"url"`
		Width      int    `json:"width"`
		Height     int    `json:"height"`
		DurationMS int    `json:"duration_ms"`
		Poster     *struct {
			URL    string `json:"url"`
			Width  int    `json:"width"`
			Height int    `json:"height"`
		} `json:"poster"`
	} `json:"media"`
}

func (u *user) postWith(text string, media ...string) postWithMedia {
	u.h.t.Helper()
	var post postWithMedia
	u.ok("POST", "/posts", map[string]any{"text": text, "via": "approved", "media": media}, &post)
	return post
}

func (h *harness) get(url string, header ...string) (int, []byte, http.Header) {
	h.t.Helper()
	request, _ := http.NewRequest("GET", url, nil)
	for i := 0; i+1 < len(header); i += 2 {
		request.Header.Set(header[i], header[i+1])
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		h.t.Fatal(err)
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(response.Body)
	return response.StatusCode, body, response.Header
}

func (h *harness) objects() []string {
	entries, _ := os.ReadDir(h.media.Dir)
	var names []string
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), ".") {
			names = append(names, entry.Name())
		}
	}
	return names
}

func TestImagePostsCarryMediaAndServeIt(t *testing.T) {
	h := newHarness(t)
	alice := h.enabled("alice")
	first := alice.uploadOK("image", "image/jpeg", jpegBytes(t, 64, 48), 0, 0, "")
	second := alice.uploadOK("image", "image/png", pngBytes(t, 32, 32), 0, 0, "")
	post := alice.postWith("two pictures", first.ID, second.ID)
	if len(post.Media) != 2 || post.Media[0].Kind != "image" || post.Media[0].Width != 64 || post.Media[0].Height != 48 || post.Media[1].Width != 32 {
		t.Fatalf("media = %+v", post.Media)
	}
	// Seen by someone else in the feed and in the post detail, in order.
	bob := h.enabled("bob")
	var feed struct{ Posts []postWithMedia }
	bob.ok("GET", "/feed", nil, &feed)
	if len(feed.Posts) != 1 || len(feed.Posts[0].Media) != 2 || feed.Posts[0].Media[0].URL != first.URL {
		t.Fatalf("feed = %+v", feed)
	}
	var detail struct{ Post postWithMedia }
	bob.ok("GET", "/posts/"+post.ID, nil, &detail)
	if len(detail.Post.Media) != 2 {
		t.Fatalf("detail = %+v", detail)
	}
	// The object is public by its key, typed, cacheable, and ranged.
	status, body, header := h.get(first.URL)
	if status != 200 || header.Get("Content-Type") != "image/jpeg" || !strings.Contains(header.Get("Cache-Control"), "immutable") || len(body) == 0 {
		t.Fatalf("GET media = %d %v", status, header)
	}
	if status, _, _ := h.get(strings.TrimSuffix(first.URL, ".jpg") + "x.jpg"); status != 404 {
		t.Fatalf("a made-up key answered %d", status)
	}
	if status, _, _ := h.get(h.server.URL + PathPrefix + "/media/..%2F..%2Ffriends.db"); status != 404 {
		t.Fatalf("a path answered %d", status)
	}
}

func TestVideoPostNeedsItsPosterAndTakesLengthFromTheFile(t *testing.T) {
	h := newHarness(t)
	alice := h.enabled("alice")
	video := mp4Bytes(14)
	alice.uploadRefused(422, "invalid_poster", "video", "video/mp4", video, 720, 720, "")
	poster := alice.uploadOK("poster", "image/jpeg", jpegBytes(t, 90, 90), 0, 0, "")
	clip := alice.uploadOK("video", "video/mp4", video, 720, 720, poster.ID)
	if clip.DurationMS != 14000 {
		t.Fatalf("duration = %d", clip.DurationMS)
	}
	// The poster is not a post attachment by itself.
	alice.expect(400, "POST", "/posts", map[string]any{"text": "x", "via": "approved", "media": []string{poster.ID}})
	post := alice.postWith("a video", clip.ID)
	if len(post.Media) != 1 || post.Media[0].Kind != "video" || post.Media[0].Poster == nil || post.Media[0].Poster.URL != poster.URL || post.Media[0].DurationMS != 14000 {
		t.Fatalf("media = %+v", post.Media)
	}
	// Seek: ranged reads work for players.
	status, body, header := h.get(clip.URL, "Range", "bytes=0-7")
	if status != http.StatusPartialContent || len(body) != 8 || !strings.HasPrefix(header.Get("Content-Range"), "bytes 0-7/") {
		t.Fatalf("range = %d %d %v", status, len(body), header)
	}
	alice.uploadRefused(422, "video_too_long", "video", "video/mp4", mp4Bytes(90), 720, 720, alice.uploadOK("poster", "image/jpeg", jpegBytes(t, 20, 20), 0, 0, "").ID)
}

func TestUploadsAreCheckedByTheServer(t *testing.T) {
	h := newHarness(t)
	alice := h.enabled("alice")
	image := jpegBytes(t, 16, 16)
	// A hash that is not the body's.
	if status, raw := alice.uploadRaw("image", "image/jpeg", image, digest([]byte("other")), 0, 0, ""); status != 400 || !strings.Contains(string(raw), "hash_mismatch") {
		t.Fatalf("hash mismatch = %d %s", status, raw)
	}
	// Bytes that are not the kind they say.
	alice.uploadRefused(422, "unsupported_media", "image", "image/jpeg", []byte("<html>not an image at all</html>"), 0, 0, "")
	alice.uploadRefused(422, "unsupported_media", "image", "image/jpeg", mp4Bytes(3), 0, 0, "")
	alice.uploadRefused(422, "unsupported_media", "video", "video/mp4", image, 16, 16, "")
	alice.uploadRefused(422, "unsupported_media", "poster", "image/jpeg", pngBytes(t, 8, 8), 0, 0, "")
	// A body over the kind's limit is refused from its length alone.
	big := append(jpegBytes(t, 16, 16), make([]byte, maxImageBytes)...)
	if status, raw := alice.uploadRaw("image", "image/jpeg", big, digest(big), 0, 0, ""); status != 413 {
		t.Fatalf("big image = %d %s", status, raw)
	}
	// Unsigned or someone else's signature.
	request, _ := http.NewRequest("PUT", h.server.URL+apiPrefix+"/media", bytes.NewReader(image))
	request.Header.Set("X-Mewla-Sha256", digest(image))
	request.Header.Set("X-Mewla-Kind", "image")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 401 {
		t.Fatalf("unsigned = %d", response.StatusCode)
	}
	// A signed claim cannot be reused for another kind or another body.
	if status, _ := alice.uploadRaw("poster", "image/jpeg", image, digest(image), 0, 0, ""); status != 201 {
		t.Fatalf("poster = %d", status)
	}
	if got := len(h.objects()); got != 1 {
		t.Fatalf("refused uploads left %d objects, want only the poster", got)
	}
	// Friends must be on.
	stranger := h.signIn("carol", 90*24*time.Hour)
	if status, _ := stranger.uploadRaw("image", "image/jpeg", image, digest(image), 0, 0, ""); status != 404 {
		t.Fatalf("not enabled = %d", status)
	}
}

func TestAPostTakesOnlyItsOwnersUnusedUploadsInTheAllowedShapes(t *testing.T) {
	h := newHarness(t)
	alice, bob := h.enabled("alice"), h.enabled("bob")
	img := func(u *user, n int) uploaded {
		return u.uploadOK("image", "image/jpeg", jpegBytes(t, 8+n, 8), 0, 0, "")
	}
	poster := alice.uploadOK("poster", "image/jpeg", jpegBytes(t, 9, 9), 0, 0, "")
	clip := alice.uploadOK("video", "video/mp4", mp4Bytes(5), 100, 100, poster.ID)
	var five []string
	for i := 0; i < 5; i++ {
		five = append(five, img(alice, i).ID)
	}
	bad := func(media ...string) {
		t.Helper()
		alice.expect(400, "POST", "/posts", map[string]any{"text": "x", "via": "approved", "media": media})
	}
	bad(five...)                        // five images
	bad(five[0], clip.ID)               // an image and a video
	bad(clip.ID, five[0])               // a video and an image
	bad(five[0], five[0])               // the same one twice
	bad("0123456789abcdef")             // unknown
	bad(img(bob, 20).ID)                // someone else's
	alice.postWith("four", five[:4]...) // allowed
	bad(five[0])                        // already used
	alice.postWith("clip", clip.ID)
	// No media is a text post with no media field at all.
	var raw map[string]json.RawMessage
	alice.ok("POST", "/posts", map[string]any{"text": "text only", "via": "approved"}, &raw)
	if _, has := raw["media"]; has {
		t.Fatalf("a text post carries media: %s", raw["media"])
	}
}

func TestDeletingAPostRemovesItsMediaForGood(t *testing.T) {
	h := newHarness(t)
	alice := h.enabled("alice")
	poster := alice.uploadOK("poster", "image/jpeg", jpegBytes(t, 9, 9), 0, 0, "")
	clip := alice.uploadOK("video", "video/mp4", mp4Bytes(5), 100, 100, poster.ID)
	keep := alice.uploadOK("image", "image/jpeg", jpegBytes(t, 12, 12), 0, 0, "")
	gone := alice.postWith("with a clip", clip.ID)
	kept := alice.postWith("with a photo", keep.ID)
	if len(h.objects()) != 3 {
		t.Fatalf("objects = %v", h.objects())
	}
	alice.ok("DELETE", "/posts/"+gone.ID, nil, nil)
	if names := h.objects(); len(names) != 1 || !strings.HasPrefix(keep.URL[strings.LastIndex(keep.URL, "/")+1:], strings.TrimSuffix(names[0], ".jpg")) {
		t.Fatalf("after delete the objects are %v, want only the other post's photo", names)
	}
	for _, url := range []string{clip.URL, poster.URL} {
		if status, _, _ := h.get(url); status != 404 {
			t.Fatalf("%s answers %d after its post was deleted", url, status)
		}
	}
	if status, _, _ := h.get(keep.URL); status != 200 {
		t.Fatalf("another post's media answers %d", status)
	}
	var left int
	h.store.DB().QueryRow(`SELECT COUNT(*) FROM media_trash`).Scan(&left)
	if left != 0 {
		t.Fatalf("%d objects still queued", left)
	}
	_ = kept
}

func TestTurningFriendsOffRemovesEveryMedia(t *testing.T) {
	h := newHarness(t)
	alice, bob := h.enabled("alice"), h.enabled("bob")
	one := alice.uploadOK("image", "image/jpeg", jpegBytes(t, 10, 10), 0, 0, "")
	alice.postWith("posted", one.ID)
	unused := alice.uploadOK("image", "image/png", pngBytes(t, 10, 10), 0, 0, "")
	theirs := bob.uploadOK("image", "image/jpeg", jpegBytes(t, 11, 11), 0, 0, "")
	bob.postWith("bob's", theirs.ID)
	alice.ok("DELETE", "/me", nil, nil)
	for _, url := range []string{one.URL, unused.URL} {
		if status, _, _ := h.get(url); status != 404 {
			t.Fatalf("%s answers %d after the person was deleted", url, status)
		}
	}
	if status, _, _ := h.get(theirs.URL); status != 200 {
		t.Fatalf("someone else's media answers %d", status)
	}
	if names := h.objects(); len(names) != 1 {
		t.Fatalf("objects = %v, want only bob's", names)
	}
	var rows int
	h.store.DB().QueryRow(`SELECT COUNT(*) FROM media WHERE person_id NOT IN (SELECT id FROM people)`).Scan(&rows)
	if rows != 0 {
		t.Fatalf("%d media rows outlived their person", rows)
	}
}

// failingStore refuses deletes until told otherwise.
type failingStore struct {
	*DirMediaStore
	mu   sync.Mutex
	fail bool
}

func (f *failingStore) Delete(ctx context.Context, key string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail {
		return errors.New("object store is down")
	}
	return f.DirMediaStore.Delete(ctx, key)
}

func TestAnObjectThatCannotBeDeletedIsRetriedAndStaleUploadsAreReaped(t *testing.T) {
	h := newHarness(t)
	store := &failingStore{DirMediaStore: h.media, fail: true}
	h.service.config.Media = store
	alice := h.enabled("alice")
	posted := alice.uploadOK("image", "image/jpeg", jpegBytes(t, 10, 10), 0, 0, "")
	post := alice.postWith("x", posted.ID)
	stale := alice.uploadOK("image", "image/png", pngBytes(t, 10, 10), 0, 0, "")
	alice.ok("DELETE", "/posts/"+post.ID, nil, nil)
	if got := len(h.objects()); got != 2 {
		t.Fatalf("objects = %d while the store is down, want 2", got)
	}
	if status, _, _ := h.get(posted.URL); status != 404 {
		t.Fatalf("a deleted post's media is still public (%d) while its object waits", status)
	}
	// A day passes: the upload nobody used is reaped, and the store recovers.
	h.clock.Add(25 * time.Hour)
	store.fail = false
	h.service.Sweep(context.Background())
	if names := h.objects(); len(names) != 0 {
		t.Fatalf("objects = %v after the sweep", names)
	}
	if status, _, _ := h.get(stale.URL); status != 404 {
		t.Fatalf("a stale upload answers %d", status)
	}
}

func TestAutoPostsCarryMediaOnlyWhenTheOwnerAllowedIt(t *testing.T) {
	h := newHarness(t)
	alice := h.enabled("alice")
	// What v0.2.77 sends: no media field.
	alice.ok("PUT", "/me/auto-post", map[string]any{"enabled": true, "tz": "UTC"}, &alice.me)
	if !alice.me.AutoPost || alice.me.AutoMedia {
		t.Fatalf("me = %+v", alice.me)
	}
	one := alice.uploadOK("image", "image/jpeg", jpegBytes(t, 10, 10), 0, 0, "")
	body := map[string]any{"text": "auto", "via": "auto", "media": []string{one.ID}}
	if raw := alice.expect(403, "POST", "/posts", body); !strings.Contains(string(raw), "auto_media_off") {
		t.Fatalf("answer = %s", raw)
	}
	alice.expect(201, "POST", "/posts", map[string]any{"text": "auto text", "via": "auto"})
	// Turning auto-post off and on from an old client does not touch the choice.
	alice.ok("PUT", "/me/auto-post", map[string]any{"media": true}, &alice.me)
	alice.ok("PUT", "/me/auto-post", map[string]any{"enabled": false}, &alice.me)
	alice.ok("PUT", "/me/auto-post", map[string]any{"enabled": true}, &alice.me)
	if !alice.me.AutoPost || !alice.me.AutoMedia {
		t.Fatalf("me = %+v", alice.me)
	}
	alice.expect(201, "POST", "/posts", body)
}

func TestMediaUploadsAreRateLimitedPerDay(t *testing.T) {
	h := newHarness(t)
	alice := h.enabled("alice")
	for i := 0; i < maxUploadsPerDay; i++ {
		alice.uploadOK("image", "image/jpeg", jpegBytes(t, 8, 8+i%50), i, 0, "")
	}
	alice.uploadRefused(429, "daily_media_cap", "image", "image/png", pngBytes(t, 7, 7), 0, 0, "")
	h.clock.Add(25 * time.Hour)
	alice.uploadOK("image", "image/png", pngBytes(t, 7, 7), 0, 0, "")
}

func TestRetriedUploadReturnsTheSameObject(t *testing.T) {
	h := newHarness(t)
	alice := h.enabled("alice")
	data := jpegBytes(t, 20, 20)
	first := alice.uploadOK("image", "image/jpeg", data, 0, 0, "")
	again := alice.uploadOK("image", "image/jpeg", data, 0, 0, "")
	if first.ID != again.ID || len(h.objects()) != 1 {
		t.Fatalf("a retry made a second object: %v %v", first, again)
	}
}

func TestMediaMigrationAddsTheOptInToAnOlderDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	store, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().Exec(`ALTER TABLE people DROP COLUMN auto_media`); err != nil {
		t.Fatal(err)
	}
	store.Close()
	store, err = OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := store.DB().Exec(`INSERT INTO people (id, account_id, handle, created_at) VALUES ('p', 'a', 'old-user', 1)`); err != nil {
		t.Fatal(err)
	}
	person, err := store.personByID(context.Background(), "p")
	if err != nil || person.AutoMedia {
		t.Fatalf("person = %+v, %v", person, err)
	}
}
