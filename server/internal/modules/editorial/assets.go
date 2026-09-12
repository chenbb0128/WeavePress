package editorial

import (
	"bytes"
	"crypto/sha256"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"mime"
	"net/http"
	"strings"
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
	if len(body) < 30 || string(body[:4]) != "RIFF" || string(body[8:12]) != "WEBP" {
		return 0, 0
	}
	chunk, payload := string(body[12:16]), body[20:]
	read24 := func(value []byte) uint {
		return uint(value[0]) | uint(value[1])<<8 | uint(value[2])<<16
	}
	switch chunk {
	case "VP8X":
		if len(payload) >= 10 {
			return read24(payload[4:7]) + 1, read24(payload[7:10]) + 1
		}
	case "VP8L":
		if len(payload) >= 5 && payload[0] == 0x2f {
			bits := uint(payload[1]) | uint(payload[2])<<8 | uint(payload[3])<<16 | uint(payload[4])<<24
			return (bits & 0x3fff) + 1, ((bits >> 14) & 0x3fff) + 1
		}
	case "VP8 ":
		if len(payload) >= 10 && payload[3] == 0x9d && payload[4] == 0x01 && payload[5] == 0x2a {
			width := (uint(payload[6]) | uint(payload[7])<<8) & 0x3fff
			height := (uint(payload[8]) | uint(payload[9])<<8) & 0x3fff
			return width, height
		}
	}
	return 0, 0
}
