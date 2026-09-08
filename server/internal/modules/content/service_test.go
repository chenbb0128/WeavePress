package content

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"errors"
	"image"
	"image/color"
	"image/png"
	"io"
	neturl "net/url"
	"reflect"
	"strconv"
	"testing"
	"time"

	"github.com/chenbb0128/weavepress/server/internal/collectors"
	"github.com/chenbb0128/weavepress/server/internal/config"
	"github.com/chenbb0128/weavepress/server/internal/modules/workspace"
)

type workflowStore struct {
	workspace.Store
	article   workspace.Article
	job       workspace.Job
	stages    []string
	completed bool
	rawKey    string
	assets    []workspace.StoredAsset
	warnings  []string
}

func (s *workflowStore) GetJob(context.Context, uint64, bool) (workspace.Job, error) {
	return s.job, nil
}
func (s *workflowStore) GetArticle(context.Context, uint64) (workspace.Article, error) {
	return s.article, nil
}
func (s *workflowStore) SetJobStage(_ context.Context, _ uint64, status, _ string) error {
	s.stages = append(s.stages, status)
	s.job.Status = status
	return nil
}
func (s *workflowStore) CompleteArticle(_ context.Context, _, _ uint64, _ workspace.CollectedArticle, rawKey string, _ [32]byte, assets []workspace.StoredAsset, warnings []string) error {
	s.completed, s.rawKey, s.assets, s.warnings = true, rawKey, assets, warnings
	return nil
}

type workflowFetcher struct {
	page  collectors.FetchResult
	image collectors.FetchResult
}

func (f workflowFetcher) Validate(context.Context, *neturl.URL) error { return nil }
func (f workflowFetcher) FetchHTML(context.Context, string) (collectors.FetchResult, error) {
	return f.page, nil
}
func (f workflowFetcher) FetchImage(context.Context, string, string) (collectors.FetchResult, error) {
	return f.image, nil
}

type fakeObjectStore struct{ objects map[string][]byte }

func (s *fakeObjectStore) Put(_ context.Context, key string, data []byte, _ string) error {
	if s.objects == nil {
		s.objects = make(map[string][]byte)
	}
	s.objects[key] = bytes.Clone(data)
	return nil
}
func (s *fakeObjectStore) Open(_ context.Context, key string) (io.ReadCloser, error) {
	data, ok := s.objects[key]
	if !ok {
		return nil, errors.New("object not found")
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}
func (*fakeObjectStore) PrivateURL(string, time.Duration) (string, error) { return "", nil }

func TestMediaSignatureExpiresAndRejectsTampering(t *testing.T) {
	service := New(nil, nil, nil, config.Config{Auth: config.AuthConfig{MediaSigningKey: "test-media-secret-that-is-at-least-32-chars", MediaURLTTL: 10 * time.Minute}})
	url := service.SignMedia("assets", 7)
	if url == "" {
		t.Fatal("SignMedia returned empty URL")
	}
	parsed, err := neturl.Parse(url)
	if err != nil {
		t.Fatal(err)
	}
	expires, _ := strconv.ParseInt(parsed.Query().Get("expires"), 10, 64)
	if !service.VerifyMedia("assets", 7, expires, parsed.Query().Get("signature")) {
		t.Fatal("valid signature was rejected")
	}
	valid := service.SignMedia("assets", 7)
	if valid == service.SignMedia("assets", 8) {
		t.Fatal("different asset IDs received the same signed URL")
	}
	if service.VerifyMedia("assets", 7, time.Now().Add(-time.Second).Unix(), "deadbeef") {
		t.Fatal("expired signature was accepted")
	}
}

func TestImageDimensions(t *testing.T) {
	var encoded bytes.Buffer
	source := image.NewRGBA(image.Rect(0, 0, 23, 17))
	source.Set(0, 0, color.White)
	if err := png.Encode(&encoded, source); err != nil {
		t.Fatal(err)
	}
	if width, height := imageDimensions(encoded.Bytes()); width != 23 || height != 17 {
		t.Fatalf("PNG dimensions = %dx%d, want 23x17", width, height)
	}

	webp := make([]byte, 30)
	copy(webp[:4], "RIFF")
	copy(webp[8:12], "WEBP")
	copy(webp[12:16], "VP8X")
	webp[24], webp[25], webp[26] = 22, 0, 0
	webp[27], webp[28], webp[29] = 16, 0, 0
	if width, height := imageDimensions(webp); width != 23 || height != 17 {
		t.Fatalf("WebP dimensions = %dx%d, want 23x17", width, height)
	}
}

func TestCollectionWorkflowUsesFakeStorage(t *testing.T) {
	pageURL, _ := neturl.Parse("https://mp.weixin.qq.com/s?__biz=fake&mid=1&idx=1&sn=test")
	html := []byte(`<!doctype html><html><head><meta property="og:title" content="采集流程测试"></head><body><div id="js_content"><p>这是一段用于验证完整采集流程的固定正文，内容足够长，可以通过正文校验并生成结构化段落，同时不需要访问任何外部网络服务。</p><img data-src="https://images.example.com/test.png" alt="测试图片"></div></body></html>`)
	var imageBytes bytes.Buffer
	if err := png.Encode(&imageBytes, image.NewRGBA(image.Rect(0, 0, 23, 17))); err != nil {
		t.Fatal(err)
	}
	store := &workflowStore{article: workspace.Article{ID: 11, CanonicalURL: pageURL.String()}, job: workspace.Job{ID: 7, ArticleID: 11, Status: "queued"}}
	objects := &fakeObjectStore{}
	service := &Service{
		store:   store,
		objects: objects,
		fetcher: workflowFetcher{
			page:  collectors.FetchResult{Body: html, FinalURL: pageURL, ContentType: "text/html"},
			image: collectors.FetchResult{Body: imageBytes.Bytes(), ContentType: "image/png"},
		},
		registry: collectors.NewRegistry(),
		cfg: config.Config{Collector: config.CollectorConfig{
			MaxImages: 100, ImageConcurrency: 4, ImageTimeout: time.Second,
			ArticleMaxBytes: 100 << 20,
		}},
	}
	if err := service.process(context.Background(), store.job.ID); err != nil {
		t.Fatal(err)
	}
	if !store.completed || !reflect.DeepEqual(store.stages, []string{"fetching", "parsing", "storing_assets"}) {
		t.Fatalf("completed=%v stages=%v", store.completed, store.stages)
	}
	if len(store.assets) != 1 || store.assets[0].Width != 23 || store.assets[0].Height != 17 || store.assets[0].DownloadStatus != "completed" {
		t.Fatalf("unexpected assets: %#v", store.assets)
	}
	if len(store.warnings) != 0 || store.rawKey == "" || len(objects.objects) != 2 {
		t.Fatalf("warnings=%v rawKey=%q objectCount=%d", store.warnings, store.rawKey, len(objects.objects))
	}
	reader, err := gzip.NewReader(bytes.NewReader(objects.objects[store.rawKey]))
	if err != nil {
		t.Fatal(err)
	}
	decompressed, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(decompressed, html) {
		t.Fatal("raw snapshot did not preserve source HTML")
	}
	wantHash := sha256.Sum256(imageBytes.Bytes())
	if store.assets[0].SHA256 != wantHash {
		t.Fatal("stored image hash mismatch")
	}
}
