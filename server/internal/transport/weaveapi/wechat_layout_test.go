package weaveapi

import (
	"bytes"
	"context"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"

	"github.com/chenbb0128/weavepress/server/internal/config"
	"github.com/chenbb0128/weavepress/server/internal/modules/authn"
	"github.com/chenbb0128/weavepress/server/internal/modules/content"
	"github.com/chenbb0128/weavepress/server/internal/modules/editorial"
	"github.com/chenbb0128/weavepress/server/internal/modules/workspace"
)

type weChatLayoutTestStore struct {
	workspace.Store
	user   workspace.User
	draft  editorial.Draft
	assets []editorial.DraftAsset
}

type weChatLayoutEditorialStore struct {
	editorial.Store
	state *weChatLayoutTestStore
}

func (s *weChatLayoutTestStore) GetUserByID(context.Context, uint64) (workspace.User, error) {
	return s.user, nil
}

func (s *weChatLayoutTestStore) GetArticle(context.Context, uint64) (workspace.Article, error) {
	return workspace.Article{}, workspace.ErrNotFound
}

func (s *weChatLayoutTestStore) GetAsset(context.Context, uint64) (workspace.Asset, error) {
	return workspace.Asset{}, workspace.ErrNotFound
}

func (s *weChatLayoutEditorialStore) GetDraft(_ context.Context, id uint64, _ bool) (editorial.Draft, error) {
	if id != s.state.draft.ID {
		return editorial.Draft{}, workspace.ErrNotFound
	}
	return s.state.draft, nil
}

func (s *weChatLayoutEditorialStore) ListDraftAssets(_ context.Context, draftID uint64) ([]editorial.DraftAsset, error) {
	result := make([]editorial.DraftAsset, 0, len(s.state.assets))
	for _, asset := range s.state.assets {
		if asset.DraftID == draftID {
			result = append(result, asset)
		}
	}
	return result, nil
}

func (s *weChatLayoutEditorialStore) GetDraftAssetByID(_ context.Context, assetID uint64) (editorial.DraftAsset, error) {
	for _, asset := range s.state.assets {
		if asset.ID == assetID {
			return asset, nil
		}
	}
	return editorial.DraftAsset{}, workspace.ErrNotFound
}

func (s *weChatLayoutEditorialStore) CreateUploadedDraftAsset(_ context.Context, draftID, userID uint64, input editorial.NewDraftAsset) (editorial.DraftAsset, error) {
	if s.state.draft.Status != editorial.StatusEditing {
		return editorial.DraftAsset{}, editorial.ErrDraftNotEditable
	}
	asset := editorial.DraftAsset{
		ID: uint64(len(s.state.assets) + 1), DraftID: draftID, Origin: "upload", ObjectKey: input.ObjectKey,
		MediaType: input.MediaType, ByteSize: input.ByteSize, Width: input.Width, Height: input.Height,
		SHA256: input.SHA256, UploadedBy: userID,
		BodyEligible:  input.ByteSize <= editorial.WeChatMaxContentImageSize,
		CoverEligible: input.ByteSize <= editorial.WeChatMaxCoverImageSize,
	}
	s.state.assets = append(s.state.assets, asset)
	return asset, nil
}

type weChatLayoutObjects struct {
	objects         map[string][]byte
	privateURL      string
	privateURLCalls int
}

func (s *weChatLayoutObjects) Put(_ context.Context, key string, body []byte, _ string) error {
	if s.objects == nil {
		s.objects = make(map[string][]byte)
	}
	s.objects[key] = append([]byte(nil), body...)
	return nil
}

func (s *weChatLayoutObjects) Open(_ context.Context, key string) (io.ReadCloser, error) {
	body, ok := s.objects[key]
	if !ok {
		return nil, workspace.ErrNotFound
	}
	return io.NopCloser(bytes.NewReader(body)), nil
}

func (s *weChatLayoutObjects) PrivateURL(string, time.Duration) (string, error) {
	s.privateURLCalls++
	return s.privateURL, nil
}

