package aiwriting

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/chenbb0128/weavepress/server/internal/modules/workspace"
)

func TestBuildSourceDocumentAssignsStableBlockIDsWithoutTruncating(t *testing.T) {
	assetID := uint64(7)
	article := workspace.Article{
		ID: 12,
		Blocks: []workspace.Block{
			{Type: "heading", Level: 2, Text: "第一节"},
			{Type: "paragraph", Text: strings.Repeat("甲", 60_001)},
			{Type: "image", AssetID: &assetID, Alt: "图示"},
		},
	}

	source := BuildSourceDocument(article)

	if len(source.Blocks) != 3 || source.Blocks[0].ID != "B1" || source.Blocks[1].ID != "B2" || source.Blocks[2].ID != "B3" {
		t.Fatalf("blocks = %#v", source.Blocks)
	}
	if source.BlockByID["B3"].AssetID == nil || *source.BlockByID["B3"].AssetID != assetID {
		t.Fatalf("blockByID = %#v", source.BlockByID)
	}
	if got := len([]rune(source.Blocks[1].Text)); got != 60_001 {
		t.Fatalf("block text runes = %d", got)
	}
}

func TestPlainTextSourceUsesOneFallbackBlockAcrossPromptAndValidation(t *testing.T) {
	article := workspace.Article{ID: 12, PlainText: "纯文本来源事实"}

	source := BuildSourceDocument(article)

	if len(source.Blocks) != 1 || source.Blocks[0].ID != "B1" || source.Blocks[0].Type != "paragraph" || source.Blocks[0].Text != article.PlainText {
		t.Fatalf("blocks = %#v", source.Blocks)
	}
	if block, ok := source.BlockByID["B1"]; !ok || block != source.Blocks[0] {
		t.Fatalf("blockByID = %#v", source.BlockByID)
	}
	messages := BuildAnalysisMessages(source)
	if !strings.Contains(messages[1].Content, `"id":"B1"`) || strings.Count(messages[1].Content, article.PlainText) != 1 {
		t.Fatalf("prompt = %s", messages[1].Content)
	}
	output := AnalysisOutput{
		Summary: "摘要",
		Facts: []Fact{{
			ID:             "F1",
			Text:           "来源事实",
			SourceBlockIDs: []string{"B1"},
			Confidence:     "high",
		}},
		Angles: []Angle{
			{ID: "A1", Title: "角度一", Thesis: "论点一", Outline: []string{"提纲一"}},
			{ID: "A2", Title: "角度二", Thesis: "论点二", Outline: []string{"提纲二"}},
			{ID: "A3", Title: "角度三", Thesis: "论点三", Outline: []string{"提纲三"}},
		},
	}
	if err := ValidateAnalysis(source, output); err != nil {
		t.Fatalf("validate analysis: %v", err)
	}
}

