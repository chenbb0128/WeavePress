package editorial

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"image/png"
	"io"
	"strconv"
	"time"

	xdraw "golang.org/x/image/draw"
)

// normalizeBodyImages creates draft-local copies; collected originals and the
// cover stay intact. Conversion failures leave the image for preflight to flag.
func (s *Service) normalizeBodyImages(ctx context.Context, draftID, userID uint64, version uint, doc *Document) (*Document, error) {
	if s.objects == nil {
		return doc, nil
	}
	replacements := make(map[uint64]uint64)
	checkedVersion := false
	// Limit optional image work so unavailable storage cannot block saving.
	imageCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	for _, id := range ReferencedDraftAssetIDs(*doc) {
		asset, err := s.store.GetDraftAsset(ctx, draftID, id)
		if err != nil {
			return nil, err
		}
		if asset.ByteSize <= WeChatMaxContentImageSize || asset.ByteSize > WeChatMaxCoverImageSize || asset.MediaType == "image/gif" {
			continue
		}
		if !checkedVersion {
			draft, err := s.store.GetDraft(ctx, draftID, false)
			if err != nil {
				return nil, err
			}
			if draft.CurrentVersion != version {
				return nil, ErrDraftVersionConflict
			}
			if draft.Status != StatusEditing {
				// Legacy reviewed drafts may be migrated by UpdateDraft, which
				// clears the approval atomically. Do not upload while still reviewed;
				// save the migration and leave oversized images to preflight.
				return doc, nil
			}
			checkedVersion = true
		}
		if imageCtx.Err() != nil {
			break
		}
		reader, err := s.objects.Open(imageCtx, asset.ObjectKey)
		if err != nil {
			continue
		}
		body, err := io.ReadAll(io.LimitReader(reader, WeChatMaxCoverImageSize+1))
		_ = reader.Close()
		if err != nil {
			continue
		}
		body, mediaType, err := normalizeContentImage(body, asset.MediaType)
		if err != nil || imageCtx.Err() != nil {
			continue
		}
		copy, err := s.UploadAsset(imageCtx, draftID, userID, "body-image", mediaType, body)
		if err != nil {
			continue
		}
		replacements[id] = copy.ID
	}
	if len(replacements) == 0 {
		return doc, nil
	}
	// Copy before replacing IDs to keep the request/historical document intact.
	raw, err := json.Marshal(doc)
	if err != nil {
		return nil, err
	}
	var result Document
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, err
	}
	var walk func([]Node)
	walk = func(nodes []Node) {
		for i := range nodes {
			node := &nodes[i]
			if node.Type == "image" {
				var id uint64
				if json.Unmarshal(node.Attrs["draftAssetId"], &id) == nil {
					if replacement, ok := replacements[id]; ok {
						node.Attrs["draftAssetId"] = json.RawMessage(strconv.FormatUint(replacement, 10))
					}
				}
			}
			walk(node.Content)
		}
	}
	walk(result.Content)
	return &result, nil
}

func normalizeContentImage(body []byte, mediaType string) ([]byte, string, error) {
	inspected, err := InspectDraftImage(body, mediaType)
	if err != nil {
		return nil, "", err
	}
	if inspected.ByteSize <= WeChatMaxContentImageSize {
		return body, inspected.MediaType, nil
	}
	// Preserve animation instead of silently flattening it to a still image.
	if inspected.MediaType == "image/gif" {
		return nil, "", ErrDraftAssetTooLarge
	}
	source, _, err := image.Decode(bytes.NewReader(body))
	if err != nil {
		return nil, "", err
	}
	var encoded bytes.Buffer
	var exif []byte
	if inspected.MediaType == "image/jpeg" {
		exif = jpegEXIFSegment(body)
	}
	if inspected.MediaType == "image/png" {
		if err := png.Encode(&encoded, source); err == nil && encoded.Len() <= WeChatMaxContentImageSize {
			return encoded.Bytes(), "image/png", nil
		}
	}
	// Flatten transparency onto the white page before encoding a JPEG copy.
	canvas := image.NewRGBA(image.Rect(0, 0, source.Bounds().Dx(), source.Bounds().Dy()))
	draw.Draw(canvas, canvas.Bounds(), &image.Uniform{C: color.White}, image.Point{}, draw.Src)
	draw.Draw(canvas, canvas.Bounds(), source, source.Bounds().Min, draw.Over)
	for {
		for _, quality := range []int{85, 75} {
			encoded.Reset()
			if err := jpeg.Encode(&encoded, canvas, &jpeg.Options{Quality: quality}); err != nil {
				return nil, "", err
			}
			if encoded.Len()+len(exif) <= WeChatMaxContentImageSize {
				if len(exif) == 0 {
					return encoded.Bytes(), "image/jpeg", nil
				}
				// image.Decode does not apply EXIF Orientation. Keep the original
				// bounded APP1 segment so browsers display the same orientation.
				result := make([]byte, 0, encoded.Len()+len(exif))
				result = append(result, encoded.Bytes()[:2]...)
				result = append(result, exif...)
				result = append(result, encoded.Bytes()[2:]...)
				return result, "image/jpeg", nil
			}
		}
		width, height := canvas.Bounds().Dx()*4/5, canvas.Bounds().Dy()*4/5
		if width < 1 {
			width = 1
		}
		if height < 1 {
			height = 1
		}
		if width == canvas.Bounds().Dx() && height == canvas.Bounds().Dy() {
			return nil, "", ErrDraftAssetTooLarge
		}
		smaller := image.NewRGBA(image.Rect(0, 0, width, height))
		xdraw.CatmullRom.Scale(smaller, smaller.Bounds(), canvas, canvas.Bounds(), draw.Src, nil)
		canvas = smaller
	}
}

func jpegEXIFSegment(body []byte) []byte {
	for offset := 2; offset+1 < len(body) && body[offset] == 0xff; {
		start := offset
		for offset < len(body) && body[offset] == 0xff {
			offset++
		}
		if offset >= len(body) {
			break
		}
		marker := body[offset]
		offset++
		if marker == 0xda || marker == 0xd9 {
			break
		} // Scan data or end of image.
		if marker == 0x01 || marker >= 0xd0 && marker <= 0xd8 {
			continue
		}
		if offset+2 > len(body) {
			break
		}
		length := int(binary.BigEndian.Uint16(body[offset : offset+2]))
		if length < 2 || length > len(body)-offset {
			break
		}
		end := offset + length
		if marker == 0xe1 && bytes.HasPrefix(body[offset+2:end], []byte("Exif\x00\x00")) {
			return body[start:end]
		}
		offset = end
	}
	return nil
}
