package friends_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/daoleno/mewla/daemon/friends"
)

// jpegWithEXIF is a w x h JPEG carrying an EXIF block with an orientation, a
// GPS-looking string and a camera name, followed by a comment segment.
func jpegWithEXIF(t *testing.T, w, h, orientation int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for x := 0; x < w; x++ {
		for y := 0; y < h; y++ {
			img.Set(x, y, color.RGBA{uint8(x * 4), uint8(y * 4), 80, 255})
		}
	}
	var plain bytes.Buffer
	if err := jpeg.Encode(&plain, img, nil); err != nil {
		t.Fatal(err)
	}
	tiff := []byte("MM\x00\x2a\x00\x00\x00\x08")
	entry := make([]byte, 12)
	binary.BigEndian.PutUint16(entry[0:], 0x0112)
	binary.BigEndian.PutUint16(entry[2:], 3)
	binary.BigEndian.PutUint32(entry[4:], 1)
	binary.BigEndian.PutUint16(entry[8:], uint16(orientation))
	tiff = append(tiff, 0, 1)
	tiff = append(tiff, entry...)
	tiff = append(tiff, 0, 0, 0, 0)
	tiff = append(tiff, []byte("GPS 48.8584N 2.2945E SecretCamera X100")...)
	app1 := append([]byte("Exif\x00\x00"), tiff...)
	segment := append([]byte{0xFF, 0xE1, byte((len(app1) + 2) >> 8), byte(len(app1) + 2)}, app1...)
	comment := []byte("COMMENT-owner-home-address")
	com := append([]byte{0xFF, 0xFE, byte((len(comment) + 2) >> 8), byte(len(comment) + 2)}, comment...)
	out := append([]byte{}, plain.Bytes()[:2]...)
	out = append(out, segment...)
	out = append(out, com...)
	return append(out, plain.Bytes()[2:]...)
}

func writeFile(t *testing.T, dir, name string, data []byte) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestPrepareImageDropsEXIFAndAppliesTheOrientation(t *testing.T) {
	source := t.TempDir()
	original := jpegWithEXIF(t, 60, 40, 6) // stored sideways: should come out 40 x 60
	if !bytes.Contains(original, []byte("SecretCamera")) {
		t.Fatal("fixture has no EXIF")
	}
	out := t.TempDir()
	file, err := friends.PrepareMedia(context.Background(), writeFile(t, source, "photo.jpg", original), out)
	if err != nil {
		t.Fatal(err)
	}
	if file.Kind != "image" || file.Mime != "image/jpeg" || file.Width != 40 || file.Height != 60 {
		t.Fatalf("file = %+v", file)
	}
	data, err := os.ReadFile(filepath.Join(out, file.File))
	if err != nil {
		t.Fatal(err)
	}
	for _, leaked := range []string{"Exif", "SecretCamera", "GPS", "COMMENT", "owner-home"} {
		if bytes.Contains(data, []byte(leaked)) {
			t.Errorf("prepared image still holds %q", leaked)
		}
	}
	if _, err := jpeg.Decode(bytes.NewReader(data)); err != nil {
		t.Fatalf("prepared image does not decode: %v", err)
	}
	// The name is not the owner's file name.
	if strings.Contains(file.File, "photo") {
		t.Errorf("file name %q carries the source name", file.File)
	}
}

