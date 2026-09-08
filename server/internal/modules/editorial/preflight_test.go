package editorial

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/chenbb0128/weavepress/server/internal/modules/workspace"
)

func TestValidateWeChatDraftAcceptsArchivedAssets(t *testing.T) {
	coverID := uint64(11)
	draft := Draft{
		Title:        "可发布稿件",
		Author:       "作者",
		Digest:       "摘要",
		ContentHTML:  `<p>正文</p><img data-weavepress-asset-id="12" alt="正文图">`,
		CoverAssetID: &coverID,
		SourceArticle: &workspace.Article{Assets: []workspace.Asset{
			{ID: 11, ObjectKey: "cover.jpg", MediaType: "image/jpeg", ByteSize: 1024, DownloadStatus: "completed"},
			{ID: 12, ObjectKey: "content.png", MediaType: "image/png", ByteSize: 2048, DownloadStatus: "completed"},
		}},
	}

	result := ValidateWeChatDraft(draft)
	if !result.Valid || len(result.Issues) != 0 {
		t.Fatalf("ValidateWeChatDraft() = %#v", result)
	}
}

func TestValidateWeChatDraftReportsWechatLimitsAndInvalidAssets(t *testing.T) {
	coverID := uint64(21)
	draft := Draft{
		Title:        strings.Repeat("题", WeChatMaxTitleRunes+1),
		Author:       strings.Repeat("作", WeChatMaxAuthorRunes+1),
		Digest:       strings.Repeat("摘", WeChatMaxDigestRunes+1),
		ContentHTML:  `<p></p><img src="https://example.com/not-archived.jpg">`,
		CoverAssetID: &coverID,
		SourceArticle: &workspace.Article{Assets: []workspace.Asset{
			{ID: 21, ObjectKey: "cover.webp", MediaType: "image/webp", ByteSize: WeChatMaxCoverImageSize + 1, DownloadStatus: "completed"},
		}},
	}

	result := ValidateWeChatDraft(draft)
	if result.Valid {
		t.Fatal("invalid draft passed preflight")
	}
	for _, code := range []string{
		"WECHAT_TITLE_TOO_LONG",
		"WECHAT_AUTHOR_TOO_LONG",
		"WECHAT_DIGEST_TOO_LONG",
		"WECHAT_ASSET_TYPE_UNSUPPORTED",
		"WECHAT_ASSET_TOO_LARGE",
		"WECHAT_IMAGE_INVALID",
	} {
		if !hasIssue(result, code) {
			t.Errorf("missing issue %s in %#v", code, result.Issues)
		}
	}
}

func TestValidateWeChatDraftRequiresSourceCoverAndMeaningfulContent(t *testing.T) {
	result := ValidateWeChatDraft(Draft{Title: "标题", ContentHTML: "<p> </p>"})
	for _, code := range []string{"WECHAT_CONTENT_REQUIRED", "WECHAT_SOURCE_MISSING"} {
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
