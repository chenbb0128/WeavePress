package editorial

import (
	"errors"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func TestConvertLegacyHTML(t *testing.T) {
	mapping := map[uint64]uint64{11: 101}
	result, err := ConvertLegacyHTML(`<h2>标题</h2><p>正文<strong>加粗</strong></p><figure><img data-weavepress-asset-id="11" alt="图"><figcaption>说明</figcaption></figure><script>alert(1)</script>`, mapping)
	if err != nil {
		t.Fatal(err)
	}
	if result.Document.Content[2].Type != "image" {
		t.Fatalf("node = %#v", result.Document.Content[2])
	}
	if len(result.Warnings) == 0 {
		t.Fatal("expected dropped-node warning")
	}
	ids := ReferencedDraftAssetIDs(result.Document)
	if !reflect.DeepEqual(ids, []uint64{101}) {
		t.Fatalf("ids = %v", ids)
	}
}

func TestConvertLegacyHTMLPreservesSupportedStructure(t *testing.T) {
	result, err := ConvertLegacyHTML(`<ol><li>第一项<br>续行</li></ol><blockquote><p><a href="https://example.com" title="示例"><em><u>引用</u></em></a></p></blockquote>`, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := result.Document.Content[0].Type; got != "orderedList" {
		t.Fatalf("first node type = %q", got)
	}
	item := result.Document.Content[0].Content[0]
	if item.Type != "listItem" || item.Content[0].Type != "paragraph" || item.Content[0].Content[1].Type != "hardBreak" {
		t.Fatalf("list item = %#v", item)
	}
	quote := result.Document.Content[1]
	if quote.Type != "blockquote" || len(quote.Content) != 1 {
		t.Fatalf("quote = %#v", quote)
	}
	marks := quote.Content[0].Content[0].Marks
	if got := []string{marks[0].Type, marks[1].Type, marks[2].Type}; !reflect.DeepEqual(got, []string{"link", "italic", "underline"}) {
		t.Fatalf("marks = %#v", marks)
	}
}

func TestConvertLegacyHTMLFallsBackAndDropsUnmappedImages(t *testing.T) {
	result, err := ConvertLegacyHTML(`<section><div>保留文字</div></section><img data-weavepress-asset-id="99" alt="缺图">`, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Document.Content) != 1 || result.Document.Content[0].Type != "paragraph" || result.Document.Content[0].Content[0].Text != "保留文字" {
		t.Fatalf("document = %#v", result.Document)
	}
	if len(result.Warnings) < 2 {
		t.Fatalf("warnings = %v", result.Warnings)
	}
}

func TestConvertLegacyHTMLRejectsContentWithoutBody(t *testing.T) {
	if _, err := ConvertLegacyHTML(`<script>alert(1)</script><img data-weavepress-asset-id="99">`, nil); !errors.Is(err, ErrLegacyConvertFailed) {
		t.Fatalf("error = %v", err)
	}
}

func TestPreviewHTMLSupportsDraftAndLegacyPlaceholders(t *testing.T) {
	preview := PreviewHTML(`<img data-weavepress-draft-asset-id="7"><img data-weavepress-asset-id="8">`, func(id uint64) string {
		return "/media/drafts/" + strconv.FormatUint(id, 10)
	})
	if !strings.Contains(preview, `src="/media/drafts/7"`) || !strings.Contains(preview, `src="/media/drafts/8"`) {
		t.Fatalf("preview = %s", preview)
	}
}
