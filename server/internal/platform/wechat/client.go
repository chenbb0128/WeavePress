package wechat

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	xhtml "golang.org/x/net/html"
	"golang.org/x/net/html/atom"

	"github.com/chenbb0128/weavepress/server/internal/config"
	"github.com/chenbb0128/weavepress/server/internal/modules/editorial"
	"github.com/chenbb0128/weavepress/server/internal/modules/workspace"
	"github.com/chenbb0128/weavepress/server/internal/platform/objectstore"
)

const (
	maxContentImageBytes = editorial.WeChatMaxContentImageSize
	maxMaterialBytes     = editorial.WeChatMaxCoverImageSize
)

type Client struct {
	cfg     config.WeChatConfig
	objects objectstore.Store
	http    *http.Client

	mu          sync.Mutex
	accessToken string
	tokenExpiry time.Time
}

func New(cfg config.WeChatConfig, objects objectstore.Store) *Client {
	return &Client{
		cfg:     cfg,
		objects: objects,
		http:    &http.Client{Timeout: cfg.RequestTimeout},
	}
}

func (c *Client) Publish(ctx context.Context, draft editorial.Draft) (editorial.PublishResult, error) {
	if !c.cfg.Enabled || strings.TrimSpace(c.cfg.AppID) == "" || strings.TrimSpace(c.cfg.AppSecret) == "" {
		return editorial.PublishResult{}, editorial.ErrPublisherDisabled
	}
	preflight := editorial.ValidateWeChatDraft(draft)
	if !preflight.Valid {
		return editorial.PublishResult{}, &editorial.PreflightError{Result: preflight}
	}
	if draft.CoverAssetID == nil {
		return editorial.PublishResult{}, editorial.ErrCoverRequired
	}
	if draft.SourceArticle == nil {
		return editorial.PublishResult{}, publishError("DRAFT_SOURCE_MISSING", "稿件来源文章不存在", false, nil)
	}

	assets := make(map[uint64]workspace.Asset, len(draft.SourceArticle.Assets))
	for _, asset := range draft.SourceArticle.Assets {
		assets[asset.ID] = asset
	}
	token, err := c.token(ctx)
	if err != nil {
		return editorial.PublishResult{}, err
	}
	content, err := c.uploadContentImages(ctx, token, draft.ContentHTML, assets)
	if err != nil {
		return editorial.PublishResult{}, err
	}
	cover, ok := assets[*draft.CoverAssetID]
	if !ok {
		return editorial.PublishResult{}, publishError("WECHAT_COVER_INVALID", "封面素材不属于稿件来源文章", false, nil)
	}
	thumbMediaID, err := c.uploadAsset(ctx, token, "/cgi-bin/material/add_material", "thumb", cover, true)
	if err != nil {
		return editorial.PublishResult{}, err
	}

	payload := struct {
		Articles []draftArticle `json:"articles"`
	}{Articles: []draftArticle{{
		Title:              draft.Title,
		Author:             draft.Author,
		Digest:             draft.Digest,
		Content:            content,
		ContentSourceURL:   draft.SourceArticle.CanonicalURL,
		ThumbMediaID:       thumbMediaID,
		ShowCoverPic:       1,
		NeedOpenComment:    0,
		OnlyFansCanComment: 0,
	}}}
	var response struct {
		wechatResponse
		MediaID string `json:"media_id"`
	}
	if err := c.postJSON(ctx, token, "/cgi-bin/draft/add", payload, &response); err != nil {
		return editorial.PublishResult{}, err
	}
	if strings.TrimSpace(response.MediaID) == "" {
		return editorial.PublishResult{}, publishError("WECHAT_INVALID_RESPONSE", "微信公众号未返回草稿 media_id", true, nil)
	}
	return editorial.PublishResult{RemoteMediaID: response.MediaID}, nil
}

