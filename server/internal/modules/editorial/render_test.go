package editorial

import (
	"strconv"
	"strings"
	"testing"

	"github.com/chenbb0128/weavepress/server/internal/modules/workspace"
)

func TestRenderArticleAndPreviewUseArchivedAssets(t *testing.T) {
	assetID := uint64(42)
	article := workspace.Article{Blocks: []workspace.Block{
		{Type: "heading", Level: 1, Text: "标题"},
		{Type: "paragraph", Text: "正文 <unsafe>"},
		{Type: "image", AssetID: &assetID, Alt: "配图"},
	}}

	content := RenderArticle(article)
	if !strings.Contains(content, `data-weavepress-asset-id="42"`) || !strings.Contains(content, `alt="配图"`) {
		t.Fatalf("content does not contain asset placeholder: %s", content)
	}
	if strings.Contains(content, "<unsafe>") {
		t.Fatalf("content was not escaped: %s", content)
	}
	ids, err := ReferencedAssetIDs(content)
	if err != nil || len(ids) != 1 || ids[0] != assetID {
		t.Fatalf("ReferencedAssetIDs() = %v, %v", ids, err)
	}
	preview := PreviewHTML(content, func(id uint64) string { return "/media/assets/" + strconv.FormatUint(id, 10) })
	if !strings.Contains(preview, `src="/media/assets/42"`) {
		t.Fatalf("preview does not contain signed media URL: %s", preview)
	}
}

func TestSanitizeHTMLDropsScriptsAndExternalImageSources(t *testing.T) {
	content := SanitizeHTML(`<script>alert(1)</script><p>ok</p><img src="https://tracker.invalid/x" onerror="alert(1)" data-weavepress-asset-id="7">`)
	if strings.Contains(content, "script") || strings.Contains(content, "onerror") || strings.Contains(content, "tracker.invalid") {
		t.Fatalf("unsafe HTML remained: %s", content)
	}
	if !strings.Contains(content, `data-weavepress-asset-id="7"`) {
		t.Fatalf("asset placeholder was removed: %s", content)
	}
}

func TestReferencedAssetIDsRejectsUnarchivedImages(t *testing.T) {
	if _, err := ReferencedAssetIDs(`<p>正文</p><img alt="外部图片">`); err == nil {
		t.Fatal("image without archived asset placeholder was accepted")
	}
}
