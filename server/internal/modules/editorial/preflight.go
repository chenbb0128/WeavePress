package editorial

import (
	"fmt"
	"strings"
	"unicode/utf8"

	xhtml "golang.org/x/net/html"

	"github.com/chenbb0128/weavepress/server/internal/modules/workspace"
)

// ValidateWeChatDraft validates only deterministic WeChat draft rules. It does
// not call WeChat or read object storage, so it is safe to use before enqueueing.
func ValidateWeChatDraft(draft Draft) PreflightResult {
	result := PreflightResult{Valid: true, Issues: make([]PreflightIssue, 0)}
	add := func(code, message, field string) {
		result.Issues = append(result.Issues, PreflightIssue{Code: code, Message: message, Field: field})
	}

	title := strings.TrimSpace(draft.Title)
	switch {
	case title == "":
		add("WECHAT_TITLE_REQUIRED", "微信公众号稿件标题不能为空", "title")
	case utf8.RuneCountInString(title) > WeChatMaxTitleRunes:
		add("WECHAT_TITLE_TOO_LONG", fmt.Sprintf("微信公众号标题不能超过 %d 个字符", WeChatMaxTitleRunes), "title")
	}
	if utf8.RuneCountInString(strings.TrimSpace(draft.Author)) > WeChatMaxAuthorRunes {
		add("WECHAT_AUTHOR_TOO_LONG", fmt.Sprintf("微信公众号作者不能超过 %d 个字符", WeChatMaxAuthorRunes), "author")
	}
	if utf8.RuneCountInString(strings.TrimSpace(draft.Digest)) > WeChatMaxDigestRunes {
		add("WECHAT_DIGEST_TOO_LONG", fmt.Sprintf("微信公众号摘要不能超过 %d 个字符", WeChatMaxDigestRunes), "digest")
	}
	if !hasDraftContent(draft.ContentHTML) {
		add("WECHAT_CONTENT_REQUIRED", "微信公众号稿件正文不能为空", "contentHtml")
	}

	if draft.SourceArticle == nil {
		add("WECHAT_SOURCE_MISSING", "稿件来源文章不存在", "sourceArticleId")
		result.Valid = false
		return result
	}
	assets := make(map[uint64]workspace.Asset, len(draft.SourceArticle.Assets))
	for _, asset := range draft.SourceArticle.Assets {
		assets[asset.ID] = asset
	}

	if draft.CoverAssetID == nil {
		add("WECHAT_COVER_REQUIRED", "发布到微信公众号前必须选择封面", "coverAssetId")
	} else if cover, ok := assets[*draft.CoverAssetID]; !ok {
		add("WECHAT_COVER_INVALID", "封面素材不属于稿件来源文章", "coverAssetId")
	} else {
		validateWeChatAsset(&result, cover, true, "coverAssetId")
	}

	ids, err := ReferencedAssetIDs(draft.ContentHTML)
	if err != nil {
		add("WECHAT_IMAGE_INVALID", "正文图片必须使用已归档素材", "contentHtml")
	} else {
		for _, id := range ids {
			asset, ok := assets[id]
			if !ok {
				add("WECHAT_IMAGE_INVALID", fmt.Sprintf("正文素材 #%d 不属于稿件来源文章", id), "contentHtml")
				continue
			}
			validateWeChatAsset(&result, asset, false, "contentHtml")
		}
	}

	result.Valid = len(result.Issues) == 0
	return result
}

func validateWeChatAsset(result *PreflightResult, asset workspace.Asset, cover bool, field string) {
	label := fmt.Sprintf("正文素材 #%d", asset.ID)
	limit := uint64(WeChatMaxContentImageSize)
	if cover {
		label = fmt.Sprintf("封面素材 #%d", asset.ID)
		limit = uint64(WeChatMaxCoverImageSize)
	}
	if asset.DownloadStatus != "completed" || strings.TrimSpace(asset.ObjectKey) == "" {
		result.Issues = append(result.Issues, PreflightIssue{Code: "WECHAT_ASSET_UNAVAILABLE", Message: label + "尚未完成归档", Field: field})
		return
	}
	if !isWeChatImageType(asset.MediaType, cover) {
		message := label + "格式不受微信公众号支持"
		if !cover {
			message += "，正文图片仅支持 JPEG/PNG"
		}
		result.Issues = append(result.Issues, PreflightIssue{Code: "WECHAT_ASSET_TYPE_UNSUPPORTED", Message: message, Field: field})
	}
	if asset.ByteSize > limit {
		result.Issues = append(result.Issues, PreflightIssue{Code: "WECHAT_ASSET_TOO_LARGE", Message: fmt.Sprintf("%s超过 %d MiB 限制", label, limit>>20), Field: field})
	}
}

func isWeChatImageType(mediaType string, cover bool) bool {
	switch strings.ToLower(strings.TrimSpace(strings.Split(mediaType, ";")[0])) {
	case "image/jpeg", "image/png":
		return true
	case "image/gif":
		return cover
	default:
		return false
	}
}

func hasDraftContent(value string) bool {
	nodes, err := xhtml.ParseFragment(strings.NewReader(value), fragmentContext())
	if err != nil {
		return false
	}
	var walk func(*xhtml.Node) bool
	walk = func(node *xhtml.Node) bool {
		if node.Type == xhtml.TextNode && strings.TrimSpace(node.Data) != "" {
			return true
		}
		if node.Type == xhtml.ElementNode && node.Data == "img" {
			return true
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			if walk(child) {
				return true
			}
		}
		return false
	}
	for _, node := range nodes {
		if walk(node) {
			return true
		}
	}
	return false
}
