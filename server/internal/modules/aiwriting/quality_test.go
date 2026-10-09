package aiwriting

import (
	"context"
	"strings"
	"testing"

	"github.com/chenbb0128/weavepress/server/internal/modules/workspace"
	"github.com/chenbb0128/weavepress/server/internal/platform/llm"
)

func TestQualityRulesLocateReuseAcrossWhitespaceAndSplitBlocks(t *testing.T) {
	sourceText := "这段原文介绍了一项面向幼童的保护措施并详细列出了执行条件以及相关部门在不同情况下需要承担的职责。"
	source := BuildSourceDocument(workspace.Article{Title: "保护幼童！", PlainText: sourceText})
	input := QualityInput{Title: "保护 幼童。", Blocks: []QualityBlock{
		{Type: "paragraph", Text: sourceText[:30]},
		{Type: "paragraph", Text: "\n" + sourceText[30:]},
	}}
	report := CheckQuality(source, input)
	if len(report.Issues) < 2 || report.Issues[0].Code != "TITLE_DUPLICATE" {
		t.Fatalf("report=%#v", report)
	}
	var found bool
	for _, issue := range report.Issues {
		if issue.Code == "SOURCE_REUSE" && issue.BlockIndex != nil && issue.SourceBlockID == "B1" && strings.Contains(issue.Message, "48 字") {
			found = true
		}
	}
	if !found {
		t.Fatalf("split/whitespace reuse not located: %#v", report)
	}
}

func TestQualityRulesExcludeQuotesAndFlagExcessQuotesAndUnattributedVoice(t *testing.T) {
	source := BuildSourceDocument(workspace.Article{Title: "原文", PlainText: strings.Repeat("正文", 30)})
	input := QualityInput{Title: "新标题", Blocks: []QualityBlock{
		{Type: "quote", Text: source.PlainText},
		{Type: "paragraph", Text: "作者自述：我住过医院。"},
		{Type: "quote", Text: "第二处"},
		{Type: "paragraph", Text: "哥前几天住院了。"},
		{Type: "quote", Text: "第三处"},
	}}
	report := CheckQuality(source, input)
	if len(report.Issues) != 2 || report.Issues[0].Code != "AUTHOR_VOICE" || report.Issues[0].Severity != "warning" || *report.Issues[0].BlockIndex != 3 || report.Issues[1].Code != "QUOTE_LIMIT" || *report.Issues[1].BlockIndex != 4 {
		t.Fatalf("report=%#v", report)
	}
}

func TestQualityRulesFindReuseAcrossShortSourceParagraphs(t *testing.T) {
	first := "文章先介绍保护幼童措施并列出了相关部门职责。"
	second := "随后说明执行条件及当事人在不同情况下可以寻求的帮助。"
	source := BuildSourceDocument(workspace.Article{Blocks: []workspace.Block{{Type: "paragraph", Text: first}, {Type: "paragraph", Text: second}}})
	report := CheckQuality(source, QualityInput{Title: "新标题", Blocks: []QualityBlock{{Type: "paragraph", Text: first}, {Type: "paragraph", Text: second}}})
	if len(report.Issues) != 2 || report.Issues[0].Code != "SOURCE_REUSE" || report.Issues[0].SourceBlockID != "B1" || report.Issues[1].SourceBlockID != "B2" || report.Issues[1].SourceExcerpt != second {
		t.Fatalf("report=%#v", report)
	}
}

func TestQualityRulesEmptySeparatorsCannotHideConsecutiveQuotes(t *testing.T) {
	for _, between := range []QualityBlock{{Type: "paragraph", Text: " \n"}, {Type: "separator"}} {
		report := CheckQuality(SourceDocument{}, QualityInput{Title: "新标题", Blocks: []QualityBlock{{Type: "quote", Text: "甲"}, between, {Type: "quote", Text: "乙"}}})
		if len(report.Issues) != 1 || report.Issues[0].Code != "QUOTE_LIMIT" || *report.Issues[0].BlockIndex != 2 {
			t.Fatalf("report=%#v", report)
		}
	}
}

func TestQualityReuseEvidenceStaysLocalAndBounded(t *testing.T) {
	text := strings.Repeat("原文内容", 15000)
	source := BuildSourceDocument(workspace.Article{PlainText: text})
	input := QualityInput{Title: "新标题"}
	for range 5000 {
		input.Blocks = append(input.Blocks, QualityBlock{Type: "paragraph", Text: "原文内容原文内容原文内容"})
	}
	report := CheckQuality(source, input)
	size := 0
	for _, issue := range report.Issues {
		size += len([]rune(issue.SourceExcerpt))
	}
	if size > 60000 || len(report.Issues) != 5000 {
		t.Fatalf("evidence runes=%d issues=%d", size, len(report.Issues))
	}
}

