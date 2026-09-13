package editorial

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"os"
	"testing"
)

func TestDraftAssetUploadRejectsTruncatedImage(t *testing.T) {
	for _, test := range []struct {
		name, format string
		body         []byte
		cut          int
	}{
		{"png", "png", validDraftPNG(t, 20, 10), 20},
		{"jpeg", "jpeg", validDraftJPEG(t, 20, 10), 10},
		{"gif", "gif", validDraftGIF(t, 20, 10), 5},
		{"gif later frame", "gif", validAnimatedDraftGIF(t, 20, 10), 5},
	} {
		t.Run(test.name, func(t *testing.T) {
			// Truncate an encoded image after its complete size header, in the image data.
			body := test.body[:len(test.body)-test.cut]
			if _, _, err := image.DecodeConfig(bytes.NewReader(body)); err != nil {
				t.Fatalf("fixture must retain a valid size header: %v", err)
			}
			_, _, decodeErr := image.Decode(bytes.NewReader(body))
			if test.format == "gif" {
				_, decodeErr = gif.DecodeAll(bytes.NewReader(body))
			}
			if decodeErr == nil {
				t.Fatal("fixture must contain truncated pixel data")
			}
			store := &fakeEditorialStore{draft: Draft{ID: 7, Status: StatusEditing, CurrentVersion: 3}}
			objects := &fakeAssetObjects{}
			service := New(store, &fakeArticleStore{}, nil, nil, objects, false)
			_, err := service.UploadAsset(context.Background(), 7, 42, "image."+test.format, "image/"+test.format, body)
			if !errors.Is(err, ErrDraftAssetInvalid) {
				t.Fatalf("UploadAsset() error = %v, want invalid image", err)
			}
			if len(objects.body) != 0 || len(store.assets) != 0 || store.draft.CurrentVersion != 3 {
				t.Fatal("invalid image changed object storage, asset records or draft version")
			}
		})
	}
}

func TestInspectDraftImageRejectsPixelBudget(t *testing.T) {
	webp := append([]byte(nil), validDraftWebPVP8L()...)
	binary.LittleEndian.PutUint32(webp[21:25], uint32(4097-1)|uint32(4096-1)<<14|1<<28)
	for _, test := range []struct {
		name, mediaType string
		body            []byte
	}{
		{"png pixels", "image/png", validDraftPNG(t, 4097, 4096)},
		{"jpeg pixels", "image/jpeg", validDraftJPEG(t, 4097, 4096)},
		{"gif pixels", "image/gif", validDraftGIF(t, 4097, 4096)},
		{"gif cumulative frame pixels", "image/gif", validAnimatedDraftGIF(t, 4096, 4096)},
		{"webp pixels", "image/webp", webp},
		{"png side", "image/png", validDraftPNG(t, 16385, 1)},
		{"jpeg side", "image/jpeg", validDraftJPEG(t, 1, 16385)},
		{"gif side", "image/gif", validDraftGIF(t, 16385, 1)},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := InspectDraftImage(test.body, test.mediaType)
			if !errors.Is(err, ErrDraftAssetInvalid) {
				t.Fatalf("InspectDraftImage() error = %v, want pixel budget rejection", err)
			}
		})
	}
}

func validAnimatedDraftGIF(t *testing.T, width, height int) []byte {
	t.Helper()
	palette := color.Palette{color.Black, color.White}
	var buffer bytes.Buffer
	if err := gif.EncodeAll(&buffer, &gif.GIF{
		Image: []*image.Paletted{
			image.NewPaletted(image.Rect(0, 0, width, height), palette),
			image.NewPaletted(image.Rect(0, 0, 1, 1), palette),
		},
		Delay: []int{10, 10},
	}); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func TestInspectDraftImageRejectsGIFFrameMetadataBudget(t *testing.T) {
	for _, frames := range []int{2, 1024, 1025} {
		t.Run(fmt.Sprint(frames), func(t *testing.T) {
			body := transparentPaletteDraftGIF(t, frames)
			if len(body) >= 10<<20 {
				t.Fatal("fixture must fit the upload size limit")
			}
			decoded, err := gif.DecodeAll(bytes.NewReader(body))
			if err != nil || len(decoded.Image) != frames || len(decoded.Image[0].Palette) != 256 {
				t.Fatalf("invalid transparent global palette fixture: %v", err)
			}
			_, err = InspectDraftImage(body, "image/gif")
			if frames <= 1024 && err != nil {
				t.Fatalf("normal animation rejected: %v", err)
			}
			if frames > 1024 && !errors.Is(err, ErrDraftAssetInvalid) {
				t.Fatalf("%d transparent frames accepted: %v; body=%d bytes, pixels=%d", frames, err, len(body), frames)
			}
			if frames > 1024 && draftGIFWithinPixelBudget(body) {
				t.Fatal("excessive frame metadata must be rejected before DecodeAll")
			}
		})
	}
}

func TestDraftGIFPlainTextExtension(t *testing.T) {
	for _, test := range []struct {
		name       string
		headerSize byte
		frames     int
		want       bool
	}{
		{"standard header", 12, 2, true},
		{"standard header at frame limit", 12, 1024, true},
		{"standard header over frame limit", 12, 1025, false},
		{"short declared header", 11, 2, false},
		{"long declared header", 13, 2, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			original := transparentPaletteDraftGIF(t, test.frames)
			const headerEnd = 13 + 256*3
			// A tiny local robustness fixture: one text extension before ordinary frames.
			body := append([]byte(nil), original[:headerEnd]...)
			body = append(body, 0x21, 0x01, test.headerSize)
			body = append(body, make([]byte, int(test.headerSize))...)
			body = append(body, 1, 'x', 0)
			body = append(body, original[headerEnd:]...)
			if test.headerSize == 12 {
				decoded, err := gif.DecodeAll(bytes.NewReader(body))
				if err != nil || len(decoded.Image) != test.frames {
					t.Fatalf("standard text extension changed decoded frame count: %v", err)
				}
			}
			if got := draftGIFWithinPixelBudget(body); got != test.want {
				t.Fatalf("pre-decode validation = %t, want %t for text header size %d", got, test.want, test.headerSize)
			}
			_, err := InspectDraftImage(body, "image/gif")
			if test.want && err != nil || !test.want && !errors.Is(err, ErrDraftAssetInvalid) {
				t.Fatalf("InspectDraftImage() = %v, accepted = %t", err, test.want)
			}
		})
	}
}

