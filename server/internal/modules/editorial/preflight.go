package editorial

import (
	"fmt"
	"strings"
	"unicode/utf8"

	xhtml "golang.org/x/net/html"
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

	if draft.MigrationNeeded || draft.EditorDocument == nil || ValidateDocument(*draft.EditorDocument) != nil || !documentHasBody(*draft.EditorDocument) {
		add("EDITOR_DOCUMENT_REQUIRED", "请保存有效的结构化正文后再提交", "editorDocument")
	}
	assets := make(map[uint64]DraftAsset, len(draft.Assets))
	for _, asset := range draft.Assets {
		assets[asset.ID] = asset
	}

	if draft.CoverAssetID == nil {
		add("WECHAT_COVER_REQUIRED", "发布到微信公众号前必须选择封面", "coverAssetId")
	} else if cover, ok := assets[*draft.CoverAssetID]; !ok || cover.DraftID != draft.ID || !cover.CoverEligible || cover.ByteSize > WeChatMaxCoverImageSize || strings.TrimSpace(cover.ObjectKey) == "" || !isWeChatImageType(cover.MediaType, true) {
		add("COVER_ASSET_INVALID", "封面素材不可用或不属于当前稿件", "coverAssetId")
	}

	if draft.EditorDocument != nil {
		for _, id := range ReferencedDraftAssetIDs(*draft.EditorDocument) {
			asset, ok := assets[id]
			if !ok || asset.DraftID != draft.ID || strings.TrimSpace(asset.ObjectKey) == "" {
				add("DRAFT_ASSET_MISSING", fmt.Sprintf("正文素材 #%d 不可用或不属于当前稿件", id), "editorDocument")
				continue
			}
			if asset.ByteSize > WeChatMaxContentImageSize {
				add("CONTENT_IMAGE_TOO_LARGE", fmt.Sprintf("正文素材 #%d 超过 1 MiB 限制", id), "editorDocument")
			} else if !asset.BodyEligible || !isWeChatImageType(asset.MediaType, false) {
				add("DRAFT_ASSET_MISSING", fmt.Sprintf("正文素材 #%d 不可用于正文", id), "editorDocument")
			}
		}
	}

	result.Valid = len(result.Issues) == 0
	return result
}

func isWeChatImageType(mediaType string, cover bool) bool {
	switch strings.ToLower(strings.TrimSpace(strings.Split(mediaType, ";")[0])) {
	case "image/jpeg", "image/png", "image/gif", "image/webp":
		return true
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
