package editorial

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestPreflightAcceptsDraftAssets(t *testing.T) {
	coverID := uint64(11)
	draft := Draft{
		ID:             1,
		EditorDocument: &Document{Type: "doc", Content: []Node{imageNode(12)}},
		Title:          "可发布稿件",
		Author:         "作者",
		Digest:         "摘要",
		ContentHTML:    `<p>正文</p><img data-weavepress-draft-asset-id="12" alt="正文图">`,
		CoverAssetID:   &coverID,
		Assets: []DraftAsset{
			{ID: 11, DraftID: 1, ObjectKey: "cover.jpg", MediaType: "image/jpeg", ByteSize: 1024, BodyEligible: true, CoverEligible: true},
			{ID: 12, DraftID: 1, ObjectKey: "content.webp", MediaType: "image/webp", ByteSize: 2048, BodyEligible: true, CoverEligible: true},
		},
	}

	result := ValidateWeChatDraft(draft)
	if !result.Valid || len(result.Issues) != 0 {
		t.Fatalf("ValidateWeChatDraft() = %#v", result)
	}
}

func TestPreflightReportsWechatLimitsAndInvalidAssets(t *testing.T) {
	coverID := uint64(21)
	draft := Draft{
		ID:             1,
		EditorDocument: &Document{Type: "doc", Content: []Node{imageNode(22), imageNode(23)}},
		Title:          strings.Repeat("题", WeChatMaxTitleRunes+1),
		Author:         strings.Repeat("作", WeChatMaxAuthorRunes+1),
		Digest:         strings.Repeat("摘", WeChatMaxDigestRunes+1),
		ContentHTML:    `<p></p><img src="https://example.com/not-archived.jpg">`,
		CoverAssetID:   &coverID,
		Assets: []DraftAsset{
			{ID: 21, DraftID: 1, ObjectKey: "cover.webp", MediaType: "image/webp", ByteSize: WeChatMaxCoverImageSize + 1},
			{ID: 22, DraftID: 1, ObjectKey: "body.gif", MediaType: "image/gif", ByteSize: WeChatMaxContentImageSize + 1, CoverEligible: true},
		},
	}

	result := ValidateWeChatDraft(draft)
	if result.Valid {
		t.Fatal("invalid draft passed preflight")
	}
	for _, code := range []string{
		"WECHAT_TITLE_TOO_LONG",
		"WECHAT_AUTHOR_TOO_LONG",
		"WECHAT_DIGEST_TOO_LONG",
		"COVER_ASSET_INVALID",
		"CONTENT_IMAGE_TOO_LARGE",
		"DRAFT_ASSET_MISSING",
	} {
		if !hasIssue(result, code) {
			t.Errorf("missing issue %s in %#v", code, result.Issues)
		}
	}
}

func TestPreflightRequiresDocumentCoverAndMeaningfulContent(t *testing.T) {
	result := ValidateWeChatDraft(Draft{Title: "标题", ContentHTML: "<p> </p>"})
	for _, code := range []string{"WECHAT_CONTENT_REQUIRED", "EDITOR_DOCUMENT_REQUIRED", "WECHAT_COVER_REQUIRED"} {
		if !hasIssue(result, code) {
			t.Errorf("missing issue %s in %#v", code, result.Issues)
		}
	}
}

func TestPreflightErrorUnwraps(t *testing.T) {
	err := &PreflightError{Result: PreflightResult{Issues: []PreflightIssue{{Message: "标题过长"}}}}
	if got := err.Error(); got != fmt.Sprintf("%s: 标题过长", ErrDraftPreflightFailed) {
		t.Fatalf("Error() = %q", got)
	}
	if !errors.Is(err, ErrDraftPreflightFailed) {
		t.Fatal("PreflightError must unwrap ErrDraftPreflightFailed")
	}
}

func hasIssue(result PreflightResult, code string) bool {
	for _, issue := range result.Issues {
		if issue.Code == code {
			return true
		}
	}
	return false
}
