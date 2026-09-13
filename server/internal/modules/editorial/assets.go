package editorial

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"image"
	"image/gif"
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

const (
	maxDraftImageSide   = 16384
	maxDraftImagePixels = 16777216
)

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
	// Divide instead of multiplying untrusted dimensions to avoid overflow.
	if width == 0 || height == 0 || width > maxDraftImageSide || height > maxDraftImageSide || width > maxDraftImagePixels/height {
		return InspectedImage{}, ErrDraftAssetInvalid
	}
	if actualType == "image/gif" {
		// DecodeAll retains every frame; bound their combined allocation first.
		if !draftGIFWithinPixelBudget(body) {
			return InspectedImage{}, ErrDraftAssetInvalid
		}
		_, err = gif.DecodeAll(bytes.NewReader(body))
	} else {
		_, _, err = image.Decode(bytes.NewReader(body))
	}
	if err != nil {
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

func draftGIFWithinPixelBudget(body []byte) bool {
	if len(body) < 13 {
		return false
	}
	offset := 13
	if body[10]&0x80 != 0 {
		offset += 3 << ((body[10] & 7) + 1)
	}
	var pixels uint
	for offset < len(body) {
		block := body[offset]
		offset++
		switch block {
		case 0x3b: // Trailer.
			return pixels > 0
		case 0x21: // Extension label, followed by data sub-blocks.
			offset++
		case 0x2c: // Image descriptor and optional local color table.
			if len(body)-offset < 9 {
				return false
			}
			width := uint(binary.LittleEndian.Uint16(body[offset+4 : offset+6]))
			height := uint(binary.LittleEndian.Uint16(body[offset+6 : offset+8]))
			if width == 0 || height == 0 || width > maxDraftImageSide || height > maxDraftImageSide || width > (maxDraftImagePixels-pixels)/height {
				return false
			}
			pixels += width * height
			packed := body[offset+8]
			offset += 9
			if packed&0x80 != 0 {
				offset += 3 << ((packed & 7) + 1)
			}
			offset++ // LZW minimum code size.
		default:
			return false
		}
		for {
			if offset >= len(body) {
				return false
			}
			size := int(body[offset])
			offset++
			if size == 0 {
				break
			}
			if size > len(body)-offset {
				return false
			}
			offset += size
		}
	}
	return false
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

	config, err := webp.DecodeConfig(bytes.NewReader(body))
	if err != nil {
		return 0, 0
	}
	if config.Width <= 0 || config.Height <= 0 {
		return 0, 0
	}
	return uint(config.Width), uint(config.Height)
}
