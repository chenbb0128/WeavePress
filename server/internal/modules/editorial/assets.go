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

	var canvasWidth, canvasHeight uint
	var imageWidth, imageHeight uint
	seenVP8X, seenImage, seenAlpha := false, false, false
	for offset := 12; offset < len(body); {
		if len(body)-offset < 8 {
			return 0, 0
		}
		chunkID := string(body[offset : offset+4])
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
		payload := body[offset:chunkEnd]

		switch chunkID {
		case "VP8X":
			if seenVP8X || seenImage || offset != 20 || len(payload) != 10 || payload[0]&0xc1 != 0 || payload[1] != 0 || payload[2] != 0 || payload[3] != 0 {
				return 0, 0
			}
			seenVP8X = true
			canvasWidth = readDraftWebPUint24(payload[4:7]) + 1
			canvasHeight = readDraftWebPUint24(payload[7:10]) + 1
			if uint64(canvasWidth)*uint64(canvasHeight) > 1<<32-1 {
				return 0, 0
			}
		case "ALPH":
			if !seenVP8X || seenAlpha || seenImage || len(payload) < 2 || payload[0]&0xc0 != 0 || payload[0]&0x03 > 1 {
				return 0, 0
			}
			seenAlpha = true
		case "VP8 ":
			if seenImage {
				return 0, 0
			}
			imageWidth, imageHeight = draftVP8Dimensions(payload)
			if imageWidth == 0 || imageHeight == 0 || seenVP8X && payloadNeedsDraftWebPAlpha(body[20]) != seenAlpha {
				return 0, 0
			}
			seenImage = true
		case "VP8L":
			if seenImage || seenAlpha {
				return 0, 0
			}
			imageWidth, imageHeight = draftVP8LDimensions(payload)
			if imageWidth == 0 || imageHeight == 0 {
				return 0, 0
			}
			seenImage = true
		}
		offset = paddedEnd
	}
	if !seenImage {
		return 0, 0
	}
	if seenVP8X && (imageWidth != canvasWidth || imageHeight != canvasHeight) {
		return 0, 0
	}
	return imageWidth, imageHeight
}

func draftVP8Dimensions(payload []byte) (uint, uint) {
	const minimumTokenPartitionSize = 4

	if len(payload) < 11 || payload[0]&1 != 0 || payload[3] != 0x9d || payload[4] != 0x01 || payload[5] != 0x2a {
		return 0, 0
	}
	frameTag := uint32(payload[0]) | uint32(payload[1])<<8 | uint32(payload[2])<<16
	firstPartitionSize := uint64(frameTag >> 5)
	if firstPartitionSize == 0 || firstPartitionSize+10+minimumTokenPartitionSize > uint64(len(payload)) {
		return 0, 0
	}
	width := (uint(payload[6]) | uint(payload[7])<<8) & 0x3fff
	height := (uint(payload[8]) | uint(payload[9])<<8) & 0x3fff
	return width, height
}

func draftVP8LDimensions(payload []byte) (uint, uint) {
	if len(payload) < 8 || payload[0] != 0x2f {
		return 0, 0
	}
	bits := binary.LittleEndian.Uint32(payload[1:5])
	if bits>>29 != 0 {
		return 0, 0
	}
	return uint(bits&0x3fff) + 1, uint((bits>>14)&0x3fff) + 1
}

func readDraftWebPUint24(value []byte) uint {
	return uint(value[0]) | uint(value[1])<<8 | uint(value[2])<<16
}

func payloadNeedsDraftWebPAlpha(vp8xFlags byte) bool {
	return vp8xFlags&(1<<4) != 0
}
