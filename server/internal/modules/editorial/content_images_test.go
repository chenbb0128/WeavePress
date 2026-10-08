package editorial

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/png"
	"testing"
)

func TestUpdateNormalizesOversizedBodyImageAndKeepsOriginal(t *testing.T) {
	body := append(validDraftPNG(t, 1080, 1221), make([]byte, WeChatMaxContentImageSize)...)
	store := &fakeEditorialStore{
		draft:  Draft{ID: 1, Status: StatusEditing, CurrentVersion: 1},
		assets: []DraftAsset{{ID: 1, DraftID: 1, ObjectKey: "original.png", MediaType: "image/png", ByteSize: uint64(len(body)), CoverEligible: true}},
	}
	objects := &fakeAssetObjects{original: body}
	service := New(store, &fakeArticleStore{}, nil, nil, objects, false)
	doc := savedDocument(t, `{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"正文"}]}]}`)
	doc.Content = append(doc.Content, imageNode(1), imageNode(1))
	cover := uint64(1)
	got, err := service.Update(context.Background(), 1, 7, UpdateInput{Title: "标题", EditorDocument: doc, ThemeID: "clear-blue", CoverAssetID: &cover, ExpectedVersion: 1})
	if err != nil {
		t.Fatal(err)
	}
	ids := ReferencedDraftAssetIDs(*got.EditorDocument)
	if len(ids) != 1 || ids[0] == 1 {
		t.Fatalf("body still references original: %v", ids)
	}
	if objects.calls != 1 || len(objects.body) > WeChatMaxContentImageSize {
		t.Fatalf("normalized writes=%d bytes=%d", objects.calls, len(objects.body))
	}
	if len(store.assets) != 2 || store.assets[0].ObjectKey != "original.png" || store.assets[0].ByteSize != uint64(len(body)) || !store.assets[1].BodyEligible {
		t.Fatalf("assets = %#v", store.assets)
	}
	if got.CoverAssetID == nil || *got.CoverAssetID != 1 {
		t.Fatal("cover was replaced")
	}
	if ids := ReferencedDraftAssetIDs(*doc); len(ids) != 1 || ids[0] != 1 {
		t.Fatal("caller document was mutated")
	}
	got.Assets = store.assets
	if result := ValidateWeChatDraft(got); !result.Valid {
		t.Fatalf("preflight = %#v", result)
	}
	if _, err = service.Update(context.Background(), 1, 7, UpdateInput{Title: "标题", EditorDocument: got.EditorDocument, ThemeID: "clear-blue", CoverAssetID: &cover, ExpectedVersion: 2}); err != nil {
		t.Fatal(err)
	}
	if objects.calls != 1 {
		t.Fatal("saving normalized image wrote another copy")
	}
}

func TestUpdateKeepsEditingProgressWhenBodyImageCannotBeNormalized(t *testing.T) {
	for _, test := range []struct {
		name, mediaType string
		objects         *fakeAssetObjects
		wantWrites      int
	}{
		{"animation preserved", "image/gif", &fakeAssetObjects{original: validDraftGIF(t, 20, 10)}, 0},
		{"decode failure", "image/png", &fakeAssetObjects{original: []byte("invalid image")}, 0},
		{"read failure", "image/png", &fakeAssetObjects{openErr: errors.New("read unavailable")}, 0},
		{"write failure", "image/png", &fakeAssetObjects{original: append(validDraftPNG(t, 20, 10), make([]byte, WeChatMaxContentImageSize)...), err: errors.New("write unavailable")}, 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := &fakeEditorialStore{draft: Draft{ID: 1, Status: StatusEditing, CurrentVersion: 1}, assets: []DraftAsset{{ID: 1, DraftID: 1, ObjectKey: "original", MediaType: test.mediaType, ByteSize: WeChatMaxContentImageSize + 1, CoverEligible: true}}}
			objects := test.objects
			doc := savedDocument(t, `{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"正文"}]}]}`)
			doc.Content = append(doc.Content, imageNode(1))
			cover := uint64(1)
			got, err := New(store, &fakeArticleStore{}, nil, nil, objects, false).Update(context.Background(), 1, 7, UpdateInput{Title: "已编辑标题", EditorDocument: doc, ThemeID: "clear-blue", CoverAssetID: &cover, ExpectedVersion: 1})
			if err != nil {
				t.Fatal(err)
			}
			if got.CurrentVersion != 2 || got.Title != "已编辑标题" || got.ThemeID != "clear-blue" {
				t.Fatalf("progress not saved: %#v", got)
			}
			got.Assets = store.assets
			if result := ValidateWeChatDraft(got); result.Valid || !hasIssue(result, "CONTENT_IMAGE_TOO_LARGE") {
				t.Fatalf("preflight should locate oversized asset: %#v", result)
			}
			if objects.calls != test.wantWrites || store.createAssetCalls != 0 {
				t.Fatalf("failed conversion writes=%d records=%d", objects.calls, store.createAssetCalls)
			}
		})
	}
}

