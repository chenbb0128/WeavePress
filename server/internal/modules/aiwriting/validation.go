package aiwriting

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/chenbb0128/weavepress/server/internal/modules/workspace"
)

const sourceOverlapRunes = 80

func BuildSourceDocument(article workspace.Article) SourceDocument {
	blocks := make([]SourceBlock, 0, len(article.Blocks))
	blockByID := make(map[string]SourceBlock, len(article.Blocks))
	plainParts := make([]string, 0, len(article.Blocks))
	for index, block := range article.Blocks {
		sourceBlock := SourceBlock{
			ID:      fmt.Sprintf("B%d", index+1),
			Type:    block.Type,
			Text:    block.Text,
			Level:   block.Level,
			AssetID: block.AssetID,
			Alt:     block.Alt,
		}
		blocks = append(blocks, sourceBlock)
		blockByID[sourceBlock.ID] = sourceBlock
		if block.Text != "" {
			plainParts = append(plainParts, block.Text)
		}
	}
	plainText := article.PlainText
	if plainText == "" {
		plainText = strings.Join(plainParts, "\n")
	}
	return SourceDocument{
		Article:   article,
		PlainText: plainText,
		Blocks:    blocks,
		BlockByID: blockByID,
	}
}

func ValidateGenerationParams(params GenerationParams) error {
	if strings.TrimSpace(params.AngleID) == "" {
		return invalidParameters("angleId is required")
	}
	audienceRunes := utf8.RuneCountInString(params.Audience)
	if strings.TrimSpace(params.Audience) == "" || audienceRunes > 100 {
		return invalidParameters("audience must contain 1 to 100 runes")
	}
	if _, ok := AllowedTones[params.Tone]; !ok {
		return invalidParameters("tone is not allowed")
	}
	if params.TargetWords < 300 || params.TargetWords > 5000 {
		return invalidParameters("targetWords must be between 300 and 5000")
	}
	if utf8.RuneCountInString(params.AdditionalInstructions) > 500 {
		return invalidParameters("additionalInstructions exceeds 500 runes")
	}
	if len(params.IdempotencyKey) < 8 || len(params.IdempotencyKey) > 128 || !isASCII(params.IdempotencyKey) {
		return invalidParameters("idempotencyKey must contain 8 to 128 ASCII characters")
	}
	return nil
}

func ValidateGenerationRequest(analysis Analysis, params GenerationParams) error {
	if err := ValidateGenerationParams(params); err != nil {
		return err
	}
	for _, angle := range analysis.Angles {
		if angle.ID == params.AngleID {
			return nil
		}
	}
	return invalidParameters("angleId does not belong to the analysis")
}

func ValidateAnalysis(source SourceDocument, output AnalysisOutput) error {
	if strings.TrimSpace(output.Summary) == "" {
		return invalidOutput("summary is required")
	}
	if len(output.Angles) != 3 {
		return invalidOutput("exactly three angles are required")
	}

	seen := make(map[string]struct{}, len(output.Facts)+len(output.Viewpoints)+len(output.Quotes)+len(output.Risks)+len(output.Angles))
	blocks := sourceBlockMap(source)
	for _, fact := range output.Facts {
		if err := validateEntityID(fact.ID, 'F', seen); err != nil {
			return err
		}
		if strings.TrimSpace(fact.Text) == "" {
			return invalidOutput("fact text is required")
		}
		switch fact.Confidence {
		case "high", "medium", "low":
		default:
			return invalidOutput("fact confidence is invalid")
		}
		if err := validateSourceBlockIDs(fact.SourceBlockIDs, blocks); err != nil {
			return err
		}
	}
	for _, viewpoint := range output.Viewpoints {
		if err := validateEntityID(viewpoint.ID, 'V', seen); err != nil {
			return err
		}
		if strings.TrimSpace(viewpoint.Text) == "" || strings.TrimSpace(viewpoint.Holder) == "" {
			return invalidOutput("viewpoint text and holder are required")
		}
		if err := validateSourceBlockIDs(viewpoint.SourceBlockIDs, blocks); err != nil {
			return err
		}
	}
	for _, quote := range output.Quotes {
		if err := validateEntityID(quote.ID, 'Q', seen); err != nil {
			return err
		}
		if strings.TrimSpace(quote.Text) == "" {
			return invalidOutput("quote text is required")
		}
		block, ok := blocks[quote.SourceBlockID]
		if !ok {
			return invalidSourceReference(quote.SourceBlockID)
		}
		if !strings.Contains(block.Text, quote.Text) {
			return fmt.Errorf("%w: quote %s", ErrQuoteMismatch, quote.ID)
		}
	}
	for _, risk := range output.Risks {
		if err := validateEntityID(risk.ID, 'R', seen); err != nil {
			return err
		}
		if strings.TrimSpace(risk.Text) == "" {
			return invalidOutput("risk text is required")
		}
		if err := validateSourceBlockIDs(risk.SourceBlockIDs, blocks); err != nil {
			return err
		}
	}
	for _, angle := range output.Angles {
		if err := validateEntityID(angle.ID, 'A', seen); err != nil {
			return err
		}
		if strings.TrimSpace(angle.Title) == "" || strings.TrimSpace(angle.Thesis) == "" || len(angle.Outline) == 0 {
			return invalidOutput("angle title, thesis, and outline are required")
		}
		for _, item := range angle.Outline {
			if strings.TrimSpace(item) == "" {
				return invalidOutput("angle outline item is required")
			}
		}
	}
	return nil
}