type draftArticle struct {
	Title              string `json:"title"`
	Author             string `json:"author,omitempty"`
	Digest             string `json:"digest,omitempty"`
	Content            string `json:"content"`
	ContentSourceURL   string `json:"content_source_url,omitempty"`
	ThumbMediaID       string `json:"thumb_media_id"`
	ShowCoverPic       int    `json:"show_cover_pic"`
	NeedOpenComment    int    `json:"need_open_comment"`
	OnlyFansCanComment int    `json:"only_fans_can_comment"`
}

type wechatResponse struct {
	ErrCode int    `json:"errcode"`
	ErrMsg  string `json:"errmsg"`
}

func (c *Client) token(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.accessToken != "" && time.Now().Before(c.tokenExpiry) {
		return c.accessToken, nil
	}
	endpoint, err := c.endpoint("/cgi-bin/token", url.Values{
		"grant_type": []string{"client_credential"},
		"appid":      []string{c.cfg.AppID},
		"secret":     []string{c.cfg.AppSecret},
	})
	if err != nil {
		return "", publishError("WECHAT_CONFIG_INVALID", "微信公众号接口地址不正确", false, err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", publishError("WECHAT_REQUEST_FAILED", "无法创建微信公众号请求", false, err)
	}
	var response struct {
		wechatResponse
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := c.do(request, &response); err != nil {
		return "", err
	}
	if response.AccessToken == "" {
		return "", publishError("WECHAT_INVALID_RESPONSE", "微信公众号未返回 access_token", true, nil)
	}
	ttl := time.Duration(response.ExpiresIn) * time.Second
	if ttl <= 0 {
		ttl = 2 * time.Hour
	}
	if ttl > 5*time.Minute {
		ttl -= 5 * time.Minute
	}
	c.accessToken = response.AccessToken
	c.tokenExpiry = time.Now().Add(ttl)
	return c.accessToken, nil
}

func (c *Client) uploadContentImages(ctx context.Context, token, content string, assets map[uint64]workspace.Asset) (string, error) {
	nodes, err := xhtml.ParseFragment(strings.NewReader(content), &xhtml.Node{Type: xhtml.ElementNode, Data: "div", DataAtom: atom.Div})
	if err != nil {
		return "", publishError("DRAFT_HTML_INVALID", "稿件正文 HTML 无法解析", false, err)
	}
	uploaded := make(map[uint64]string)
	var walk func(*xhtml.Node) error
	walk = func(node *xhtml.Node) error {
		if node.Type == xhtml.ElementNode && node.Data == "img" {
			rawID, ok := attribute(node, "data-weavepress-asset-id")
			if !ok {
				return publishError("DRAFT_IMAGE_INVALID", "稿件正文包含未归档的图片", false, nil)
			}
			id, parseErr := strconv.ParseUint(rawID, 10, 64)
			if parseErr != nil || id == 0 {
				return publishError("DRAFT_IMAGE_INVALID", "稿件正文图片标识不正确", false, parseErr)
			}
			remoteURL := uploaded[id]
			if remoteURL == "" {
				asset, exists := assets[id]
				if !exists {
					return publishError("DRAFT_IMAGE_INVALID", "稿件正文图片不属于来源文章", false, nil)
				}
				remoteURL, parseErr = c.uploadAsset(ctx, token, "/cgi-bin/media/uploadimg", "", asset, false)
				if parseErr != nil {
					return parseErr
				}
				uploaded[id] = remoteURL
			}
			setAttribute(node, "src", remoteURL)
			removeAttribute(node, "data-weavepress-asset-id")
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			if err := walk(child); err != nil {
				return err
			}
		}
		return nil
	}
	for _, node := range nodes {
		if err := walk(node); err != nil {
			return "", err
		}
	}
	var output bytes.Buffer
	for _, node := range nodes {
		if err := xhtml.Render(&output, node); err != nil {
			return "", publishError("DRAFT_HTML_INVALID", "稿件正文 HTML 无法生成", false, err)
		}
	}
	return output.String(), nil
}

func (c *Client) uploadAsset(ctx context.Context, token, endpointPath, mediaType string, asset workspace.Asset, material bool) (string, error) {
	data, err := c.readAsset(ctx, asset, material)
	if err != nil {
		return "", err
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	extension := path.Ext(asset.ObjectKey)
	if extension == "" {
		extension = extensionFor(asset.MediaType)
	}
	part, err := writer.CreateFormFile("media", fmt.Sprintf("asset-%d%s", asset.ID, extension))
	if err != nil {
		return "", publishError("WECHAT_UPLOAD_FAILED", "无法创建微信素材上传请求", false, err)
	}
	if _, err = part.Write(data); err != nil {
		return "", publishError("WECHAT_UPLOAD_FAILED", "无法读取待上传素材", true, err)
	}
	if err = writer.Close(); err != nil {
		return "", publishError("WECHAT_UPLOAD_FAILED", "无法完成微信素材上传请求", false, err)
	}
	query := url.Values{"access_token": []string{token}}
	if mediaType != "" {
		query.Set("type", mediaType)
	}
	endpoint, err := c.endpoint(endpointPath, query)
	if err != nil {
		return "", publishError("WECHAT_CONFIG_INVALID", "微信公众号接口地址不正确", false, err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, &body)
	if err != nil {
		return "", publishError("WECHAT_REQUEST_FAILED", "无法创建微信素材上传请求", false, err)
	}
	request.Header.Set("Content-Type", writer.FormDataContentType())
	var response struct {
		wechatResponse
		URL     string `json:"url"`
		MediaID string `json:"media_id"`
	}
	if err := c.do(request, &response); err != nil {
		return "", err
	}
	if material {
		if response.MediaID == "" {
			return "", publishError("WECHAT_INVALID_RESPONSE", "微信公众号未返回封面 media_id", true, nil)
		}
		return response.MediaID, nil
	}
	if response.URL == "" {
		return "", publishError("WECHAT_INVALID_RESPONSE", "微信公众号未返回正文图片地址", true, nil)
	}
	return response.URL, nil
}

func (c *Client) readAsset(ctx context.Context, asset workspace.Asset, material bool) ([]byte, error) {
	if asset.DownloadStatus != "completed" || asset.ObjectKey == "" {
		return nil, publishError("DRAFT_ASSET_UNAVAILABLE", "稿件素材尚未完成归档", false, nil)
	}
	limit := int64(maxContentImageBytes)
	if material {
		limit = maxMaterialBytes
	}
	if asset.ByteSize > uint64(limit) {
		return nil, publishError("WECHAT_ASSET_TOO_LARGE", assetLimitMessage(material), false, nil)
	}
	if !supportedImage(asset.MediaType, material) {
		return nil, publishError("WECHAT_ASSET_TYPE_UNSUPPORTED", "微信素材格式不支持", false, nil)
	}
	reader, err := c.objects.Open(ctx, asset.ObjectKey)
	if err != nil {
		return nil, publishError("ASSET_STORAGE_UNAVAILABLE", "无法读取已归档素材", true, err)
	}
	defer reader.Close()
	data, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		return nil, publishError("ASSET_STORAGE_UNAVAILABLE", "读取已归档素材失败", true, err)
	}
	if int64(len(data)) > limit {
		return nil, publishError("WECHAT_ASSET_TOO_LARGE", assetLimitMessage(material), false, nil)
	}
	return data, nil
}

func (c *Client) postJSON(ctx context.Context, token, endpointPath string, payload, target any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return publishError("WECHAT_REQUEST_FAILED", "无法生成微信公众号请求", false, err)
	}
	endpoint, err := c.endpoint(endpointPath, url.Values{"access_token": []string{token}})
	if err != nil {
		return publishError("WECHAT_CONFIG_INVALID", "微信公众号接口地址不正确", false, err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return publishError("WECHAT_REQUEST_FAILED", "无法创建微信公众号请求", false, err)
	}
	request.Header.Set("Content-Type", "application/json; charset=utf-8")
	return c.do(request, target)
}

func (c *Client) do(request *http.Request, target any) error {
	response, err := c.http.Do(request)
	if err != nil {
		return publishError("WECHAT_NETWORK_ERROR", "微信公众号接口暂时不可用", true, err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		message, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		retryable := response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= 500
		return publishError("WECHAT_HTTP_ERROR", fmt.Sprintf("微信公众号接口返回 HTTP %d", response.StatusCode), retryable, fmt.Errorf("%s", strings.TrimSpace(string(message))))
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, 1<<20))
	if err := decoder.Decode(target); err != nil {
		return publishError("WECHAT_INVALID_RESPONSE", "微信公众号接口返回了无效响应", true, err)
	}
	encoded, err := json.Marshal(target)
	if err != nil {
		return publishError("WECHAT_INVALID_RESPONSE", "微信公众号接口返回了无效响应", true, err)
	}
	var base wechatResponse
	if err := json.Unmarshal(encoded, &base); err != nil {
		return publishError("WECHAT_INVALID_RESPONSE", "微信公众号接口返回了无效响应", true, err)
	}
	if base.ErrCode != 0 {
		if (base.ErrCode == 40014 || base.ErrCode == 42001) && request.URL.Query().Get("access_token") != "" {
			c.invalidateToken()
		}
		return publishError(fmt.Sprintf("WECHAT_%d", base.ErrCode), wechatMessage(base), retryableCode(base.ErrCode), nil)
	}
	return nil
}

func (c *Client) endpoint(endpointPath string, query url.Values) (string, error) {
	base, err := url.Parse(strings.TrimRight(c.cfg.APIBase, "/"))
	if err != nil {
		return "", err
	}
	base.Path = strings.TrimRight(base.Path, "/") + endpointPath
	base.RawQuery = query.Encode()
	return base.String(), nil
}

func (c *Client) invalidateToken() {
	c.mu.Lock()
	c.accessToken = ""
	c.tokenExpiry = time.Time{}
	c.mu.Unlock()
}

func attribute(node *xhtml.Node, key string) (string, bool) {
	for _, attr := range node.Attr {
		if attr.Key == key {
			return attr.Val, true
		}
	}
	return "", false
}

func setAttribute(node *xhtml.Node, key, value string) {
	for index := range node.Attr {
		if node.Attr[index].Key == key {
			node.Attr[index].Val = value
			return
		}
	}
	node.Attr = append(node.Attr, xhtml.Attribute{Key: key, Val: value})
}

func removeAttribute(node *xhtml.Node, key string) {
	for index := range node.Attr {
		if node.Attr[index].Key == key {
			node.Attr = append(node.Attr[:index], node.Attr[index+1:]...)
			return
		}
	}
}

func supportedImage(mediaType string, material bool) bool {
	switch strings.ToLower(strings.TrimSpace(strings.Split(mediaType, ";")[0])) {
	case "image/jpeg", "image/png":
		return true
	case "image/gif":
		return material
	default:
		return false
	}
}

func assetLimitMessage(material bool) string {
	if material {
		return "微信封面素材超过 10 MiB 限制"
	}
	return "微信正文图片超过 1 MiB 限制"
}

func extensionFor(mediaType string) string {
	switch strings.ToLower(strings.TrimSpace(strings.Split(mediaType, ";")[0])) {
	case "image/gif":
		return ".gif"
	case "image/png":
		return ".png"
	default:
		return ".jpg"
	}
}

func retryableCode(code int) bool {
	switch code {
	case -1, 40014, 42001, 45009:
		return true
	default:
		return false
	}
}

func wechatMessage(response wechatResponse) string {
	if response.ErrMsg == "" {
		return fmt.Sprintf("微信公众号接口返回错误 %d", response.ErrCode)
	}
	return fmt.Sprintf("微信公众号接口返回错误 %d：%s", response.ErrCode, response.ErrMsg)
}

func publishError(code, message string, retryable bool, cause error) error {
	return &editorial.PublishError{Code: code, Message: message, Retryable: retryable, Cause: cause}
}
