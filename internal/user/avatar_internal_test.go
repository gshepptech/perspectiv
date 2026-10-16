package user

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"

	"connectrpc.com/connect"

	"perspectiv/internal/api/apiutil"
)

func pngBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	img.Set(0, 0, color.RGBA{R: 200, A: 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return buf.Bytes()
}

func jpegBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		t.Fatalf("encode jpeg: %v", err)
	}
	return buf.Bytes()
}

// FR-052: the avatar limits decided in avatar.go.
func TestNormalizeAvatar(t *testing.T) {
	t.Parallel()
	gif := []byte("GIF89a\x01\x00\x01\x00\x80\x00\x00\x00\x00\x00\xff\xff\xff!\xf9\x04\x01\x00\x00\x00\x00,\x00\x00\x00\x00\x01\x00\x01\x00\x00\x02\x02D\x01\x00;")
	oversized := append(pngBytes(t, 8, 8), make([]byte, MaxAvatarBytes)...)

	tests := []struct {
		name        string
		data        []byte
		contentType string
		wantExt     string
		wantErr     bool
	}{
		{name: "png", data: pngBytes(t, 64, 64), contentType: "image/png", wantExt: ".png"},
		{name: "jpeg", data: jpegBytes(t, 64, 64), contentType: "image/jpeg", wantExt: ".jpg"},
		{name: "content type with parameters", data: pngBytes(t, 8, 8), contentType: "image/png; charset=binary", wantExt: ".png"},
		{name: "largest side allowed", data: pngBytes(t, MaxAvatarSide, 1), contentType: "image/png", wantExt: ".png"},
		{name: "side too large", data: pngBytes(t, MaxAvatarSide+1, 1), contentType: "image/png", wantErr: true},
		{name: "too many bytes", data: oversized, contentType: "image/png", wantErr: true},
		{name: "declared type disagrees with bytes", data: pngBytes(t, 8, 8), contentType: "image/jpeg", wantErr: true},
		{name: "gif not accepted", data: gif, contentType: "image/gif", wantErr: true},
		{name: "not an image", data: []byte("<svg xmlns='http://www.w3.org/2000/svg'/>"), contentType: "image/svg+xml", wantErr: true},
		{name: "png header with garbage body", data: append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 64)...), contentType: "image/png", wantErr: true},
		{name: "missing content type", data: pngBytes(t, 8, 8), contentType: "", wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ext, out, err := normalizeAvatar(tc.data, tc.contentType)
			if tc.wantErr {
				if code, typed := apiutil.CodeOf(err); err == nil || !typed || code != connect.CodeInvalidArgument {
					t.Fatalf("err = %v, want InvalidArg", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("err = %v", err)
			}
			if ext != tc.wantExt {
				t.Fatalf("ext = %q, want %q", ext, tc.wantExt)
			}
			checkStored(t, out, ext)
		})
	}
}

// checkStored asserts the stored bytes are a re-encoding that decodes in the
// format the extension names.
func checkStored(t *testing.T, out []byte, ext string) {
	t.Helper()
	_, format, err := image.DecodeConfig(bytes.NewReader(out))
	if err != nil {
		t.Fatalf("stored avatar does not decode: %v", err)
	}
	if want := map[string]string{".png": "png", ".jpg": "jpeg"}[ext]; format != want {
		t.Fatalf("stored format %q, ext %q", format, ext)
	}
}

// Metadata such as a phone's GPS position must not survive the upload.
func TestNormalizeAvatarStripsMetadata(t *testing.T) {
	t.Parallel()
	src := pngBytes(t, 8, 8)
	// A well-formed tEXt chunk inserted after IHDR (8-byte signature plus the
	// 25-byte IHDR chunk).
	const text = "Comment\x00home 40.7,-74.0"
	body := []byte("tEXt" + text)
	chunk := binary.BigEndian.AppendUint32(nil, uint32(len(text)))
	chunk = append(chunk, body...)
	chunk = binary.BigEndian.AppendUint32(chunk, crc32.ChecksumIEEE(body))
	withText := append(append(append([]byte{}, src[:33]...), chunk...), src[33:]...)
	if _, err := png.Decode(bytes.NewReader(withText)); err != nil {
		t.Fatalf("fixture is not a valid png: %v", err)
	}
	_, out, err := normalizeAvatar(withText, "image/png")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if bytes.Contains(out, []byte("home 40.7")) {
		t.Fatal("stored avatar kept the upload's text metadata")
	}
}