func TestDraftAssetUploadHTTPBoundaries(t *testing.T) {
	validPNG := validHTTPDraftPNG(t)
	tests := []struct {
		name       string
		status     string
		mediaType  string
		body       []byte
		token      bool
		wantStatus int
		wantText   string
	}{
		{name: "unauthenticated", status: editorial.StatusEditing, mediaType: "image/png", body: validPNG, wantStatus: http.StatusUnauthorized},
		{name: "not editing", status: editorial.StatusApproved, mediaType: "image/png", body: validPNG, token: true, wantStatus: http.StatusConflict},
		{name: "svg", status: editorial.StatusEditing, mediaType: "image/svg+xml", body: []byte(`<svg xmlns="http://www.w3.org/2000/svg"/>`), token: true, wantStatus: http.StatusBadRequest, wantText: "图片格式不支持"},
		{name: "over 10 MiB", status: editorial.StatusEditing, mediaType: "image/png", body: make([]byte, editorial.WeChatMaxCoverImageSize+1), token: true, wantStatus: http.StatusRequestEntityTooLarge, wantText: "封面及上传最大 10 MiB"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			api, token, _, _ := newWeChatLayoutTestAPI(t, test.status)
			if !test.token {
				token = ""
			}
			recorder := performMultipartRequest(t, api, "/api/drafts/7/assets", token, []multipartTestFile{{field: "file", name: "asset", mediaType: test.mediaType, body: test.body}})
			assertStatus(t, recorder, test.wantStatus)
			if test.wantText != "" && !strings.Contains(recorder.Body.String(), test.wantText) {
				t.Fatalf("response missing %q: %s", test.wantText, recorder.Body.String())
			}
		})
	}
}

func TestDraftAssetUploadRejectsAdditionalFiles(t *testing.T) {
	api, token, store, _ := newWeChatLayoutTestAPI(t, editorial.StatusEditing)
	imageBody := validHTTPDraftPNG(t)
	recorder := performMultipartRequest(t, api, "/api/drafts/7/assets", token, []multipartTestFile{
		{field: "file", name: "first.png", mediaType: "image/png", body: imageBody},
		{field: "extra", name: "second.png", mediaType: "image/png", body: imageBody},
	})
	assertStatus(t, recorder, http.StatusBadRequest)
	if len(store.assets) != 0 {
		t.Fatalf("assets created = %d", len(store.assets))
	}
}

func TestDraftAssetUploadReturnsDecoratedEligibility(t *testing.T) {
	api, token, store, objects := newWeChatLayoutTestAPI(t, editorial.StatusEditing)
	body := append(validHTTPDraftPNG(t), make([]byte, editorial.WeChatMaxContentImageSize)...)
	recorder := performMultipartRequest(t, api, "/api/drafts/7/assets", token, []multipartTestFile{{field: "file", name: "cover.png", mediaType: "image/png", body: body}})
	assertStatus(t, recorder, http.StatusCreated)
	responseBody := recorder.Body.String()
	for _, expected := range []string{`"origin":"upload"`, `"mediaUrl":"/media/draft-assets/1?`, `"bodyEligible":false`, `"coverEligible":true`} {
		if !strings.Contains(responseBody, expected) {
			t.Fatalf("response missing %s: %s", expected, responseBody)
		}
	}
	if len(store.assets) != 1 || len(objects.objects) != 1 {
		t.Fatalf("assets = %d, objects = %d", len(store.assets), len(objects.objects))
	}
}

func TestWeChatLayoutThemesAndDraftAssets(t *testing.T) {
	api, token, store, _ := newWeChatLayoutTestAPI(t, editorial.StatusEditing)
	store.assets = []editorial.DraftAsset{{ID: 5, DraftID: 7, Origin: "article", MediaType: "image/jpeg", BodyEligible: true, CoverEligible: true}}

	themes := performRequest(t, api, http.MethodGet, "/api/wechat-layout/themes", "", token)
	assertStatus(t, themes, http.StatusOK)
	if !strings.Contains(themes.Body.String(), `"minimal-business"`) || !strings.Contains(themes.Body.String(), `"vibrant-brand"`) {
		t.Fatalf("unexpected themes: %s", themes.Body.String())
	}

	assets := performRequest(t, api, http.MethodGet, "/api/drafts/7/assets", "", token)
	assertStatus(t, assets, http.StatusOK)
	if !strings.Contains(assets.Body.String(), `"id":5`) || !strings.Contains(assets.Body.String(), `"mediaUrl":"/media/draft-assets/5?`) {
		t.Fatalf("unexpected assets: %s", assets.Body.String())
	}
}