func ValidateGeneration(source SourceDocument, analysis Analysis, assets map[uint64]workspace.Asset, output GenerationOutput) error {
	if invalidBoundedText(output.Title, 255) {
		return invalidOutput("title must contain 1 to 255 runes")
	}
	if invalidBoundedText(output.Digest, 255) {
		return invalidOutput("digest must contain 1 to 255 runes")
	}
	if len(output.Blocks) == 0 {
		return invalidOutput("blocks are required")
	}

	facts := make(map[string]struct{}, len(analysis.Facts))
	for _, fact := range analysis.Facts {
		facts[fact.ID] = struct{}{}
	}
	quotes := make(map[string]Quote, len(analysis.Quotes))
	for _, quote := range analysis.Quotes {
		quotes[quote.ID] = quote
	}

	for _, block := range output.Blocks {
		for _, factID := range block.FactIDs {
			if _, ok := facts[factID]; !ok {
				return invalidSourceReference(factID)
			}
		}
		switch block.Type {
		case "heading":
			if block.Level < 2 || block.Level > 4 || strings.TrimSpace(block.Text) == "" {
				return invalidOutput("heading requires level 2 to 4 and non-empty text")
			}
		case "paragraph":
			if strings.TrimSpace(block.Text) == "" {
				return invalidOutput("paragraph text is required")
			}
		case "quote":
			quote, ok := quotes[block.QuoteID]
			if !ok {
				return invalidSourceReference(block.QuoteID)
			}
			if block.Text != quote.Text {
				return fmt.Errorf("%w: quote %s", ErrQuoteMismatch, block.QuoteID)
			}
		case "list":
			if len(block.Items) == 0 {
				return invalidOutput("list items are required")
			}
			for _, item := range block.Items {
				if strings.TrimSpace(item) == "" {
					return invalidOutput("list item is required")
				}
			}
		case "image":
			if block.AssetID == nil || *block.AssetID == 0 {
				return fmt.Errorf("%w: image assetId is required", ErrAssetInvalid)
			}
			asset, ok := assets[*block.AssetID]
			if !ok || asset.ID != *block.AssetID || asset.ArticleID != source.Article.ID || asset.DownloadStatus != "completed" {
				return fmt.Errorf("%w: asset %d", ErrAssetInvalid, *block.AssetID)
			}
		default:
			return invalidOutput("generated block type is invalid")
		}
	}

	if generationOverlapsSource(source, output.Blocks) {
		return ErrExcessiveSourceOverlap
	}
	return nil
}