func TestQualitySemanticTitleUsesTitleEvidence(t *testing.T) {
	article := readyAIArticle()
	article.Title = "3人参与活动"
	provider := &fakeAIProvider{responses: []llm.Response{{Content: `{"issues":[{"code":"FACT_CHANGED","message":"标题人数改变","blockIndex":-1,"sourceBlockId":"TITLE","excerpt":"30人","sourceExcerpt":"3人"}]}`}}}
	service := New(&fakeAIStore{}, &fakeAIArticles{article: article}, nil, provider, testAIConfig())
	report, err := service.Quality(context.Background(), article.ID, QualityInput{Title: "30人参与活动", Blocks: []QualityBlock{}, Semantic: true})
	if err != nil || !report.SemanticChecked || len(report.Issues) != 1 || *report.Issues[0].BlockIndex != -1 {
		t.Fatalf("report=%#v err=%v", report, err)
	}
}

func TestQualityRewriteQuoteMustBecomeParaphrase(t *testing.T) {
	article := readyAIArticle()
	input := RewriteInput{QualityInput: QualityInput{Title: "新标题", Blocks: []QualityBlock{{Type: "quote", Text: "逐字引用"}}}, TargetIndex: 0}
	for _, kind := range []string{"quote", "paragraph"} {
		provider := &fakeAIProvider{responses: []llm.Response{{Content: `{"blocks":[{"index":0,"type":"` + kind + `","text":"原作者说明了这一事实。"}]}`}}}
		service := New(&fakeAIStore{}, &fakeAIArticles{article: article}, nil, provider, testAIConfig())
		result, err := service.Rewrite(context.Background(), article.ID, input)
		if kind == "quote" && err == nil {
			t.Fatal("accepted altered direct quotation")
		}
		if kind == "paragraph" && (err != nil || result.Type != "paragraph") {
			t.Fatalf("result=%#v err=%v", result, err)
		}
	}
}

func TestQualityPatchRejectsUnrequestedChangesAndKeepsUnchangedBlocks(t *testing.T) {
	original := GenerationOutput{Title: "原标题", Digest: "摘要", Blocks: []GeneratedBlock{{Type: "paragraph", Text: "需要改写"}, {Type: "paragraph", Text: "保留这段"}}}
	index := 0
	issues := []QualityIssue{{Code: "SOURCE_REUSE", BlockIndex: &index}}
	for _, raw := range []string{
		`{"title":"擅改标题","blocks":[]}`,
		`{"blocks":[{"index":1,"type":"paragraph","text":"擅改正常段落"}]}`,
		`{"blocks":[{"index":0,"type":"image","text":"新图"}]}`,
		`{"blocks":[{"index":0,"type":"paragraph","text":"甲"},{"index":0,"type":"paragraph","text":"乙"}]}`,
	} {
		if _, err := applyQualityPatch(original, issues, raw); err == nil {
			t.Fatalf("accepted patch: %s", raw)
		}
	}
	patched, err := applyQualityPatch(original, issues, `{"blocks":[{"index":0,"type":"paragraph","text":"已经重新表达"}]}`)
	if err != nil || patched.Title != original.Title || patched.Digest != original.Digest || patched.Blocks[0].Text != "已经重新表达" || patched.Blocks[1].Text != "保留这段" || original.Blocks[0].Text != "需要改写" {
		t.Fatalf("patched=%#v original=%#v err=%v", patched, original, err)
	}
}

func TestQualitySemanticReviewRequiresAnchoredEvidence(t *testing.T) {
	article := readyAIArticle()
	input := QualityInput{Title: "新标题", Blocks: []QualityBlock{{Type: "paragraph", Text: "哥前几天住院了。"}}, Semantic: true, Faithful: true}
	for _, raw := range []string{
		`{"issues":[{"code":"AUTHOR_ATTRIBUTION","message":"未归因","blockIndex":0,"sourceBlockId":"B1","excerpt":"哥前几天住院了","sourceExcerpt":"原文事实"}]}`,
		`{"issues":[{"code":"FACT_CHANGED","message":"变化","blockIndex":7,"sourceBlockId":"B1","excerpt":"伪造","sourceExcerpt":"原文事实"}]}`,
	} {
		provider := &fakeAIProvider{responses: []llm.Response{{Content: raw}}}
		service := New(&fakeAIStore{}, &fakeAIArticles{article: article}, nil, provider, testAIConfig())
		report, err := service.Quality(context.Background(), article.ID, input)
		if strings.Contains(raw, `"blockIndex":7`) {
			if err == nil {
				t.Fatal("semantic output without actual evidence accepted")
			}
		} else if err != nil || !report.SemanticChecked || len(report.Issues) != 2 || report.Issues[1].Code != "AUTHOR_ATTRIBUTION" {
			t.Fatalf("report=%#v err=%v", report, err)
		}
	}
}
