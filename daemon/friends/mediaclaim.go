package friends

import "strconv"

// UploadSignedBody is the byte string a media upload is signed over in place
// of its (large) body: every header that decides what the upload is, with the
// body's SHA-256. The service verifies the same bytes and then checks the
// received body against the hash, so a signature cannot be reused for other
// content, another kind or another poster.
func UploadSignedBody(sha256Hex, kind, mime string, width, height int, posterID string) []byte {
	parts := []string{sha256Hex, kind, mime, strconv.Itoa(width) + "x" + strconv.Itoa(height), posterID}
	out := []byte{}
	for i, part := range parts {
		if i > 0 {
			out = append(out, '\n')
		}
		out = append(out, part...)
	}
	return out
}
