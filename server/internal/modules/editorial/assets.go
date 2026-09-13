package editorial

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"mime"
	"net/http"
	"strings"

	"golang.org/x/image/webp"
)

type InspectedImage struct {
	MediaType string
	Extension string
	ByteSize  uint64
	Width     uint
	Height    uint
	SHA256    [32]byte
}

func InspectDraftImage(body []byte, declared string) (InspectedImage, error) {
	if len(body) > WeChatMaxCoverImageSize {
		return InspectedImage{}, ErrDraftAssetTooLarge
	}
	declaredType, _, err := mime.ParseMediaType(declared)
	if err != nil {
		return InspectedImage{}, ErrDraftAssetType
	}
	actualType := strings.ToLower(http.DetectContentType(body))
	extension, supported := draftImageExtension(actualType)
	if !supported || !strings.EqualFold(declaredType, actualType) {
		return InspectedImage{}, ErrDraftAssetType
	}
	width, height := draftImageDimensions(body, actualType)
	if width == 0 || height == 0 {
		return InspectedImage{}, ErrDraftAssetInvalid
	}
	return InspectedImage{
		MediaType: actualType,
		Extension: extension,
		ByteSize:  uint64(len(body)),
		Width:     width,
		Height:    height,
		SHA256:    sha256.Sum256(body),
	}, nil
}

func draftImageExtension(mediaType string) (string, bool) {
	switch mediaType {
	case "image/jpeg":
		return "jpg", true
	case "image/png":
		return "png", true
	case "image/gif":
		return "gif", true
	case "image/webp":
		return "webp", true
	default:
		return "", false
	}
}

func draftImageDimensions(body []byte, mediaType string) (uint, uint) {
	if mediaType == "image/webp" {
		return draftWebPDimensions(body)
	}
	config, _, err := image.DecodeConfig(bytes.NewReader(body))
	if err != nil || config.Width <= 0 || config.Height <= 0 {
		return 0, 0
	}
	return uint(config.Width), uint(config.Height)
}

func draftWebPDimensions(body []byte) (uint, uint) {
	if len(body) < 20 || string(body[:4]) != "RIFF" || string(body[8:12]) != "WEBP" {
		return 0, 0
	}
	if uint64(binary.LittleEndian.Uint32(body[4:8]))+8 != uint64(len(body)) {
		return 0, 0
	}

	for offset := 12; offset < len(body); {
		if len(body)-offset < 8 {
			return 0, 0
		}
		chunkSize := uint64(binary.LittleEndian.Uint32(body[offset+4 : offset+8]))
		offset += 8
		if chunkSize > uint64(len(body)-offset) {
			return 0, 0
		}
		chunkEnd := offset + int(chunkSize)
		paddedEnd := chunkEnd + int(chunkSize&1)
		if paddedEnd > len(body) {
			return 0, 0
		}
		offset = paddedEnd
	}

	decoded, err := webp.Decode(bytes.NewReader(body))
	if err != nil {
		return 0, 0
	}
	bounds := decoded.Bounds()
	if bounds.Dx() <= 0 || bounds.Dy() <= 0 {
		return 0, 0
	}
	return uint(bounds.Dx()), uint(bounds.Dy())
}