func TestValidateGenerationParams(t *testing.T) {
	valid := GenerationParams{
		AngleID:                "A1",
		Audience:               "技术团队",
		Tone:                   "professional",
		TargetWords:            1_000,
		AdditionalInstructions: strings.Repeat("补", 500),
		IdempotencyKey:         "request-12345678",
	}
	if err := ValidateGenerationParams(valid); err != nil {
		t.Fatalf("valid params: %v", err)
	}

	tests := []struct {
		name   string
		mutate func(*GenerationParams)
	}{
		{name: "angle required", mutate: func(p *GenerationParams) { p.AngleID = " " }},
		{name: "audience required", mutate: func(p *GenerationParams) { p.Audience = "" }},
		{name: "audience too long", mutate: func(p *GenerationParams) { p.Audience = strings.Repeat("众", 101) }},
		{name: "tone", mutate: func(p *GenerationParams) { p.Tone = "casual" }},
		{name: "target words low", mutate: func(p *GenerationParams) { p.TargetWords = 299 }},
		{name: "target words high", mutate: func(p *GenerationParams) { p.TargetWords = 5_001 }},
		{name: "instructions too long", mutate: func(p *GenerationParams) { p.AdditionalInstructions = strings.Repeat("补", 501) }},
		{name: "key too short", mutate: func(p *GenerationParams) { p.IdempotencyKey = "1234567" }},
		{name: "key too long", mutate: func(p *GenerationParams) { p.IdempotencyKey = strings.Repeat("a", 129) }},
		{name: "key non ascii", mutate: func(p *GenerationParams) { p.IdempotencyKey = "请求-key-1234" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			params := valid
			tt.mutate(&params)
			if err := ValidateGenerationParams(params); !errors.Is(err, ErrInvalidParameters) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestValidateGenerationRequestRejectsUnknownAnalysisAngle(t *testing.T) {
	params := validGenerationParams()
	params.AngleID = "A999"

	err := ValidateGenerationRequest(Analysis{AnalysisOutput: validAnalysisOutput()}, params)

	if !errors.Is(err, ErrInvalidParameters) {
		t.Fatalf("error = %v", err)
	}
}

func TestValidateGenerationRequestAcceptsAnalysisAngle(t *testing.T) {
	if err := ValidateGenerationRequest(Analysis{AnalysisOutput: validAnalysisOutput()}, validGenerationParams()); err != nil {
		t.Fatalf("error = %v", err)
	}
}

func TestValidateAnalysisRejectsUnknownSourceBlock(t *testing.T) {
	source := analysisSource()
	output := validAnalysisOutput()
	output.Facts[0].SourceBlockIDs = []string{"B9"}

	if err := ValidateAnalysis(source, output); !errors.Is(err, ErrSourceReferenceInvalid) {
		t.Fatalf("error = %v", err)
	}
}

func TestValidateAnalysisRejectsQuoteMissingFromSource(t *testing.T) {
	source := analysisSource()
	output := validAnalysisOutput()
	output.Quotes[0].Text = "并不存在"

	if err := ValidateAnalysis(source, output); !errors.Is(err, ErrQuoteMismatch) {
		t.Fatalf("error = %v", err)
	}
}

func TestValidateAnalysisRequiresExactlyThreeAngles(t *testing.T) {
	output := validAnalysisOutput()
	output.Angles = output.Angles[:2]

	if err := ValidateAnalysis(analysisSource(), output); !errors.Is(err, ErrOutputInvalid) {
		t.Fatalf("error = %v", err)
	}
}

func TestValidateAnalysisRejectsInvalidConfidence(t *testing.T) {
	output := validAnalysisOutput()
	output.Facts[0].Confidence = "certain"

	if err := ValidateAnalysis(analysisSource(), output); !errors.Is(err, ErrOutputInvalid) {
		t.Fatalf("error = %v", err)
	}
}

func TestValidateAnalysisRejectsDuplicateOrMalformedEntityIDs(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*AnalysisOutput)
	}{
		{name: "duplicate", mutate: func(o *AnalysisOutput) { o.Facts = append(o.Facts, o.Facts[0]) }},
		{name: "wrong prefix", mutate: func(o *AnalysisOutput) { o.Facts[0].ID = "V9" }},
		{name: "leading zero", mutate: func(o *AnalysisOutput) { o.Angles[0].ID = "A01" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output := validAnalysisOutput()
			tt.mutate(&output)
			if err := ValidateAnalysis(analysisSource(), output); !errors.Is(err, ErrOutputInvalid) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestValidateAnalysisRejectsEmptyRequiredText(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*AnalysisOutput)
	}{
		{name: "summary", mutate: func(o *AnalysisOutput) { o.Summary = " " }},
		{name: "fact", mutate: func(o *AnalysisOutput) { o.Facts[0].Text = "" }},
		{name: "viewpoint holder", mutate: func(o *AnalysisOutput) { o.Viewpoints[0].Holder = "" }},
		{name: "risk sources", mutate: func(o *AnalysisOutput) { o.Risks[0].SourceBlockIDs = nil }},
		{name: "angle title", mutate: func(o *AnalysisOutput) { o.Angles[0].Title = "" }},
		{name: "angle thesis", mutate: func(o *AnalysisOutput) { o.Angles[0].Thesis = "" }},
		{name: "angle outline", mutate: func(o *AnalysisOutput) { o.Angles[0].Outline = nil }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output := validAnalysisOutput()
			tt.mutate(&output)
			if err := ValidateAnalysis(analysisSource(), output); !errors.Is(err, ErrOutputInvalid) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestValidateGenerationRejectsUnknownFact(t *testing.T) {
	output := validGenerationOutput()
	output.Blocks[0].FactIDs = []string{"F9"}

	if err := ValidateGeneration(generationSource(), validAnalysis(), generationAssets(), output); !errors.Is(err, ErrSourceReferenceInvalid) {
		t.Fatalf("error = %v", err)
	}
}

func TestValidateGenerationRejectsUnknownQuote(t *testing.T) {
	output := validGenerationOutput()
	output.Blocks[1].QuoteID = "Q9"

	if err := ValidateGeneration(generationSource(), validAnalysis(), generationAssets(), output); !errors.Is(err, ErrSourceReferenceInvalid) {
		t.Fatalf("error = %v", err)
	}
}

func TestValidateGenerationRejectsQuoteTextMismatch(t *testing.T) {
	output := validGenerationOutput()
	output.Blocks[1].Text = "不一致的引用"

	if err := ValidateGeneration(generationSource(), validAnalysis(), generationAssets(), output); !errors.Is(err, ErrQuoteMismatch) {
		t.Fatalf("error = %v", err)
	}
}

func TestValidateGenerationRejectsCrossArticleOrIncompleteAsset(t *testing.T) {
	tests := []struct {
		name  string
		asset workspace.Asset
	}{
		{name: "cross article", asset: workspace.Asset{ID: 7, ArticleID: 99, DownloadStatus: "completed"}},
		{name: "not completed", asset: workspace.Asset{ID: 7, ArticleID: 12, DownloadStatus: "pending"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := ValidateGeneration(generationSource(), validAnalysis(), map[uint64]workspace.Asset{7: tt.asset}, validGenerationOutput()); !errors.Is(err, ErrAssetInvalid) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestValidateGenerationRejectsAssetMapKeyIDMismatch(t *testing.T) {
	assets := map[uint64]workspace.Asset{
		7: {ID: 8, ArticleID: 12, DownloadStatus: "completed"},
	}

	err := ValidateGeneration(generationSource(), validAnalysis(), assets, validGenerationOutput())

	if !errors.Is(err, ErrAssetInvalid) {
		t.Fatalf("error = %v", err)
	}
}

func TestValidateGenerationRejectsInvalidBlocks(t *testing.T) {
	tests := []struct {
		name  string
		block GeneratedBlock
	}{
		{name: "unknown type", block: GeneratedBlock{Type: "html", Text: "正文"}},
		{name: "heading level", block: GeneratedBlock{Type: "heading", Level: 1, Text: "标题"}},
		{name: "empty heading", block: GeneratedBlock{Type: "heading", Level: 2}},
		{name: "empty paragraph", block: GeneratedBlock{Type: "paragraph"}},
		{name: "empty list", block: GeneratedBlock{Type: "list"}},
		{name: "empty list item", block: GeneratedBlock{Type: "list", Items: []string{" "}}},
		{name: "image without asset", block: GeneratedBlock{Type: "image", Alt: "图"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output := validGenerationOutput()
			output.Blocks = []GeneratedBlock{tt.block}
			if err := ValidateGeneration(generationSource(), validAnalysis(), generationAssets(), output); !errors.Is(err, ErrOutputInvalid) && !errors.Is(err, ErrAssetInvalid) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestValidateGenerationChecksTitleDigestAndBlocks(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*GenerationOutput)
	}{
		{name: "title required", mutate: func(o *GenerationOutput) { o.Title = "" }},
		{name: "title max", mutate: func(o *GenerationOutput) { o.Title = strings.Repeat("题", 256) }},
		{name: "digest required", mutate: func(o *GenerationOutput) { o.Digest = "" }},
		{name: "digest max", mutate: func(o *GenerationOutput) { o.Digest = strings.Repeat("摘", 256) }},
		{name: "blocks required", mutate: func(o *GenerationOutput) { o.Blocks = nil }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output := validGenerationOutput()
			tt.mutate(&output)
			if err := ValidateGeneration(generationSource(), validAnalysis(), generationAssets(), output); !errors.Is(err, ErrOutputInvalid) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestValidateGenerationRejectsEightyRuneOverlap(t *testing.T) {
	original := strings.Repeat("甲", 80) + "原文结尾"
	output := GenerationOutput{Title: "新标题", Digest: "摘要", Blocks: []GeneratedBlock{{Type: "paragraph", Text: "引入" + strings.Repeat("甲", 80)}}}

	err := ValidateGeneration(SourceDocument{PlainText: original}, Analysis{}, nil, output)

	if !errors.Is(err, ErrExcessiveSourceOverlap) {
		t.Fatalf("error = %v", err)
	}
}

func TestValidateGenerationAllowsSeventyNineRuneOverlap(t *testing.T) {
	original := strings.Repeat("甲", 79) + "原文结尾"
	output := GenerationOutput{Title: "新标题", Digest: "摘要", Blocks: []GeneratedBlock{{Type: "paragraph", Text: "引入" + strings.Repeat("甲", 79)}}}

	if err := ValidateGeneration(SourceDocument{PlainText: original}, Analysis{}, nil, output); err != nil {
		t.Fatalf("error = %v", err)
	}
}

func TestValidateGenerationDoesNotJoinSourceBlocksForOverlap(t *testing.T) {
	left := strings.Repeat("甲", 40)
	right := strings.Repeat("乙", 40)
	source := SourceDocument{Blocks: []SourceBlock{{ID: "B1", Text: left}, {ID: "B2", Text: right}}}
	output := GenerationOutput{Title: "新标题", Digest: "摘要", Blocks: []GeneratedBlock{{Type: "paragraph", Text: left + right}}}

	if err := ValidateGeneration(source, Analysis{}, nil, output); err != nil {
		t.Fatalf("error = %v", err)
	}
}

func TestValidateGenerationChecksPlainTextAlongsideSourceBlocks(t *testing.T) {
	copied := strings.Repeat("原", 80)
	source := SourceDocument{
		PlainText: copied,
		Blocks:    []SourceBlock{{ID: "B1", Text: "结构化块与纯文本内容不同"}},
	}
	output := GenerationOutput{Title: "新标题", Digest: "摘要", Blocks: []GeneratedBlock{{Type: "paragraph", Text: copied}}}

	err := ValidateGeneration(source, Analysis{}, nil, output)

	if !errors.Is(err, ErrExcessiveSourceOverlap) {
		t.Fatalf("error = %v", err)
	}
}

func TestValidateGenerationDoesNotJoinGeneratedBlocksForOverlap(t *testing.T) {
	left := strings.Repeat("甲", 40)
	right := strings.Repeat("乙", 40)
	source := SourceDocument{PlainText: left + right}
	output := GenerationOutput{
		Title:  "新标题",
		Digest: "摘要",
		Blocks: []GeneratedBlock{
			{Type: "paragraph", Text: left},
			{Type: "paragraph", Text: right},
		},
	}

	if err := ValidateGeneration(source, Analysis{}, nil, output); err != nil {
		t.Fatalf("error = %v", err)
	}
}

func TestValidateGenerationAllowsLongDirectQuote(t *testing.T) {
	quoted := strings.Repeat("原", 80)
	source := SourceDocument{PlainText: quoted}
	analysis := Analysis{AnalysisOutput: AnalysisOutput{Quotes: []Quote{{ID: "Q1", Text: quoted, SourceBlockID: "B1"}}}}
	output := GenerationOutput{Title: "新标题", Digest: "摘要", Blocks: []GeneratedBlock{{Type: "quote", Text: quoted, QuoteID: "Q1"}}}

	if err := ValidateGeneration(source, analysis, nil, output); err != nil {
		t.Fatalf("error = %v", err)
	}
}

func TestDecodeOutputsRequireOneStrictJSONObject(t *testing.T) {
	validAnalysisJSON, err := json.Marshal(validAnalysisOutput())
	if err != nil {
		t.Fatal(err)
	}
	validGenerationJSON, err := json.Marshal(validGenerationOutput())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeAnalysisOutput(string(validAnalysisJSON)); err != nil {
		t.Fatalf("decode analysis: %v", err)
	}
	if _, err := DecodeGenerationOutput(string(validGenerationJSON)); err != nil {
		t.Fatalf("decode generation: %v", err)
	}

	tests := []struct {
		name string
		raw  string
		kind string
	}{
		{name: "syntax", raw: `{`, kind: "analysis"},
		{name: "markdown fence", raw: "```json\n{}\n```", kind: "analysis"},
		{name: "leading text", raw: `result: {}`, kind: "analysis"},
		{name: "trailing object", raw: `{} {}`, kind: "analysis"},
		{name: "unknown analysis field", raw: `{"summary":"x","extra":true}`, kind: "analysis"},
		{name: "unknown generation field", raw: `{"title":"x","extra":true}`, kind: "generation"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var err error
			if tt.kind == "analysis" {
				_, err = DecodeAnalysisOutput(tt.raw)
			} else {
				_, err = DecodeGenerationOutput(tt.raw)
			}
			if !errors.Is(err, ErrOutputInvalid) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestDecodeAnalysisOutputRequiresNonNullArrayFields(t *testing.T) {
	valid := map[string]any{
		"summary":    "摘要",
		"facts":      []any{},
		"viewpoints": []any{},
		"quotes":     []any{},
		"risks":      []any{},
		"angles":     []any{},
	}

	encoded, err := json.Marshal(valid)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeAnalysisOutput(string(encoded)); err != nil {
		t.Fatalf("empty arrays should decode: %v", err)
	}

	for _, field := range []string{"facts", "viewpoints", "quotes", "risks", "angles"} {
		t.Run(field+" missing", func(t *testing.T) {
			input := cloneJSONMap(valid)
			delete(input, field)
			raw, marshalErr := json.Marshal(input)
			if marshalErr != nil {
				t.Fatal(marshalErr)
			}
			if _, err := DecodeAnalysisOutput(string(raw)); !errors.Is(err, ErrOutputInvalid) {
				t.Fatalf("error = %v", err)
			}
		})
		t.Run(field+" null", func(t *testing.T) {
			input := cloneJSONMap(valid)
			input[field] = nil
			raw, marshalErr := json.Marshal(input)
			if marshalErr != nil {
				t.Fatal(marshalErr)
			}
			if _, err := DecodeAnalysisOutput(string(raw)); !errors.Is(err, ErrOutputInvalid) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func cloneJSONMap(source map[string]any) map[string]any {
	result := make(map[string]any, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}

func analysisSource() SourceDocument {
	blocks := []SourceBlock{{ID: "B1", Type: "paragraph", Text: "原文事实和逐字引用"}, {ID: "B2", Type: "paragraph", Text: "作者认为需要谨慎"}}
	return SourceDocument{Blocks: blocks, BlockByID: map[string]SourceBlock{"B1": blocks[0], "B2": blocks[1]}}
}

func validAnalysisOutput() AnalysisOutput {
	return AnalysisOutput{
		Summary:    "摘要",
		Facts:      []Fact{{ID: "F1", Text: "原文事实", SourceBlockIDs: []string{"B1"}, Confidence: "high"}},
		Viewpoints: []Viewpoint{{ID: "V1", Text: "需要谨慎", Holder: "作者", SourceBlockIDs: []string{"B2"}}},
		Quotes:     []Quote{{ID: "Q1", Text: "逐字引用", SourceBlockID: "B1"}},
		Risks:      []Risk{{ID: "R1", Text: "仍需核验", SourceBlockIDs: []string{"B2"}}},
		Angles: []Angle{
			{ID: "A1", Title: "角度一", Thesis: "论点一", Outline: []string{"开头", "结尾"}},
			{ID: "A2", Title: "角度二", Thesis: "论点二", Outline: []string{"开头", "结尾"}},
			{ID: "A3", Title: "角度三", Thesis: "论点三", Outline: []string{"开头", "结尾"}},
		},
	}
}

func generationSource() SourceDocument {
	return SourceDocument{Article: workspace.Article{ID: 12}, Blocks: []SourceBlock{{ID: "B1", Text: "来源事实和原句"}}}
}

func validAnalysis() Analysis {
	return Analysis{AnalysisOutput: AnalysisOutput{
		Facts:  []Fact{{ID: "F1", Text: "来源事实", SourceBlockIDs: []string{"B1"}, Confidence: "high"}},
		Quotes: []Quote{{ID: "Q1", Text: "原句", SourceBlockID: "B1"}},
		Angles: []Angle{{ID: "A1"}, {ID: "A2"}, {ID: "A3"}},
	}}
}

func validGenerationParams() GenerationParams {
	return GenerationParams{
		AngleID:        "A1",
		Audience:       "技术团队",
		Tone:           "professional",
		TargetWords:    1_000,
		IdempotencyKey: "request-12345678",
	}
}

func generationAssets() map[uint64]workspace.Asset {
	return map[uint64]workspace.Asset{7: {ID: 7, ArticleID: 12, DownloadStatus: "completed"}}
}

func validGenerationOutput() GenerationOutput {
	assetID := uint64(7)
	return GenerationOutput{
		Title:  "新标题",
		Digest: "新摘要",
		Blocks: []GeneratedBlock{
			{Type: "paragraph", Text: "重新组织后的事实", FactIDs: []string{"F1"}},
			{Type: "quote", Text: "原句", QuoteID: "Q1"},
			{Type: "list", Items: []string{"要点一", "要点二"}},
			{Type: "image", AssetID: &assetID, Alt: "配图"},
		},
	}
}