func TestDraftAssetPrivateMediaAccess(t *testing.T) {
	api, _, store, objects := newWeChatLayoutTestAPI(t, editorial.StatusEditing)
	body := validHTTPDraftPNG(t)
	const key = "drafts/2026/09/private.png"
	store.assets = []editorial.DraftAsset{{ID: 6, DraftID: 7, ObjectKey: key, MediaType: "image/png"}}
	objects.objects[key] = body
	objects.privateURL = "https://private-storage.example/" + key
	mediaURL := api.content.SignMedia("draft-assets", 6)

	recorder := performRawRequest(api, http.MethodGet, mediaURL, "")
	assertStatus(t, recorder, http.StatusOK)
	if recorder.Header().Get("Content-Type") != "image/png" || !bytes.Equal(recorder.Body.Bytes(), body) {
		t.Fatalf("media response type=%q bytes=%d", recorder.Header().Get("Content-Type"), recorder.Body.Len())
	}
	for name, values := range recorder.Header() {
		if strings.Contains(strings.Join(values, "\n"), key) {
			t.Fatalf("response header %q leaked object key %q: %q", name, key, values)
		}
	}
	if strings.Contains(recorder.Body.String(), key) {
		t.Fatalf("response body leaked object key %q", key)
	}
	if objects.privateURLCalls != 0 {
		t.Fatalf("PrivateURL calls = %d, want 0", objects.privateURLCalls)
	}

	recorder = performRawRequest(api, http.MethodGet, "/media/draft-assets/6?expires=1&signature=bad", "")
	assertStatus(t, recorder, http.StatusForbidden)
}

func newWeChatLayoutTestAPI(t *testing.T, status string) (*API, string, *weChatLayoutTestStore, *weChatLayoutObjects) {
	t.Helper()
	const secret = "test-jwt-secret-that-is-at-least-32-characters"
	user := workspace.User{ID: 42, Username: "editor", Role: workspace.RoleEditor, Status: "active"}
	store := &weChatLayoutTestStore{user: user, draft: editorial.Draft{ID: 7, Status: status}}
	objects := &weChatLayoutObjects{objects: make(map[string][]byte)}
	cfg := config.Config{Auth: config.AuthConfig{JWTSecret: secret, MediaSigningKey: "media-signing-test-key", MediaURLTTL: time.Hour}}
	authService := authn.New(store, nil, cfg.Auth)
	contentService := content.New(store, nil, objects, cfg)
	editorialService := editorial.New(&weChatLayoutEditorialStore{state: store}, store, nil, nil, objects, true)
	api := New(store, authService, contentService, editorialService, nil, cfg)
	claims := authn.Claims{Role: user.Role, RegisteredClaims: jwt.RegisteredClaims{
		Issuer: "weavepress", Subject: "42", ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
	}}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(secret))
	if err != nil {
		t.Fatal(err)
	}
	return api, token, store, objects
}

type multipartTestFile struct {
	field     string
	name      string
	mediaType string
	body      []byte
}

func performMultipartRequest(t *testing.T, api *API, path, token string, files []multipartTestFile) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for _, file := range files {
		header := make(textproto.MIMEHeader)
		header.Set("Content-Disposition", `form-data; name="`+file.field+`"; filename="`+file.name+`"`)
		header.Set("Content-Type", file.mediaType)
		part, err := writer.CreatePart(header)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := part.Write(file.body); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, path, &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	return serveWeChatLayoutRequest(api, request)
}

func performRawRequest(api *API, method, path, token string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, nil)
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	return serveWeChatLayoutRequest(api, request)
}

func serveWeChatLayoutRequest(api *API, request *http.Request) *httptest.ResponseRecorder {
	router := newTestGinRouter(api)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	return recorder
}

func newTestGinRouter(api *API) http.Handler {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	api.Register(router)
	return router
}

func validHTTPDraftPNG(t *testing.T) []byte {
	t.Helper()
	return []byte{
		0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a,
		0x00, 0x00, 0x00, 0x0d, 0x49, 0x48, 0x44, 0x52,
		0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
		0x08, 0x02, 0x00, 0x00, 0x00, 0x90, 0x77, 0x53,
		0xde, 0x00, 0x00, 0x00, 0x0c, 0x49, 0x44, 0x41,
		0x54, 0x08, 0xd7, 0x63, 0xf8, 0xcf, 0xc0, 0x00, 0x00,
		0x03, 0x01, 0x01, 0x00, 0x18, 0xdd, 0x8d, 0xb0, 0x00,
		0x00, 0x00, 0x00, 0x49, 0x45, 0x4e, 0x44, 0xae, 0x42,
		0x60, 0x82,
	}
}
