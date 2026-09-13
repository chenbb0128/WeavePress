package wechat

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chenbb0128/weavepress/server/internal/config"
	"github.com/chenbb0128/weavepress/server/internal/modules/editorial"
	"github.com/chenbb0128/weavepress/server/internal/modules/workspace"
)

func TestPublishUploadsImagesCoverAndCreatesDraft(t *testing.T) {
	var mu sync.Mutex
	tokenRequests, imageUploads, coverUploads := 0, 0, 0
	var draftBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		mu.Lock()
		defer mu.Unlock()
		switch request.URL.Path {
		case "/cgi-bin/token":
			tokenRequests++
			_, _ = io.WriteString(writer, `{"access_token":"token-1","expires_in":7200}`)
		case "/cgi-bin/media/uploadimg":
			imageUploads++
			if request.URL.Query().Get("access_token") != "token-1" {
				writer.WriteHeader(http.StatusUnauthorized)
				return
			}
			if err := request.ParseMultipartForm(maxMaterialBytes); err != nil {
				writer.WriteHeader(http.StatusBadRequest)
				return
			}
			_, _ = io.WriteString(writer, `{"url":"https://mmbiz.qpic.cn/remote-body.jpg"}`)
		case "/cgi-bin/material/add_material":
			coverUploads++
			if request.URL.Query().Get("type") != "thumb" {
				writer.WriteHeader(http.StatusBadRequest)
				return
			}
			_, _ = io.WriteString(writer, `{"media_id":"cover-media"}`)
		case "/cgi-bin/draft/add":
			draftBody, _ = io.ReadAll(request.Body)
			_, _ = io.WriteString(writer, `{"media_id":"draft-media"}`)
		default:
			writer.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	store := &memoryStore{objects: map[string][]byte{"body.jpg": []byte("body"), "cover.png": []byte("cover")}}
	client := New(config.WeChatConfig{Enabled: true, AppID: "app", AppSecret: "secret", APIBase: server.URL, RequestTimeout: time.Second}, store)
	bodyID, coverID := uint64(11), uint64(12)
	doc, err := editorial.ParseDocument([]byte(`{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"正文"}]},{"type":"image","attrs":{"draftAssetId":11,"width":100,"align":"center","alt":"配图","caption":""}}]}`))
	if err != nil {
		t.Fatal(err)
	}
	draft := editorial.Draft{
		ID: 1, EditorDocument: &doc,
		Title: "测试稿件", Author: "作者", Digest: "摘要", ContentHTML: `<p style="color:#222">正文</p><img data-weavepress-draft-asset-id="11" alt="配图"><img data-weavepress-draft-asset-id="11">`, CoverAssetID: &coverID,
		SourceArticle: &workspace.Article{CanonicalURL: "https://mp.weixin.qq.com/s/example", Assets: []workspace.Asset{{ID: 11, ObjectKey: "wrong.jpg"}}},
		Assets: []editorial.DraftAsset{
			{ID: bodyID, DraftID: 1, Origin: "upload", ObjectKey: "body.jpg", MediaType: "image/webp", ByteSize: 4, BodyEligible: true, CoverEligible: true},
			{ID: coverID, DraftID: 1, Origin: "upload", ObjectKey: "cover.png", MediaType: "image/gif", ByteSize: 5, BodyEligible: true, CoverEligible: true},
		},
	}
	result, err := client.Publish(context.Background(), draft)
	if err != nil {
		t.Fatal(err)
	}
	if result.RemoteMediaID != "draft-media" {
		t.Fatalf("media id = %q", result.RemoteMediaID)
	}
	mu.Lock()
	defer mu.Unlock()
	if tokenRequests != 1 || imageUploads != 1 || coverUploads != 1 {
		t.Fatalf("requests token=%d image=%d cover=%d", tokenRequests, imageUploads, coverUploads)
	}
	var payload struct {
		Articles []draftArticle `json:"articles"`
	}
	if err := json.Unmarshal(draftBody, &payload); err != nil || len(payload.Articles) != 1 {
		t.Fatalf("draft payload = %s, err=%v", draftBody, err)
	}
	article := payload.Articles[0]
	if article.ThumbMediaID != "cover-media" || !strings.Contains(article.Content, `style="color:#222"`) || !strings.Contains(article.Content, `src="https://mmbiz.qpic.cn/remote-body.jpg"`) || strings.Contains(article.Content, "data-weavepress-") {
		t.Fatalf("unexpected draft article: %#v", article)
	}
}

func TestPublishMapsCredentialErrorAsPermanent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(writer, `{"errcode":40125,"errmsg":"invalid appsecret"}`)
	}))
	defer server.Close()
	client := New(config.WeChatConfig{Enabled: true, AppID: "app", AppSecret: "bad", APIBase: server.URL, RequestTimeout: time.Second}, &memoryStore{})
	coverID := uint64(1)
	doc, parseErr := editorial.ParseDocument([]byte(`{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"正文"}]}]}`))
	if parseErr != nil {
		t.Fatal(parseErr)
	}
	_, err := client.Publish(context.Background(), editorial.Draft{
		ID: 1, EditorDocument: &doc,
		Title: "测试稿件", ContentHTML: "<p>正文</p>", CoverAssetID: &coverID,
		Assets: []editorial.DraftAsset{{ID: coverID, DraftID: 1, ObjectKey: "cover.jpg", MediaType: "image/jpeg", ByteSize: 5, BodyEligible: true, CoverEligible: true}},
	})
	var publishErr *editorial.PublishError
	if !errors.As(err, &publishErr) || publishErr.Code != "WECHAT_40125" || publishErr.Retryable {
		t.Fatalf("Publish() error = %#v", err)
	}
}

