package weaveapi

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/chenbb0128/weavepress/server/internal/config"
	"github.com/chenbb0128/weavepress/server/internal/modules/aisettings"
	"github.com/chenbb0128/weavepress/server/internal/modules/authn"
	"github.com/chenbb0128/weavepress/server/internal/modules/workspace"
)

type fakeAISettingsService struct {
	view      aisettings.SettingsView
	input     aisettings.UpdateInput
	userID    uint64
	viewCalls int
	err       error
}

func (s *fakeAISettingsService) View(context.Context) (aisettings.SettingsView, error) {
	s.viewCalls++
	return s.view, s.err
}

func (s *fakeAISettingsService) Update(_ context.Context, userID uint64, input aisettings.UpdateInput) (aisettings.SettingsView, error) {
	s.userID, s.input = userID, input
	return s.view, s.err
}

func TestAISettingsRoutesRequireAdmin(t *testing.T) {
	for _, test := range []struct {
		method string
		body   string
	}{
		{method: http.MethodGet},
		{method: http.MethodPut, body: `{"enabled":true,"activeProvider":"zhipu","baseUrl":"","model":"glm-5.3-flash","apiKey":"secret"}`},
	} {
		api, token, _ := newAISettingsTestAPI(t, workspace.RoleEditor)
		recorder := performRequest(t, api, test.method, "/api/ai/settings", test.body, token)
		assertStatus(t, recorder, http.StatusForbidden)
	}
}

func TestGetAISettingsReturnsSafeAdminView(t *testing.T) {
	api, token, settings := newAISettingsTestAPI(t, workspace.RoleAdmin)
	settings.view = aisettings.SettingsView{
		Enabled: true, ActiveProvider: aisettings.ProviderZhipu,
		Providers: []aisettings.ProviderView{{ID: aisettings.ProviderZhipu, Name: "智谱 GLM", Model: "glm-5.3-flash", KeyConfigured: true}},
	}
	recorder := performRequest(t, api, http.MethodGet, "/api/ai/settings", "", token)
	assertStatus(t, recorder, http.StatusOK)
	body := recorder.Body.String()
	if settings.viewCalls != 1 || !strings.Contains(body, `"keyConfigured":true`) || strings.Contains(strings.ToLower(body), "apikey") || strings.Contains(body, "secret-value") {
		t.Fatalf("unsafe or incomplete response: %s", body)
	}
}

func TestUpdateAISettingsUsesAuthenticatedAdminAndStrictJSON(t *testing.T) {
	api, token, settings := newAISettingsTestAPI(t, workspace.RoleAdmin)
	settings.view = aisettings.SettingsView{Enabled: true, ActiveProvider: aisettings.ProviderOpenAI}
	body := `{"enabled":true,"activeProvider":"openai","baseUrl":"https://ignored.invalid","model":"gpt-5-mini","apiKey":"sk-new"}`
	recorder := performRequest(t, api, http.MethodPut, "/api/ai/settings", body, token)
	assertStatus(t, recorder, http.StatusOK)
	if settings.userID != 42 || settings.input.ActiveProvider != aisettings.ProviderOpenAI || settings.input.APIKey != "sk-new" {
		t.Fatalf("user=%d input=%#v", settings.userID, settings.input)
	}

	unknown := `{"enabled":true,"activeProvider":"openai","baseUrl":"","model":"gpt-5-mini","apiKey":"","plaintextKey":"leak"}`
	recorder = performRequest(t, api, http.MethodPut, "/api/ai/settings", unknown, token)
	assertStatus(t, recorder, http.StatusUnprocessableEntity)
}

func TestUpdateAISettingsReturnsConflictForConcurrentChange(t *testing.T) {
	api, token, settings := newAISettingsTestAPI(t, workspace.RoleAdmin)
	settings.err = aisettings.ErrSettingsConflict
	body := `{"enabled":true,"activeProvider":"openai","baseUrl":"","model":"gpt-5-mini","apiKey":""}`
	recorder := performRequest(t, api, http.MethodPut, "/api/ai/settings", body, token)
	assertStatus(t, recorder, http.StatusConflict)
}

func newAISettingsTestAPI(t *testing.T, role workspace.Role) (*API, string, *fakeAISettingsService) {
	t.Helper()
	const secret = "test-jwt-secret-that-is-at-least-32-characters"
	user := workspace.User{ID: 42, Username: "admin", Role: role, Status: "active"}
	store := aiTestStore{user: user}
	cfg := config.Config{Auth: config.AuthConfig{JWTSecret: secret}}
	authService := authn.New(store, nil, cfg.Auth)
	settings := &fakeAISettingsService{}
	api := NewWithAISettings(store, authService, nil, nil, &fakeAIService{}, settings, cfg)
	claims := authn.Claims{Role: role, RegisteredClaims: jwt.RegisteredClaims{
		Issuer: "weavepress", Subject: "42", ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
	}}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(secret))
	if err != nil {
		t.Fatal(err)
	}
	return api, token, settings
}
