package editorial

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestThemeRegistryAndRender(t *testing.T) {
	ids := []string{"minimal-business", "clear-blue", "natural-green", "warm-lifestyle", "elegant-chinese", "vibrant-brand"}
	if themes := ListThemes(); len(themes) != len(ids) {
		t.Fatalf("themes = %d", len(themes))
	} else {
		for index, id := range ids {
			if themes[index].ID != id || themes[index].Version != 1 {
				t.Fatalf("theme[%d] = %+v", index, themes[index])
			}
		}
	}

	doc, err := ParseDocument(json.RawMessage(`{"type":"doc","content":[{"type":"heading","attrs":{"level":2},"content":[{"type":"text","text":"<标题>"}]},{"type":"image","attrs":{"draftAssetId":18,"width":75,"align":"center","alt":"\"图\"","caption":"说明"}}]}`))
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range ids {
		theme, err := ResolveTheme(id, 1)
		if err != nil {
			t.Fatal(err)
		}
		first, err := RenderDocument(doc, theme)
		if err != nil {
			t.Fatal(err)
		}
		second, err := RenderDocument(doc, theme)
		if err != nil {
			t.Fatal(err)
		}
		if first != second {
			t.Fatalf("theme %s is not deterministic", id)
		}
		if strings.Contains(first, "<标题>") || !strings.Contains(first, `data-weavepress-draft-asset-id="18"`) {
			t.Fatalf("unsafe output: %s", first)
		}
		if !strings.Contains(first, `alt="&#34;图&#34;"`) {
			t.Fatalf("image alt was not escaped: %s", first)
		}
	}
}

func TestResolveThemeRequiresExactKnownVersion(t *testing.T) {
	for _, input := range []struct {
		id      string
		version uint
	}{
		{id: "missing", version: 1},
		{id: "minimal-business", version: 2},
	} {
		if _, err := ResolveTheme(input.id, input.version); !errors.Is(err, ErrThemeNotFound) {
			t.Fatalf("ResolveTheme(%q, %d) error = %v", input.id, input.version, err)
		}
	}
}

func TestRenderDocumentEscapesTextAndLinkAttributes(t *testing.T) {
	doc, err := ParseDocument(json.RawMessage(`{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"<正文>","marks":[{"type":"link","attrs":{"href":"https://example.test/?a=1&b=2","title":"\"标题\""}}]}]}]}`))
	if err != nil {
		t.Fatal(err)
	}
	theme, err := ResolveTheme("minimal-business", 1)
	if err != nil {
		t.Fatal(err)
	}
	output, err := RenderDocument(doc, theme)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output, "<正文>") || strings.Contains(output, `title="\"标题\""`) {
		t.Fatalf("user input was not escaped: %s", output)
	}
	want := `href="https://example.test/?a=1&amp;b=2" title="&#34;标题&#34;" target="_blank" rel="nofollow noopener noreferrer"`
	if !strings.Contains(output, want) || !strings.Contains(output, `>&lt;正文&gt;</a>`) {
		t.Fatalf("link was not safely rendered: %s", output)
	}
}

func TestRenderDocumentRejectsInvalidDocument(t *testing.T) {
	theme, err := ResolveTheme("minimal-business", 1)
	if err != nil {
		t.Fatal(err)
	}
	_, err = RenderDocument(Document{Type: "doc", Content: []Node{{Type: "script"}}}, theme)
	if !errors.Is(err, ErrDocumentInvalid) {
		t.Fatalf("RenderDocument() error = %v", err)
	}
}

func TestRenderDocumentUsesRegisteredThemeStyles(t *testing.T) {
	doc, err := ParseDocument(json.RawMessage(`{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"正文"}]}]}`))
	if err != nil {
		t.Fatal(err)
	}
	output, err := RenderDocument(doc, Theme{ThemeSummary: ThemeSummary{ID: "minimal-business", Version: 1}, BodyStyle: `background:url(javascript:alert(1))`})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output, "javascript:") || !strings.Contains(output, `color:#262626`) {
		t.Fatalf("rendered unregistered theme style: %s", output)
	}
}