func TestPrepareImageTrustsBytesNotNamesAndShrinksBigOnes(t *testing.T) {
	source, out := t.TempDir(), t.TempDir()
	// A PNG with a text chunk, named .jpg; large enough to be shrunk.
	img := image.NewNRGBA(image.Rect(0, 0, 3000, 100))
	for x := 0; x < 3000; x++ {
		img.Set(x, 5, color.NRGBA{200, 10, 10, 120})
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	chunk := append([]byte("tEXt"), []byte("Secret\x00hunter2")...)
	textChunk := binary.BigEndian.AppendUint32(nil, uint32(len(chunk)-4))
	textChunk = append(textChunk, chunk...)
	textChunk = binary.BigEndian.AppendUint32(textChunk, crc32.ChecksumIEEE(chunk))
	withText := bytes.Replace(buf.Bytes(), []byte("\x00\x00\x00\x00IEND"), append(textChunk, []byte("\x00\x00\x00\x00IEND")...), 1)
	if !bytes.Contains(withText, []byte("hunter2")) {
		t.Fatal("fixture has no text chunk")
	}
	file, err := friends.PrepareMedia(context.Background(), writeFile(t, source, "x.jpg", withText), out)
	if err != nil {
		t.Fatal(err)
	}
	if file.Mime != "image/png" || file.Width != 2048 {
		t.Fatalf("file = %+v", file)
	}
	data, _ := os.ReadFile(filepath.Join(out, file.File))
	if bytes.Contains(data, []byte("hunter2")) {
		t.Error("text chunk survived")
	}
}

func TestPrepareMediaRefusesWhatIsNotMedia(t *testing.T) {
	source, out := t.TempDir(), t.TempDir()
	for name, data := range map[string][]byte{
		"notes.jpg":  []byte("this is a text file named like a picture"),
		"empty.png":  nil,
		"page.html":  []byte("<html></html>"),
		"broken.jpg": append([]byte{0xFF, 0xD8, 0xFF, 0xE0}, make([]byte, 20)...),
	} {
		_, err := friends.PrepareMedia(context.Background(), writeFile(t, source, name, data), out)
		var refused *friends.MediaRefusedError
		if !errors.As(err, &refused) {
			t.Errorf("%s: err = %v, want a refusal", name, err)
		}
	}
	if _, err := friends.PrepareMedia(context.Background(), filepath.Join(source, "missing.jpg"), out); err == nil {
		t.Error("missing file accepted")
	}
	if _, err := friends.PrepareMedia(context.Background(), source, out); err == nil {
		t.Error("a directory was accepted")
	}
}

func TestCheckMediaSet(t *testing.T) {
	image, video := friends.MediaFile{Kind: "image"}, friends.MediaFile{Kind: "video"}
	for name, tc := range map[string]struct {
		files []friends.MediaFile
		ok    bool
	}{
		"none":       {nil, true},
		"one image":  {[]friends.MediaFile{image}, true},
		"four":       {[]friends.MediaFile{image, image, image, image}, true},
		"five":       {[]friends.MediaFile{image, image, image, image, image}, false},
		"one video":  {[]friends.MediaFile{video}, true},
		"two videos": {[]friends.MediaFile{video, video}, false},
		"mixed":      {[]friends.MediaFile{image, video}, false},
	} {
		if err := friends.CheckMediaSet(tc.files); (err == nil) != tc.ok {
			t.Errorf("%s: err = %v", name, err)
		}
	}
}

func ffmpegTool(t *testing.T) string {
	t.Helper()
	path, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg is not installed")
	}
	if _, err := exec.LookPath("ffprobe"); err != nil {
		t.Skip("ffprobe is not installed")
	}
	return path
}

// makeVideo writes a short clip carrying metadata that must not survive.
func makeVideo(t *testing.T, ffmpeg, dir, name string, seconds string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	cmd := exec.Command(ffmpeg, "-nostdin", "-y", "-v", "error",
		"-f", "lavfi", "-i", "testsrc=size=320x240:rate=24", "-f", "lavfi", "-i", "sine=frequency=440",
		"-t", seconds, "-c:v", "libx264", "-pix_fmt", "yuv420p", "-c:a", "aac",
		"-metadata", "title=SecretTitle", "-metadata", "location=+48.8584+002.2945/", "-metadata", "comment=owner-laptop",
		path)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("make video: %v: %s", err, output)
	}
	return path
}

func TestPrepareVideoReencodesStripsMetadataAndCutsAPoster(t *testing.T) {
	ffmpeg := ffmpegTool(t)
	source, out := t.TempDir(), t.TempDir()
	path := makeVideo(t, ffmpeg, source, "clip.mp4", "3")
	original, _ := os.ReadFile(path)
	if !bytes.Contains(original, []byte("SecretTitle")) {
		t.Skip("this ffmpeg does not write the metadata the test needs")
	}
	file, err := friends.PrepareMedia(context.Background(), path, out)
	if err != nil {
		t.Fatal(err)
	}
	if file.Kind != "video" || file.Mime != "video/mp4" || file.Width != 320 || file.Height != 240 || file.DurationMS < 2500 || file.DurationMS > 3500 || file.Poster == nil {
		t.Fatalf("file = %+v", file)
	}
	data, _ := os.ReadFile(filepath.Join(out, file.File))
	for _, leaked := range []string{"SecretTitle", "48.8584", "owner-laptop"} {
		if bytes.Contains(data, []byte(leaked)) {
			t.Errorf("prepared video still holds %q", leaked)
		}
	}
	poster, _ := os.ReadFile(filepath.Join(out, file.Poster.File))
	if _, err := jpeg.Decode(bytes.NewReader(poster)); err != nil || file.Poster.Width != 320 {
		t.Fatalf("poster = %+v, %v", file.Poster, err)
	}
	// The index is at the front so playback starts before the download ends.
	if moov, mdat := bytes.Index(data, []byte("moov")), bytes.Index(data, []byte("mdat")); moov < 0 || mdat < 0 || moov > mdat {
		t.Errorf("moov at %d, mdat at %d: not streamable", moov, mdat)
	}
}