func DecodeAnalysisOutput(raw string) (AnalysisOutput, error) {
	output, err := decodeStrictJSON[AnalysisOutput](raw)
	if err != nil {
		return AnalysisOutput{}, err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &fields); err != nil {
		return AnalysisOutput{}, fmt.Errorf("%w: %v", ErrOutputInvalid, err)
	}
	for _, field := range []string{"facts", "viewpoints", "quotes", "risks", "angles"} {
		value, ok := fields[field]
		if !ok || strings.TrimSpace(string(value)) == "null" {
			return AnalysisOutput{}, invalidOutput(field + " must be a non-null array")
		}
	}
	return output, nil
}

func DecodeGenerationOutput(raw string) (GenerationOutput, error) {
	return decodeStrictJSON[GenerationOutput](raw)
}

func decodeStrictJSON[T any](raw string) (T, error) {
	var result T
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || trimmed[0] != '{' {
		return result, invalidOutput("response must be one JSON object")
	}
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result); err != nil {
		return result, fmt.Errorf("%w: %v", ErrOutputInvalid, err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return result, invalidOutput("response contains multiple JSON values")
		}
		return result, fmt.Errorf("%w: %v", ErrOutputInvalid, err)
	}
	return result, nil
}

func sourceBlockMap(source SourceDocument) map[string]SourceBlock {
	if source.BlockByID != nil {
		return source.BlockByID
	}
	blocks := make(map[string]SourceBlock, len(source.Blocks))
	for _, block := range source.Blocks {
		blocks[block.ID] = block
	}
	return blocks
}

func validateEntityID(id string, prefix byte, seen map[string]struct{}) error {
	if len(id) < 2 || id[0] != prefix || id[1] < '1' || id[1] > '9' {
		return invalidOutput("entity ID is malformed")
	}
	for index := 2; index < len(id); index++ {
		if id[index] < '0' || id[index] > '9' {
			return invalidOutput("entity ID is malformed")
		}
	}
	if _, ok := seen[id]; ok {
		return invalidOutput("entity ID is duplicated")
	}
	seen[id] = struct{}{}
	return nil
}

func validateSourceBlockIDs(ids []string, blocks map[string]SourceBlock) error {
	if len(ids) == 0 {
		return invalidOutput("sourceBlockIds are required")
	}
	for _, id := range ids {
		if _, ok := blocks[id]; !ok {
			return invalidSourceReference(id)
		}
	}
	return nil
}

func generationOverlapsSource(source SourceDocument, blocks []GeneratedBlock) bool {
	windows := make(map[string]struct{})
	if source.PlainText != "" {
		addRuneWindows(windows, source.PlainText)
	}
	for _, block := range source.Blocks {
		addRuneWindows(windows, block.Text)
	}
	if len(windows) == 0 {
		return false
	}
	for _, block := range blocks {
		switch block.Type {
		case "heading", "paragraph":
			if hasRuneWindow(windows, block.Text) {
				return true
			}
		case "list":
			for _, item := range block.Items {
				if hasRuneWindow(windows, item) {
					return true
				}
			}
		}
	}
	return false
}

func addRuneWindows(windows map[string]struct{}, value string) {
	runes := []rune(value)
	for index := 0; index+sourceOverlapRunes <= len(runes); index++ {
		windows[string(runes[index:index+sourceOverlapRunes])] = struct{}{}
	}
}

func hasRuneWindow(windows map[string]struct{}, value string) bool {
	runes := []rune(value)
	for index := 0; index+sourceOverlapRunes <= len(runes); index++ {
		if _, ok := windows[string(runes[index:index+sourceOverlapRunes])]; ok {
			return true
		}
	}
	return false
}

func invalidBoundedText(value string, limit int) bool {
	return strings.TrimSpace(value) == "" || utf8.RuneCountInString(value) > limit
}

func isASCII(value string) bool {
	for index := 0; index < len(value); index++ {
		if value[index] > 127 {
			return false
		}
	}
	return true
}

func invalidParameters(detail string) error {
	return fmt.Errorf("%w: %s", ErrInvalidParameters, detail)
}

func invalidOutput(detail string) error {
	return fmt.Errorf("%w: %s", ErrOutputInvalid, detail)
}

func invalidSourceReference(id string) error {
	return fmt.Errorf("%w: %s", ErrSourceReferenceInvalid, id)
}
