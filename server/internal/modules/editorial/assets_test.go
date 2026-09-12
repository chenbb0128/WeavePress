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
		name      string
		mediaType string
		body      []byte
		wantType  string
		wantExt   string
	}{
		{name: "png", mediaType: "image/png", body: validDraftPNG(t, 20, 10), wantType: "image/png", wantExt: "png"},
		{name: "jpeg", mediaType: "image/jpeg", body: validDraftJPEG(t, 20, 10), wantType: "image/jpeg", wantExt: "jpg"},
		{name: "gif", mediaType: "image/gif", body: validDraftGIF(t, 20, 10), wantType: "image/gif", wantExt: "gif"},
		{name: "webp", mediaType: "image/webp", body: validDraftWebP(20, 10), wantType: "image/webp", wantExt: "webp"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := InspectDraftImage(test.body, test.mediaType)
			if err != nil {
				t.Fatal(err)
			}
			if got.MediaType != test.wantType || got.Extension != test.wantExt || got.ByteSize != uint64(len(test.body)) || got.Width != 20 || got.Height != 10 {
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

func validDraftWebP(width, height uint) []byte {
	body := make([]byte, 30)
	copy(body[:4], "RIFF")
	copy(body[8:12], "WEBP")
	copy(body[12:16], "VP8X")
	putDraftUint24(body[24:27], width-1)
	putDraftUint24(body[27:30], height-1)
	return body
}

func zeroWidthDraftWebP() []byte {
	body := make([]byte, 30)
	copy(body[:4], "RIFF")
	copy(body[8:12], "WEBP")
	copy(body[12:16], "VP8 ")
	copy(body[23:26], []byte{0x9d, 0x01, 0x2a})
	body[28] = 10
	return body
}

func putDraftUint24(target []byte, value uint) {
	target[0] = byte(value)
	target[1] = byte(value >> 8)
	target[2] = byte(value >> 16)
}