func TestPrepareVideoRefusesLongVideosAndNonVideos(t *testing.T) {
	ffmpeg := ffmpegTool(t)
	source, out := t.TempDir(), t.TempDir()
	long := makeVideo(t, ffmpeg, source, "long.mp4", "75")
	_, err := friends.PrepareMedia(context.Background(), long, out)
	var refused *friends.MediaRefusedError
	if !errors.As(err, &refused) || !strings.Contains(err.Error(), "at most 60") {
		t.Fatalf("long video: %v", err)
	}
	// Looks like an mp4 at the front, but is not one.
	fake := writeFile(t, source, "fake.mp4", append([]byte("\x00\x00\x00\x18ftypisom\x00\x00\x02\x00isomiso2"), make([]byte, 200)...))
	if _, err := friends.PrepareMedia(context.Background(), fake, out); !errors.As(err, &refused) {
		t.Fatalf("fake mp4: %v", err)
	}
	if entries, _ := os.ReadDir(out); len(entries) != 0 {
		t.Errorf("refused videos left %d files behind", len(entries))
	}
}

func (f *fixture) enableOn(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	if _, err := f.local.Enable(context.Background(), "Mochi", "cat", "UTC"); err != nil {
		t.Fatal(err)
	}
}

func (f *fixture) draftMediaDir(id string) string {
	return filepath.Join(f.local.StateDir, "friends", "drafts", id+".media")
}

func TestDraftKeepsPreparedMediaUntilPostedOrDiscarded(t *testing.T) {
	f := newFixture(t)
	f.enableOn(t)
	ctx := context.Background()
	source := t.TempDir()
	a := writeFile(t, source, "a.jpg", jpegWithEXIF(t, 30, 20, 1))
	b := writeFile(t, source, "b.jpg", jpegWithEXIF(t, 31, 21, 1))

	out, err := f.local.SubmitMedia(ctx, "I made two pictures.", "Work: pictures", []string{a, b})
	if err != nil || out.DraftID == "" {
		t.Fatalf("outcome = %+v, %v", out, err)
	}
	store := friends.NewStore(f.local.StateDir)
	draft, err := store.Draft(out.DraftID)
	if err != nil || len(draft.Media) != 2 {
		t.Fatalf("draft = %+v, %v", draft, err)
	}
	// Prepared copies are kept; the owner's originals are untouched and not referenced.
	for _, item := range draft.Media {
		data, err := os.ReadFile(filepath.Join(f.draftMediaDir(draft.ID), item.File))
		if err != nil || bytes.Contains(data, []byte("SecretCamera")) {
			t.Fatalf("kept file %s: %v", item.File, err)
		}
	}
	if _, err := os.Stat(a); err != nil {
		t.Fatal("the original was touched")
	}

	// The owner removes one before posting; its file goes.
	removed := draft.Media[0].File
	if _, err := store.RemoveDraftMedia(draft.ID, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(f.draftMediaDir(draft.ID), removed)); !os.IsNotExist(err) {
		t.Fatalf("removed file still there: %v", err)
	}
	if _, err := store.RemoveDraftMedia(draft.ID, 5); err == nil {
		t.Fatal("removing a media that is not there succeeded")
	}

	// An App that never showed the media cannot approve it, and neither can
	// one that showed a different set.
	if _, err := f.local.PostDraft(ctx, draft.ID, "", nil); !errors.Is(err, friends.ErrMediaNotReviewed) {
		t.Fatalf("post with no review = %v", err)
	}
	if _, err := f.local.PostDraft(ctx, draft.ID, "", []string{removed}); !errors.Is(err, friends.ErrMediaNotReviewed) {
		t.Fatalf("post with the removed file reviewed = %v", err)
	}
	if _, err := os.Stat(f.draftMediaDir(draft.ID)); err != nil {
		t.Fatal("a refused post dropped the draft's media")
	}
	left, _ := store.Draft(draft.ID)
	post, err := f.local.PostDraft(ctx, draft.ID, "", []string{left.Media[0].File})
	if err != nil || post.ID == "" {
		t.Fatalf("post = %+v, %v", post, err)
	}
	if _, err := os.Stat(f.draftMediaDir(draft.ID)); !os.IsNotExist(err) {
		t.Fatalf("draft media outlived the post: %v", err)
	}
	var seen struct {
		Post struct {
			Media []struct{ Kind, URL string } `json:"media"`
		} `json:"post"`
	}
	if err := f.client.Do(ctx, "GET", "/posts/"+post.ID, nil, &seen); err != nil {
		t.Fatal(err)
	}
	if len(seen.Post.Media) != 1 || seen.Post.Media[0].Kind != "image" {
		t.Fatalf("posted media = %+v", seen.Post.Media)
	}
}

