package main

import (
	"errors"
	"flag"
	"os"
	"path/filepath"
	"strings"

	"github.com/daoleno/mewla/daemon/friendsvc"
)

// friendsMediaFlags says where Friends keeps post images and video. With
// neither choice set, media is off and posts are text only.
type friendsMediaFlags struct {
	dir string

	endpoint  string
	region    string
	bucket    string
	publicURL string
}

func (f *friendsMediaFlags) register(flags *flag.FlagSet) {
	flags.StringVar(&f.dir, "friends-media-dir", "", "DEVELOPMENT AND TESTS: keep Friends media in this directory and serve it from the service itself (off when empty)")
	flags.StringVar(&f.endpoint, "friends-media-endpoint", "", "S3-compatible API address for Friends media, for example https://<account>.r2.cloudflarestorage.com (credentials in $MEWLA_ACCOUNT_MEDIA_ACCESS_KEY and $MEWLA_ACCOUNT_MEDIA_SECRET_KEY)")
	flags.StringVar(&f.region, "friends-media-region", "auto", "region for the Friends media bucket")
	flags.StringVar(&f.bucket, "friends-media-bucket", "", "bucket for Friends media")
	flags.StringVar(&f.publicURL, "friends-media-public-url", "", "public address the bucket is read from, a host of its own (for example https://friends-media.mewla.ai)")
}

// store builds the media store, or nil when media is off. accountURL is the
// service's own public address, which a directory store serves media under.
func (f friendsMediaFlags) store(accountURL string) (friendsvc.MediaStore, error) {
	switch {
	case f.endpoint != "" || f.bucket != "" || f.publicURL != "":
		access, secret := strings.TrimSpace(os.Getenv("MEWLA_ACCOUNT_MEDIA_ACCESS_KEY")), strings.TrimSpace(os.Getenv("MEWLA_ACCOUNT_MEDIA_SECRET_KEY"))
		if f.endpoint == "" || f.bucket == "" || f.publicURL == "" || access == "" || secret == "" {
			return nil, errors.New("friends media needs -friends-media-endpoint, -friends-media-bucket, -friends-media-public-url and the two MEWLA_ACCOUNT_MEDIA_* credentials together")
		}
		if f.dir != "" {
			return nil, errors.New("choose -friends-media-dir or the object store flags, not both")
		}
		return &friendsvc.S3MediaStore{Endpoint: f.endpoint, Region: f.region, Bucket: f.bucket, AccessKey: access, SecretKey: secret, PublicBaseURL: f.publicURL}, nil
	case f.dir != "":
		if accountURL == "" {
			return nil, errors.New("-friends-media-dir needs -public-url, the address clients read the media from")
		}
		dir, err := filepath.Abs(f.dir)
		if err != nil {
			return nil, err
		}
		return &friendsvc.DirMediaStore{Dir: dir, PublicURL: strings.TrimRight(accountURL, "/") + friendsvc.PathPrefix}, nil
	}
	return nil, nil
}
