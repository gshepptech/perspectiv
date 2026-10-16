package user

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"os"
	"regexp"
	"strings"

	"connectrpc.com/connect"
	"github.com/google/uuid"

	"perspectiv/internal/api/apiutil"
)

// Avatar limits. An avatar is a small image kept on local disk or a mounted
// volume (AVATAR_DIR); there is no object storage.
//
//   - Formats: PNG and JPEG only. The declared content type must match what
//     the bytes decode as. GIF (animation), WebP (no standard-library
//     decoder) and SVG (script) are refused.
//   - Size: at most MaxAvatarBytes as uploaded, and at most MaxAvatarSide
//     pixels on each side, checked from the header before the pixels are
//     decoded so a small file cannot expand into a huge bitmap.
//   - The stored file is a fresh encoding of the decoded pixels, so EXIF,
//     text chunks and anything else riding in the upload (a phone's GPS
//     position, for one) are dropped.
const (
	MaxAvatarBytes = 256 << 10
	MaxAvatarSide  = 1024
)

// AvatarURLPrefix is where avatars are served: the avatar Mount's path under
// the router's /api prefix. A profile's avatar_url is this plus the file name.
const AvatarURLPrefix = "/api/avatars/"

// avatarMountPath is the Mount's path relative to /api.
const avatarMountPath = "/avatars/"

// avatarName matches the only file names the store writes: a random UUID and
// the format's extension. The server refuses to serve any other name, so a
// request cannot reach a temp file or anything outside the directory.
var avatarName = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\.(png|jpg)$`)

const jpegQuality = 90

// AvatarStore keeps avatar files in one directory and serves them.
type AvatarStore struct {
	root *os.Root
}

// NewAvatarStore opens dir, creating it if needed. Every file operation is
// confined to dir through os.Root.
func NewAvatarStore(dir string) (*AvatarStore, error) {
	if dir == "" {
		return nil, errors.New("avatar directory is not set")
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, fmt.Errorf("create avatar directory: %w", err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, fmt.Errorf("open avatar directory: %w", err)
	}
	return &AvatarStore{root: root}, nil
}

// avatarURL is the URL a profile shows for a stored file name; "" for none.
func avatarURL(name *string) string {
	if name == nil || *name == "" {
		return ""
	}
	return AvatarURLPrefix + *name
}

// normalizeAvatar checks an upload against the limits above and returns the
// file extension and the bytes to store. Every refusal is apiutil.InvalidArg.
func normalizeAvatar(data []byte, contentType string) (string, []byte, error) {
	if len(data) > MaxAvatarBytes {
		return "", nil, apiutil.InvalidArg(fmt.Sprintf("avatar must be at most %d KiB", MaxAvatarBytes>>10))
	}
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return "", nil, apiutil.InvalidArg("avatar_content_type must be image/png or image/jpeg")
	}
	var wantFormat, ext string
	switch mediaType {
	case "image/png":
		wantFormat, ext = "png", ".png"
	case "image/jpeg":
		wantFormat, ext = "jpeg", ".jpg"
	default:
		return "", nil, apiutil.InvalidArg("avatar must be a PNG or JPEG image")
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || format != wantFormat {
		return "", nil, apiutil.InvalidArg("avatar is not a valid " + mediaType + " image")
	}
	if cfg.Width < 1 || cfg.Height < 1 || cfg.Width > MaxAvatarSide || cfg.Height > MaxAvatarSide {
		return "", nil, apiutil.InvalidArg(fmt.Sprintf("avatar must be at most %d by %d pixels", MaxAvatarSide, MaxAvatarSide))
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return "", nil, apiutil.InvalidArg("avatar is not a valid " + mediaType + " image")
	}
	var out bytes.Buffer
	if wantFormat == "png" {
		err = png.Encode(&out, img)
	} else {
		err = jpeg.Encode(&out, img, &jpeg.Options{Quality: jpegQuality})
	}
	if err != nil {
		return "", nil, fmt.Errorf("re-encode avatar: %w", err)
	}
	return ext, out.Bytes(), nil
}

// write stores data under a new random name and returns the name. The file
// appears under its final name only once fully written.
func (a *AvatarStore) write(ext string, data []byte) (string, error) {
	name := uuid.NewString() + ext
	tmp := ".upload-" + name
	f, err := a.root.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o640)
	if err != nil {
		return "", fmt.Errorf("create avatar file: %w", err)
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		_ = a.root.Remove(tmp)
		return "", fmt.Errorf("write avatar file: %w", err)
	}
	if err := f.Close(); err != nil {
		_ = a.root.Remove(tmp)
		return "", fmt.Errorf("close avatar file: %w", err)
	}
	if err := a.root.Rename(tmp, name); err != nil {
		_ = a.root.Remove(tmp)
		return "", fmt.Errorf("publish avatar file: %w", err)
	}
	return name, nil
}

// remove deletes a stored avatar; a file already gone is not an error.
func (a *AvatarStore) remove(name string) error {
	if !avatarName.MatchString(name) {
		return fmt.Errorf("refusing to remove %q: not an avatar file name", name)
	}
	if err := a.root.Remove(name); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("remove avatar file: %w", err)
	}
	return nil
}

// Mount serves the stored avatars at /avatars/<name> under the router's /api
// prefix. It has the shape of api.Mount so the composition root can list it
// beside the Connect services; the Connect handler options do not apply to a
// static file and are ignored.
func (a *AvatarStore) Mount(_ ...connect.HandlerOption) (string, http.Handler) {
	return avatarMountPath, http.HandlerFunc(a.serve)
}

func (a *AvatarStore) serve(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	name := strings.TrimPrefix(r.URL.Path, avatarMountPath)
	if !avatarName.MatchString(name) {
		http.NotFound(w, r)
		return
	}
	f, err := a.root.Open(name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	contentType := "image/png"
	if strings.HasSuffix(name, ".jpg") {
		contentType = "image/jpeg"
	}
	w.Header().Set("Content-Type", contentType)
	// A name is never reused for different bytes: a new upload gets a new
	// random name.
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	http.ServeContent(w, r, name, info.ModTime(), io.ReadSeeker(f))
}
