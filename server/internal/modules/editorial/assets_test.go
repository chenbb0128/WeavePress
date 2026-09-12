package editorial

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"testing"
)

func TestInspectDraftImage(t *testing.T) {
	tests := []struct {
		name       string
		mediaType  string
		body       []byte
		wantType   string
		wantExt    string
		wantWidth  uint
		wantHeight uint
	}{
		{name: "png", mediaType: "image/png", body: validDraftPNG(t, 20, 10), wantType: "image/png", wantExt: "png", wantWidth: 20, wantHeight: 10},
		{name: "jpeg", mediaType: "image/jpeg", body: validDraftJPEG(t, 20, 10), wantType: "image/jpeg", wantExt: "jpg", wantWidth: 20, wantHeight: 10},
		{name: "gif", mediaType: "image/gif", body: validDraftGIF(t, 20, 10), wantType: "image/gif", wantExt: "gif", wantWidth: 20, wantHeight: 10},
		{name: "webp vp8", mediaType: "image/webp", body: validDraftWebPVP8(), wantType: "image/webp", wantExt: "webp", wantWidth: 1, wantHeight: 1},
		{name: "webp vp8l", mediaType: "image/webp", body: validDraftWebPVP8L(), wantType: "image/webp", wantExt: "webp", wantWidth: 1, wantHeight: 1},
		{name: "webp vp8x", mediaType: "image/webp", body: validDraftWebPVP8X(), wantType: "image/webp", wantExt: "webp", wantWidth: 1, wantHeight: 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := InspectDraftImage(test.body, test.mediaType)
			if err != nil {
				t.Fatal(err)
			}
			if got.MediaType != test.wantType || got.Extension != test.wantExt || got.ByteSize != uint64(len(test.body)) || got.Width != test.wantWidth || got.Height != test.wantHeight {
				t.Fatalf("InspectDraftImage() = %#v", got)
			}
		})
	}
}

func TestInspectDraftImageRejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name      string
		mediaType string
		body      []byte
		want      error
	}{
		{name: "spoofed image", mediaType: "image/png", body: []byte("not an image"), want: ErrDraftAssetType},
		{name: "svg", mediaType: "image/svg+xml", body: []byte(`<svg xmlns="http://www.w3.org/2000/svg"/>`), want: ErrDraftAssetType},
		{name: "mismatched declaration", mediaType: "image/jpeg", body: validDraftPNG(t, 20, 10), want: ErrDraftAssetType},
		{name: "zero dimensions", mediaType: "image/webp", body: zeroWidthDraftWebP(), want: ErrDraftAssetInvalid},
		{name: "vp8x canvas without image", mediaType: "image/webp", body: draftWebPCanvasOnly(), want: ErrDraftAssetInvalid},
		{name: "truncated image payload", mediaType: "image/webp", body: validDraftWebPVP8()[:len(validDraftWebPVP8())-1], want: ErrDraftAssetInvalid},
		{name: "wrong riff length", mediaType: "image/webp", body: draftWebPWithWrongRIFFLength(), want: ErrDraftAssetInvalid},
		{name: "wrong chunk length", mediaType: "image/webp", body: draftWebPWithWrongChunkLength(), want: ErrDraftAssetInvalid},
		{name: "too large", mediaType: "image/png", body: make([]byte, WeChatMaxCoverImageSize+1), want: ErrDraftAssetTooLarge},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := InspectDraftImage(test.body, test.mediaType)
			if !errors.Is(err, test.want) {
				t.Fatalf("InspectDraftImage() error = %v, want %v", err, test.want)
			}
		})
	}
}

func validDraftPNG(t *testing.T, width, height int) []byte {
	t.Helper()
	return encodeDraftImage(t, func(buffer *bytes.Buffer, source image.Image) error {
		return png.Encode(buffer, source)
	}, width, height)
}

func validDraftJPEG(t *testing.T, width, height int) []byte {
	t.Helper()
	return encodeDraftImage(t, func(buffer *bytes.Buffer, source image.Image) error {
		return jpeg.Encode(buffer, source, nil)
	}, width, height)
}

func validDraftGIF(t *testing.T, width, height int) []byte {
	t.Helper()
	return encodeDraftImage(t, func(buffer *bytes.Buffer, source image.Image) error {
		return gif.Encode(buffer, source, nil)
	}, width, height)
}

func encodeDraftImage(t *testing.T, encode func(*bytes.Buffer, image.Image) error, width, height int) []byte {
	t.Helper()
	source := image.NewRGBA(image.Rect(0, 0, width, height))
	source.Set(0, 0, color.RGBA{R: 255, A: 255})
	var buffer bytes.Buffer
	if err := encode(&buffer, source); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func draftWebPCanvasOnly() []byte {
	body := make([]byte, 30)
	copy(body[:4], "RIFF")
	body[4] = 22
	copy(body[8:12], "WEBP")
	copy(body[12:16], "VP8X")
	body[16] = 10
	return body
}

func validDraftWebPVP8() []byte {
	return []byte{
		'R', 'I', 'F', 'F', 0x24, 0, 0, 0, 'W', 'E', 'B', 'P',
		'V', 'P', '8', ' ', 0x18, 0, 0, 0,
		0x30, 0x01, 0x00, 0x9d, 0x01, 0x2a, 0x01, 0x00, 0x01, 0x00, 0x02, 0x00,
		0x34, 0x25, 0xa4, 0x00, 0x03, 0x70, 0x00, 0xfe, 0xfb, 0xfd, 0x50, 0x00,
	}
}

func validDraftWebPVP8L() []byte {
	return []byte{
		'R', 'I', 'F', 'F', 0x14, 0, 0, 0, 'W', 'E', 'B', 'P',
		'V', 'P', '8', 'L', 0x08, 0, 0, 0,
		0x2f, 0x00, 0x00, 0x00, 0x10, 0x88, 0x88, 0x08,
	}
}

func validDraftWebPVP8X() []byte {
	return []byte{
		'R', 'I', 'F', 'F', 0x40, 0, 0, 0, 'W', 'E', 'B', 'P',
		'V', 'P', '8', 'X', 0x0a, 0, 0, 0,
		0x10, 0, 0, 0, 0, 0, 0, 0, 0, 0,
		'A', 'L', 'P', 'H', 0x02, 0, 0, 0, 0, 0,
		'V', 'P', '8', ' ', 0x18, 0, 0, 0,
		0x30, 0x01, 0x00, 0x9d, 0x01, 0x2a, 0x01, 0x00, 0x01, 0x00, 0x02, 0x00,
		0x34, 0x25, 0xa4, 0x00, 0x03, 0x70, 0x00, 0xfe, 0xfb, 0xfd, 0x50, 0x00,
	}
}

func draftWebPWithWrongRIFFLength() []byte {
	body := append([]byte(nil), validDraftWebPVP8()...)
	body[4]--
	return body
}

func draftWebPWithWrongChunkLength() []byte {
	body := append([]byte(nil), validDraftWebPVP8()...)
	body[16]++
	return body
}

func zeroWidthDraftWebP() []byte {
	body := make([]byte, 30)
	copy(body[:4], "RIFF")
	body[4] = 22
	copy(body[8:12], "WEBP")
	copy(body[12:16], "VP8 ")
	body[16] = 10
	copy(body[23:26], []byte{0x9d, 0x01, 0x2a})
	body[28] = 10
	return body
}