func TestNormalizeJPEGKeepsEXIFOrientation(t *testing.T) {
	// Little-endian TIFF IFD0 with Orientation=6 (90 degrees clockwise).
	exif := []byte{0xff, 0xe1, 0, 34, 'E', 'x', 'i', 'f', 0, 0, 'I', 'I', 42, 0, 8, 0, 0, 0, 1, 0, 0x12, 1, 3, 0, 1, 0, 0, 0, 6, 0, 0, 0, 0, 0, 0, 0}
	jpeg := validDraftJPEG(t, 20, 10)
	original := append(append(append([]byte{}, jpeg[:2]...), exif...), jpeg[2:]...)
	original = append(original, make([]byte, WeChatMaxContentImageSize)...)
	body, mediaType, err := normalizeContentImage(original, "image/jpeg")
	if err != nil {
		t.Fatal(err)
	}
	if mediaType != "image/jpeg" || !bytes.Contains(body, exif) {
		t.Fatal("EXIF orientation lost; browser would display rotated image incorrectly")
	}
	if _, err := InspectDraftImage(body, mediaType); err != nil {
		t.Fatal(err)
	}
}

func TestUpdateDoesNotNormalizeImagesForStaleVersion(t *testing.T) {
	store := &fakeEditorialStore{draft: Draft{ID: 1, Status: StatusEditing, CurrentVersion: 2}, assets: []DraftAsset{{ID: 1, DraftID: 1, ObjectKey: "original.png", MediaType: "image/png", ByteSize: WeChatMaxContentImageSize + 1, CoverEligible: true}}}
	objects := &fakeAssetObjects{original: append(validDraftPNG(t, 20, 10), make([]byte, WeChatMaxContentImageSize)...)}
	doc := savedDocument(t, `{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"正文"}]}]}`)
	doc.Content = append(doc.Content, imageNode(1))
	_, err := New(store, &fakeArticleStore{}, nil, nil, objects, false).Update(context.Background(), 1, 7, UpdateInput{Title: "标题", EditorDocument: doc, ThemeID: DefaultThemeID, ExpectedVersion: 1})
	if !errors.Is(err, ErrDraftVersionConflict) || objects.calls != 0 || store.createAssetCalls != 0 {
		t.Fatalf("error=%v writes=%d records=%d", err, objects.calls, store.createAssetCalls)
	}
}

func TestUpdateLegacyReviewedDraftSkipsNormalizationAndKeepsPreflight(t *testing.T) {
	store := &fakeEditorialStore{draft: Draft{ID: 1, Status: StatusApproved, CurrentVersion: 1}, assets: []DraftAsset{{ID: 1, DraftID: 1, ObjectKey: "original.png", MediaType: "image/png", ByteSize: WeChatMaxContentImageSize + 1, CoverEligible: true}}}
	objects := &fakeAssetObjects{}
	doc := savedDocument(t, `{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"正文"}]}]}`)
	doc.Content = append(doc.Content, imageNode(1))
	got, err := New(store, &fakeArticleStore{}, nil, nil, objects, false).Update(context.Background(), 1, 7, UpdateInput{Title: "标题", EditorDocument: doc, ThemeID: DefaultThemeID, ExpectedVersion: 1})
	if err != nil {
		t.Fatal(err)
	}
	if got.CurrentVersion != 2 || objects.calls != 0 {
		t.Fatalf("migration version=%d image writes=%d", got.CurrentVersion, objects.calls)
	}
	got.Assets = store.assets
	if result := ValidateWeChatDraft(got); result.Valid || !hasIssue(result, "CONTENT_IMAGE_TOO_LARGE") {
		t.Fatalf("preflight = %#v", result)
	}
}

func TestNormalizeContentImageCompressesRealOversizedPixels(t *testing.T) {
	source := image.NewNRGBA(image.Rect(0, 0, 1600, 1200))
	state := uint32(12345)
	for i := 0; i < len(source.Pix); i += 4 {
		for c := 0; c < 3; c++ {
			state ^= state << 13
			state ^= state >> 17
			state ^= state << 5
			source.Pix[i+c] = byte(state)
		}
		source.Pix[i+3] = 255
	}
	var original bytes.Buffer
	if err := png.Encode(&original, source); err != nil {
		t.Fatal(err)
	}
	if original.Len() <= WeChatMaxContentImageSize {
		t.Fatal("fixture must have oversized encoded pixels")
	}
	body, mediaType, err := normalizeContentImage(original.Bytes(), "image/png")
	if err != nil {
		t.Fatal(err)
	}
	if len(body) > WeChatMaxContentImageSize || mediaType != "image/jpeg" {
		t.Fatalf("normalized size=%d type=%s", len(body), mediaType)
	}
	info, err := InspectDraftImage(body, mediaType)
	if err != nil || info.Width == 0 || info.Height == 0 || info.Width > 1600 || info.Height > 1200 {
		t.Fatalf("normalized = %#v err=%v", info, err)
	}
}