func TestDiscardingADraftRemovesItsMediaAndABadFileKeepsNothing(t *testing.T) {
	f := newFixture(t)
	f.enableOn(t)
	ctx := context.Background()
	source := t.TempDir()
	good := writeFile(t, source, "a.jpg", jpegWithEXIF(t, 30, 20, 1))
	out, err := f.local.SubmitMedia(ctx, "One picture.", "", []string{good})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.local.Discard(out.DraftID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(f.draftMediaDir(out.DraftID)); !os.IsNotExist(err) {
		t.Fatalf("discarded draft left media: %v", err)
	}

	bad := writeFile(t, source, "bad.jpg", []byte("not an image"))
	_, err = f.local.SubmitMedia(ctx, "Two files, one bad.", "", []string{good, bad})
	var refused *friends.MediaRefusedError
	if !errors.As(err, &refused) {
		t.Fatalf("err = %v", err)
	}
	if drafts, _ := friends.NewStore(f.local.StateDir).Drafts(); len(drafts) != 0 {
		t.Fatalf("a refused submit kept %d drafts", len(drafts))
	}
	entries, _ := os.ReadDir(filepath.Join(f.local.StateDir, "friends", "drafts"))
	if len(entries) != 0 {
		t.Fatalf("a refused submit left %d files", len(entries))
	}
	// Too many images, and a text the scrubber refuses, never reach preparation.
	var five []string
	for i := 0; i < 5; i++ {
		five = append(five, good)
	}
	if _, err := f.local.SubmitMedia(ctx, "Five.", "", five); !errors.As(err, &refused) {
		t.Fatalf("five images: %v", err)
	}
	if _, err := f.local.SubmitMedia(ctx, "mail me at someone@example.com", "", []string{good}); err == nil {
		t.Fatal("scrub skipped for a post with media")
	}
}

func TestAutoPostNeverAttachesMediaUnlessTheOwnerAllowedIt(t *testing.T) {
	f := newFixture(t)
	f.enableOn(t)
	ctx := context.Background()
	good := writeFile(t, t.TempDir(), "a.jpg", jpegWithEXIF(t, 30, 20, 1))
	if err := f.client.Do(ctx, "PUT", "/me/auto-post", map[string]any{"enabled": true}, nil); err != nil {
		t.Fatal(err)
	}
	out, err := f.local.SubmitMedia(ctx, "Auto with a picture.", "", []string{good})
	if err != nil || out.Posted || out.DraftID == "" || !strings.Contains(out.Why, "media") {
		t.Fatalf("auto without the media opt-in = %+v, %v", out, err)
	}
	// Text-only auto posts are unchanged.
	if text, err := f.local.Submit(ctx, "Auto text only.", ""); err != nil || !text.Posted {
		t.Fatalf("text auto = %+v, %v", text, err)
	}
	if err := f.client.Do(ctx, "PUT", "/me/auto-post", map[string]any{"media": true}, nil); err != nil {
		t.Fatal(err)
	}
	out, err = f.local.SubmitMedia(ctx, "Auto with a picture, allowed.", "", []string{good})
	if err != nil || !out.Posted {
		t.Fatalf("auto with the opt-in = %+v, %v", out, err)
	}
	if entries, _ := os.ReadDir(f.media.Dir); len(entries) != 1 {
		t.Fatalf("service holds %d objects, want 1", len(entries))
	}
	// Nothing of the scratch preparation stays on this computer.
	leftovers, _ := filepath.Glob(filepath.Join(os.TempDir(), "mewla-friends-media-*"))
	if len(leftovers) != 0 {
		t.Fatalf("scratch left behind: %v", leftovers)
	}
}

func TestUploadToAServiceWithoutMediaSaysMediaIsOff(t *testing.T) {
	f := newFixture(t)
	f.enableOn(t)
	old := httptest.NewServer(http.NotFoundHandler()) // a v0.2.77 service: unknown route, plain 404
	defer old.Close()
	client := &friends.Client{BaseURL: old.URL + friends.MountPrefix, Auth: f.client.Auth}
	src := writeFile(t, t.TempDir(), "a.jpg", jpegWithEXIF(t, 30, 20, 1))
	prepared, err := friends.PrepareMedia(context.Background(), src, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Upload(context.Background(), filepath.Join(filepath.Dir(src), "missing"), prepared, "image", "")
	if err == nil {
		t.Fatal("upload of a missing file succeeded")
	}
	dir := t.TempDir()
	prepared, err = friends.PrepareMedia(context.Background(), src, dir)
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Upload(context.Background(), filepath.Join(dir, prepared.File), prepared, "image", "")
	var refused *friends.ServiceError
	if !errors.As(err, &refused) || refused.Code != "media_off" || !strings.Contains(err.Error(), "does not take images") {
		t.Fatalf("err = %v", err)
	}
}