func transparentPaletteDraftGIF(t *testing.T, frames int) []byte {
	t.Helper()
	palette := make(color.Palette, 256)
	for index := range palette {
		palette[index] = color.RGBA{R: uint8(index), A: 255}
	}
	palette[0] = color.RGBA{}
	frame := image.NewPaletted(image.Rect(0, 0, 1, 1), palette)
	var buffer bytes.Buffer
	if err := gif.EncodeAll(&buffer, &gif.GIF{
		Image: []*image.Paletted{frame}, Delay: []int{1},
		Config: image.Config{ColorModel: palette, Width: 1, Height: 1},
	}); err != nil {
		t.Fatal(err)
	}
	body := buffer.Bytes()
	// Encoded global palette: 13-byte header + 256 RGB entries; duplicate only
	// the valid GCE/image block so every transparent frame clones that palette.
	const frameOffset = 13 + 256*3
	result := append([]byte(nil), body[:frameOffset]...)
	for index := 0; index < frames; index++ {
		result = append(result, body[frameOffset:len(body)-1]...)
	}
	return append(result, 0x3b)
}

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
		{name: "animated gif", mediaType: "image/gif", body: validAnimatedDraftGIF(t, 20, 10), wantType: "image/gif", wantExt: "gif", wantWidth: 20, wantHeight: 10},
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
		{name: "long vp8 bitstream truncated with consistent lengths", mediaType: "image/webp", body: truncatedDraftWebPFixture(t, "blue-purple-pink.lossy.webp", 64, false), want: ErrDraftAssetInvalid},
		{name: "long vp8l bitstream truncated with consistent lengths", mediaType: "image/webp", body: truncatedDraftWebPFixture(t, "gopher-doc.1bpp.lossless.webp", 16, false), want: ErrDraftAssetInvalid},
		{name: "long vp8x bitstream truncated with consistent lengths", mediaType: "image/webp", body: truncatedDraftWebPFixture(t, "blue-purple-pink.lossy.webp", 64, true), want: ErrDraftAssetInvalid},
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

func truncatedDraftWebPFixture(t *testing.T, name string, cut int, extended bool) []byte {
	t.Helper()
	body := readDraftWebPFixture(t, name)
	if extended {
		body = extendedDraftWebPVP8(body)
	}
	for offset := 12; offset+8 <= len(body); {
		chunkSize := int(binary.LittleEndian.Uint32(body[offset+4 : offset+8]))
		chunkEnd := offset + 8 + chunkSize
		paddedEnd := chunkEnd + chunkSize&1
		if chunkEnd > len(body) || paddedEnd > len(body) {
			t.Fatalf("invalid WebP fixture %q", name)
		}
		chunkID := string(body[offset : offset+4])
		if (chunkID == "VP8 " || chunkID == "VP8L") && paddedEnd == len(body) {
			if cut <= 0 || cut >= chunkSize || cut&1 != 0 {
				t.Fatalf("invalid truncation %d for WebP fixture %q", cut, name)
			}
			body = append([]byte(nil), body[:len(body)-cut]...)
			binary.LittleEndian.PutUint32(body[4:8], uint32(len(body)-8))
			binary.LittleEndian.PutUint32(body[offset+4:offset+8], uint32(chunkSize-cut))
			return body
		}
		offset = paddedEnd
	}
	t.Fatalf("WebP fixture %q has no final image chunk", name)
	return nil
}

func readDraftWebPFixture(t *testing.T, name string) []byte {
	t.Helper()
	encoded, err := os.ReadFile("testdata/" + name + ".b64")
	if err != nil {
		t.Fatal(err)
	}
	body, err := base64.StdEncoding.DecodeString(string(encoded))
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func extendedDraftWebPVP8(body []byte) []byte {
	extended := make([]byte, len(body)+18)
	copy(extended[:4], "RIFF")
	binary.LittleEndian.PutUint32(extended[4:8], uint32(len(extended)-8))
	copy(extended[8:12], "WEBP")
	copy(extended[12:16], "VP8X")
	binary.LittleEndian.PutUint32(extended[16:20], 10)
	extended[24] = 149
	extended[27] = 99
	copy(extended[30:], body[12:])
	return extended
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