func TestPublishContentImagesRejectLegacyPlaceholder(t *testing.T) {
	client := New(config.WeChatConfig{}, &memoryStore{})
	_, err := client.uploadContentImages(context.Background(), "token", `<img data-weavepress-asset-id="11">`, map[uint64]editorial.DraftAsset{11: {ID: 11}})
	var publishErr *editorial.PublishError
	if !errors.As(err, &publishErr) || publishErr.Code != "DRAFT_IMAGE_INVALID" || publishErr.Retryable {
		t.Fatalf("error = %#v", err)
	}
}

func TestPublishAssetSizeLimits(t *testing.T) {
	for _, test := range []struct {
		name     string
		material bool
		declared uint64
		actual   int
		wantErr  bool
	}{
		{"body exact limit", false, maxContentImageBytes, maxContentImageBytes, false},
		{"body metadata over", false, maxContentImageBytes + 1, 1, true},
		{"body stream over", false, 1, maxContentImageBytes + 1, true},
		{"cover exact limit", true, maxMaterialBytes, maxMaterialBytes, false},
		{"cover metadata over", true, maxMaterialBytes + 1, 1, true},
		{"cover stream over", true, 1, maxMaterialBytes + 1, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := New(config.WeChatConfig{}, &memoryStore{objects: map[string][]byte{"image.webp": make([]byte, test.actual)}})
			_, err := client.readAsset(context.Background(), editorial.DraftAsset{ID: 1, ObjectKey: "image.webp", MediaType: "image/webp", ByteSize: test.declared}, test.material)
			if test.wantErr {
				var publishErr *editorial.PublishError
				if !errors.As(err, &publishErr) || publishErr.Code != "WECHAT_ASSET_TOO_LARGE" || publishErr.Retryable {
					t.Fatalf("error = %#v", err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestPublishPreservesWechatUnsupportedImageError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(writer, `{"errcode":40005,"errmsg":"invalid file type"}`)
	}))
	defer server.Close()
	client := New(config.WeChatConfig{APIBase: server.URL, RequestTimeout: time.Second}, &memoryStore{objects: map[string][]byte{"body.gif": []byte("gif")}})
	_, err := client.uploadAsset(context.Background(), "token", "/cgi-bin/media/uploadimg", "", editorial.DraftAsset{ID: 1, ObjectKey: "body.gif", MediaType: "image/gif", ByteSize: 3}, false)
	var publishErr *editorial.PublishError
	if !errors.As(err, &publishErr) || publishErr.Code != "WECHAT_40005" || publishErr.Retryable {
		t.Fatalf("error = %#v", err)
	}
}

type memoryStore struct {
	objects map[string][]byte
}

func (s *memoryStore) Put(_ context.Context, key string, data []byte, _ string) error {
	if s.objects == nil {
		s.objects = map[string][]byte{}
	}
	s.objects[key] = bytes.Clone(data)
	return nil
}

func (s *memoryStore) Open(_ context.Context, key string) (io.ReadCloser, error) {
	data, ok := s.objects[key]
	if !ok {
		return nil, errors.New("not found")
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

func (s *memoryStore) PrivateURL(string, time.Duration) (string, error) { return "", nil }
