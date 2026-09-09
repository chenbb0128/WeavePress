package weaveapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"

	"github.com/chenbb0128/weavepress/server/internal/config"
	"github.com/chenbb0128/weavepress/server/internal/modules/aiwriting"
	"github.com/chenbb0128/weavepress/server/internal/modules/authn"
	"github.com/chenbb0128/weavepress/server/internal/modules/workspace"
)

type aiTestStore struct {
	workspace.Store
	user workspace.User
}

func (s aiTestStore) GetUserByID(context.Context, uint64) (workspace.User, error) {
	return s.user, nil
}

type fakeAIService struct {
	status     aiwriting.Status
	job        aiwriting.Job
	analysis   aiwriting.Analysis
	generation aiwriting.Generation
	analyses   aiwriting.Page[aiwriting.Analysis]
	jobs       aiwriting.Page[aiwriting.Job]
	reused     bool
	err        error

	call       string
	articleID  uint64
	analysisID uint64
	jobID      uint64
	userID     uint64
	force      bool
	page       int
	pageSize   int
	filter     aiwriting.JobFilter
	params     aiwriting.GenerationParams
}

func (s *fakeAIService) Status() aiwriting.Status {
	s.call = "status"
	return s.status
}

func (s *fakeAIService) StartAnalysis(_ context.Context, articleID, userID uint64, force bool) (aiwriting.Job, bool, error) {
	s.call, s.articleID, s.userID, s.force = "start-analysis", articleID, userID, force
	return s.job, s.reused, s.err
}

func (s *fakeAIService) Analyses(_ context.Context, articleID uint64, page, pageSize int) (aiwriting.Page[aiwriting.Analysis], error) {
	s.call, s.articleID, s.page, s.pageSize = "analyses", articleID, page, pageSize
	return s.analyses, s.err
}

func (s *fakeAIService) Analysis(_ context.Context, id uint64) (aiwriting.Analysis, error) {
	s.call, s.analysisID = "analysis", id
	return s.analysis, s.err
}

func (s *fakeAIService) StartGeneration(_ context.Context, analysisID, userID uint64, params aiwriting.GenerationParams) (aiwriting.Generation, aiwriting.Job, bool, error) {
	s.call, s.analysisID, s.userID, s.params = "start-generation", analysisID, userID, params
	return s.generation, s.job, s.reused, s.err
}

func (s *fakeAIService) Generation(_ context.Context, id uint64) (aiwriting.Generation, error) {
	s.call, s.analysisID = "generation", id
	return s.generation, s.err
}

func (s *fakeAIService) Jobs(_ context.Context, filter aiwriting.JobFilter, page, pageSize int) (aiwriting.Page[aiwriting.Job], error) {
	s.call, s.filter, s.page, s.pageSize = "jobs", filter, page, pageSize
	return s.jobs, s.err
}

func (s *fakeAIService) Job(_ context.Context, id uint64) (aiwriting.Job, error) {
	s.call, s.jobID = "job", id
	return s.job, s.err
}

func (s *fakeAIService) Retry(_ context.Context, jobID, userID uint64) (aiwriting.Job, error) {
	s.call, s.jobID, s.userID = "retry", jobID, userID
	return s.job, s.err
}

func TestAIRoutesAreProtected(t *testing.T) {
	tests := []struct {
		method string
		path   string
		body   string
	}{
		{http.MethodGet, "/api/ai/status", ""},
		{http.MethodPost, "/api/articles/1/ai-analyses", `{}`},
		{http.MethodGet, "/api/articles/1/ai-analyses", ""},
		{http.MethodGet, "/api/ai-analyses/1", ""},
		{http.MethodPost, "/api/ai-analyses/1/generations", validGenerationJSON()},
		{http.MethodGet, "/api/ai-generations/1", ""},
		{http.MethodGet, "/api/ai-jobs", ""},
		{http.MethodGet, "/api/ai-jobs/1", ""},
		{http.MethodPost, "/api/ai-jobs/1/retry", ""},
	}
	for _, tt := range tests {
		t.Run(tt.method+" "+tt.path, func(t *testing.T) {
			api, _ := newAITestAPI(t, &fakeAIService{}, workspace.RoleEditor)
			recorder := performRequest(t, api, tt.method, tt.path, tt.body, "")
			if recorder.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401; body=%s", recorder.Code, recorder.Body.String())
			}
		})
	}
}

func TestRequireCodeRejectsMissingPermission(t *testing.T) {
	gin.SetMode(gin.TestMode)
	authService := authn.New(nil, nil, config.AuthConfig{})
	api := &API{auth: authService}
	router := gin.New()
	router.GET("/", func(c *gin.Context) {
		c.Set(claimsKey, authn.Claims{Role: workspace.RoleEditor})
		c.Next()
	}, api.requireCode("user:view"), func(c *gin.Context) { c.Status(http.StatusNoContent) })
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403; body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestAIStatusRoute(t *testing.T) {
	fake := &fakeAIService{status: aiwriting.Status{Enabled: true, Provider: "openai-compatible", Model: "example-model"}}
	api, token := newAITestAPI(t, fake, workspace.RoleEditor)
	recorder := performRequest(t, api, http.MethodGet, "/api/ai/status", "", token)
	assertStatus(t, recorder, http.StatusOK)
	if fake.call != "status" || !strings.Contains(recorder.Body.String(), `"enabled":true`) {
		t.Fatalf("call=%q body=%s", fake.call, recorder.Body.String())
	}
}

func TestStartAnalysisStrictInputAndAuthenticatedUser(t *testing.T) {
	for _, tt := range []struct {
		name  string
		body  string
		force bool
	}{
		{name: "force true", body: `{"force":true}`, force: true},
		{name: "force false", body: `{"force":false}`, force: false},
		{name: "empty object", body: `{}`, force: false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			fake := &fakeAIService{job: aiwriting.Job{ID: 9, InputFingerprint: [32]byte{1}}, reused: true}
			api, token := newAITestAPI(t, fake, workspace.RoleEditor)
			recorder := performRequest(t, api, http.MethodPost, "/api/articles/7/ai-analyses", tt.body, token)
			assertStatus(t, recorder, http.StatusAccepted)
			if fake.call != "start-analysis" || fake.articleID != 7 || fake.userID != 42 || fake.force != tt.force {
				t.Fatalf("captured fake = %#v", fake)
			}
			body := recorder.Body.String()
			if !strings.Contains(body, `"reused":true`) || strings.Contains(body, "inputFingerprint") {
				t.Fatalf("unsafe or incomplete response: %s", body)
			}
		})
	}

	for _, tt := range []struct {
		name       string
		body       string
		wantStatus int
	}{
		{name: "unknown field", body: `{"force":false,"requestedBy":999}`, wantStatus: http.StatusUnprocessableEntity},
		{name: "trailing JSON", body: `{"force":false}{}`, wantStatus: http.StatusBadRequest},
		{name: "empty body", body: ``, wantStatus: http.StatusBadRequest},
	} {
		t.Run(tt.name, func(t *testing.T) {
			api, token := newAITestAPI(t, &fakeAIService{}, workspace.RoleEditor)
			recorder := performRequest(t, api, http.MethodPost, "/api/articles/7/ai-analyses", tt.body, token)
			assertStatus(t, recorder, tt.wantStatus)
		})
	}
}

func TestStartGenerationForwardsValidatedInput(t *testing.T) {
	fake := &fakeAIService{
		generation: aiwriting.Generation{ID: 13},
		job:        aiwriting.Job{ID: 14},
		reused:     true,
	}
	api, token := newAITestAPI(t, fake, workspace.RoleAdmin)
	recorder := performRequest(t, api, http.MethodPost, "/api/ai-analyses/12/generations", validGenerationJSON(), token)
	assertStatus(t, recorder, http.StatusAccepted)
	if fake.call != "start-generation" || fake.analysisID != 12 || fake.userID != 42 {
		t.Fatalf("captured fake = %#v", fake)
	}
	want := aiwriting.GenerationParams{AngleID: "A1", Audience: "产品经理", Tone: "analytical", TargetWords: 1200, AdditionalInstructions: "突出风险", IdempotencyKey: "request-123"}
	if fake.params != want {
		t.Fatalf("params = %#v, want %#v", fake.params, want)
	}
	body := recorder.Body.String()
	if !strings.Contains(body, `"generation":{"id":13`) || !strings.Contains(body, `"job":{"id":14`) || !strings.Contains(body, `"reused":true`) {
		t.Fatalf("unexpected response: %s", body)
	}
}

func TestStartGenerationRejectsUnknownAndTrailingJSON(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		wantStatus int
	}{
		{name: "unknown field", body: strings.TrimSuffix(validGenerationJSON(), "}") + `,"requestedBy":999}`, wantStatus: http.StatusUnprocessableEntity},
		{name: "trailing JSON", body: validGenerationJSON() + `{}`, wantStatus: http.StatusBadRequest},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := &fakeAIService{}
			api, token := newAITestAPI(t, fake, workspace.RoleEditor)
			recorder := performRequest(t, api, http.MethodPost, "/api/ai-analyses/12/generations", tt.body, token)
			assertStatus(t, recorder, tt.wantStatus)
			if fake.call != "" {
				t.Fatalf("service called for invalid input: %q", fake.call)
			}
		})
	}
}

func TestGenerationInputValidationDetails(t *testing.T) {
	valid := generationInput{AngleID: "A1", Audience: "读者", Tone: "professional", TargetWords: 300, IdempotencyKey: "12345678"}
	if details := valid.Validate(); len(details) != 0 {
		t.Fatalf("valid input details = %#v", details)
	}
	tests := []struct {
		name  string
		alter func(*generationInput)
		field string
	}{
		{name: "angle required", alter: func(i *generationInput) { i.AngleID = "" }, field: "angleId"},
		{name: "audience required", alter: func(i *generationInput) { i.Audience = "" }, field: "audience"},
		{name: "audience rune limit", alter: func(i *generationInput) { i.Audience = strings.Repeat("界", 101) }, field: "audience"},
		{name: "tone enum", alter: func(i *generationInput) { i.Tone = "sales" }, field: "tone"},
		{name: "target words low", alter: func(i *generationInput) { i.TargetWords = 299 }, field: "targetWords"},
		{name: "target words high", alter: func(i *generationInput) { i.TargetWords = 5001 }, field: "targetWords"},
		{name: "instructions rune limit", alter: func(i *generationInput) { i.AdditionalInstructions = strings.Repeat("界", 501) }, field: "additionalInstructions"},
		{name: "idempotency key short", alter: func(i *generationInput) { i.IdempotencyKey = "1234567" }, field: "idempotencyKey"},
		{name: "idempotency key long", alter: func(i *generationInput) { i.IdempotencyKey = strings.Repeat("x", 129) }, field: "idempotencyKey"},
		{name: "idempotency key ASCII", alter: func(i *generationInput) { i.IdempotencyKey = "中文请求标识" }, field: "idempotencyKey"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := valid
			tt.alter(&input)
			details := input.Validate()
			if len(details) != 1 || details[0].Field != tt.field {
				t.Fatalf("details = %#v, want field %q", details, tt.field)
			}
		})
	}
}

func TestAIQueryAndPathValidation(t *testing.T) {
	tests := []struct {
		name   string
		method string
		path   string
		body   string
		status int
		field  string
	}{
		{name: "zero path ID", method: http.MethodGet, path: "/api/ai-jobs/0", status: http.StatusBadRequest},
		{name: "negative path ID", method: http.MethodGet, path: "/api/ai-jobs/-1", status: http.StatusBadRequest},
		{name: "invalid page", method: http.MethodGet, path: "/api/ai-jobs?page=x", status: http.StatusUnprocessableEntity, field: "page"},
		{name: "page below one", method: http.MethodGet, path: "/api/ai-jobs?page=0", status: http.StatusUnprocessableEntity, field: "page"},
		{name: "invalid page size", method: http.MethodGet, path: "/api/ai-jobs?pageSize=101", status: http.StatusUnprocessableEntity, field: "pageSize"},
		{name: "invalid type", method: http.MethodGet, path: "/api/ai-jobs?type=other", status: http.StatusUnprocessableEntity, field: "type"},
		{name: "invalid status", method: http.MethodGet, path: "/api/ai-jobs?status=waiting", status: http.StatusUnprocessableEntity, field: "status"},
		{name: "invalid article ID", method: http.MethodGet, path: "/api/ai-jobs?articleId=nope", status: http.StatusUnprocessableEntity, field: "articleId"},
		{name: "zero article ID", method: http.MethodGet, path: "/api/ai-jobs?articleId=0", status: http.StatusUnprocessableEntity, field: "articleId"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			api, token := newAITestAPI(t, &fakeAIService{}, workspace.RoleEditor)
			recorder := performRequest(t, api, tt.method, tt.path, tt.body, token)
			assertStatus(t, recorder, tt.status)
			if tt.field != "" && !strings.Contains(recorder.Body.String(), `"field":"`+tt.field+`"`) {
				t.Fatalf("response missing field %q: %s", tt.field, recorder.Body.String())
			}
		})
	}
}

func TestAIListQueriesRejectUnknownRepeatedAndOverflowingValues(t *testing.T) {
	maxInt := strconv.FormatUint(uint64(^uint(0)>>1), 10)
	tests := []struct {
		name  string
		path  string
		field string
	}{
		{name: "analyses unknown key", path: "/api/articles/8/ai-analyses?type=analysis", field: "type"},
		{name: "analyses repeated value", path: "/api/articles/8/ai-analyses?page=1&page=2", field: "page"},
		{name: "jobs unknown key", path: "/api/ai-jobs?sort=createdAt", field: "sort"},
		{name: "jobs repeated value", path: "/api/ai-jobs?status=queued&status=failed", field: "status"},
		{name: "jobs overflowing offset", path: "/api/ai-jobs?page=" + maxInt, field: "page"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := &fakeAIService{}
			api, token := newAITestAPI(t, fake, workspace.RoleEditor)
			recorder := performRequest(t, api, http.MethodGet, tt.path, "", token)
			assertStatus(t, recorder, http.StatusUnprocessableEntity)
			if !strings.Contains(recorder.Body.String(), `"field":"`+tt.field+`"`) {
				t.Fatalf("response missing field %q: %s", tt.field, recorder.Body.String())
			}
			if fake.call != "" {
				t.Fatalf("service called for invalid query: %q", fake.call)
			}
		})
	}
}

func TestAIQueryRoutesReturnPagesAndForwardFilters(t *testing.T) {
	fake := &fakeAIService{
		analyses: aiwriting.Page[aiwriting.Analysis]{Items: []aiwriting.Analysis{{ID: 2}}, Total: 1, Page: 2, PageSize: 10},
		jobs:     aiwriting.Page[aiwriting.Job]{Items: []aiwriting.Job{{ID: 3}}, Total: 1, Page: 3, PageSize: 5},
	}
	api, token := newAITestAPI(t, fake, workspace.RoleEditor)
	recorder := performRequest(t, api, http.MethodGet, "/api/articles/8/ai-analyses?page=2&pageSize=10", "", token)
	assertStatus(t, recorder, http.StatusOK)
	if fake.call != "analyses" || fake.articleID != 8 || fake.page != 2 || fake.pageSize != 10 || !strings.Contains(recorder.Body.String(), `"pageSize":10`) {
		t.Fatalf("fake=%#v body=%s", fake, recorder.Body.String())
	}

	recorder = performRequest(t, api, http.MethodGet, "/api/ai-jobs?type=generation&status=failed&articleId=8&page=3&pageSize=5", "", token)
	assertStatus(t, recorder, http.StatusOK)
	if fake.call != "jobs" || fake.filter != (aiwriting.JobFilter{Type: "generation", Status: "failed", ArticleID: 8}) || fake.page != 3 || fake.pageSize != 5 || !strings.Contains(recorder.Body.String(), `"page":3`) {
		t.Fatalf("fake=%#v body=%s", fake, recorder.Body.String())
	}
}

func TestAIGetAndRetryRoutes(t *testing.T) {
	tests := []struct {
		name       string
		method     string
		path       string
		wantStatus int
		wantCall   string
	}{
		{name: "analysis", method: http.MethodGet, path: "/api/ai-analyses/21", wantStatus: http.StatusOK, wantCall: "analysis"},
		{name: "generation", method: http.MethodGet, path: "/api/ai-generations/22", wantStatus: http.StatusOK, wantCall: "generation"},
		{name: "job", method: http.MethodGet, path: "/api/ai-jobs/23", wantStatus: http.StatusOK, wantCall: "job"},
		{name: "retry", method: http.MethodPost, path: "/api/ai-jobs/24/retry", wantStatus: http.StatusAccepted, wantCall: "retry"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := &fakeAIService{analysis: aiwriting.Analysis{ID: 21}, generation: aiwriting.Generation{ID: 22}, job: aiwriting.Job{ID: 23}}
			api, token := newAITestAPI(t, fake, workspace.RoleEditor)
			recorder := performRequest(t, api, tt.method, tt.path, "", token)
			assertStatus(t, recorder, tt.wantStatus)
			if fake.call != tt.wantCall {
				t.Fatalf("call = %q, want %q", fake.call, tt.wantCall)
			}
			if tt.wantCall == "retry" && (fake.jobID != 24 || fake.userID != 42) {
				t.Fatalf("retry captured fake = %#v", fake)
			}
		})
	}
}

func TestRetryAIJobDisabledReturnsSafe503(t *testing.T) {
	fake := &fakeAIService{err: aiwriting.ErrNotConfigured}
	api, token := newAITestAPI(t, fake, workspace.RoleEditor)
	recorder := performRequest(t, api, http.MethodPost, "/api/ai-jobs/24/retry", "", token)
	assertStatus(t, recorder, http.StatusServiceUnavailable)
	if fake.call != "retry" || strings.Contains(recorder.Body.String(), aiwriting.ErrNotConfigured.Error()) {
		t.Fatalf("call=%q unsafe body=%s", fake.call, recorder.Body.String())
	}
}

func TestListAIAnalysesMissingArticleReturns404(t *testing.T) {
	fake := &fakeAIService{err: workspace.ErrNotFound}
	api, token := newAITestAPI(t, fake, workspace.RoleEditor)
	recorder := performRequest(t, api, http.MethodGet, "/api/articles/99/ai-analyses", "", token)
	assertStatus(t, recorder, http.StatusNotFound)
	if fake.call != "analyses" || fake.articleID != 99 {
		t.Fatalf("captured fake = %#v", fake)
	}
}

func TestAIErrorMappingIsSafe(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		status int
	}{
		{name: "not configured", err: aiwriting.ErrNotConfigured, status: http.StatusServiceUnavailable},
		{name: "input too large", err: aiwriting.ErrInputTooLarge, status: http.StatusRequestEntityTooLarge},
		{name: "invalid parameters", err: aiwriting.ErrInvalidParameters, status: http.StatusBadRequest},
		{name: "invalid output", err: aiwriting.ErrOutputInvalid, status: http.StatusBadRequest},
		{name: "invalid reference", err: aiwriting.ErrSourceReferenceInvalid, status: http.StatusBadRequest},
		{name: "quote mismatch", err: aiwriting.ErrQuoteMismatch, status: http.StatusBadRequest},
		{name: "invalid asset", err: aiwriting.ErrAssetInvalid, status: http.StatusBadRequest},
		{name: "source overlap", err: aiwriting.ErrExcessiveSourceOverlap, status: http.StatusBadRequest},
		{name: "article not ready", err: aiwriting.ErrArticleNotReady, status: http.StatusConflict},
		{name: "analysis not ready", err: aiwriting.ErrAnalysisNotReady, status: http.StatusConflict},
		{name: "AI job not retryable", err: aiwriting.ErrJobNotRetryable, status: http.StatusConflict},
		{name: "state conflict", err: workspace.ErrJobStateConflict, status: http.StatusConflict},
		{name: "not found", err: workspace.ErrNotFound, status: http.StatusNotFound},
		{name: "unknown", err: errors.New("SQL failed; api-key=secret; provider-body=raw"), status: http.StatusInternalServerError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			recorder := httptest.NewRecorder()
			context, _ := gin.CreateTestContext(recorder)
			(&API{}).writeError(context, tt.err)
			assertStatus(t, recorder, tt.status)
			body := recorder.Body.String()
			for _, sensitive := range []string{"secret", "provider-body", "SQL failed", "api-key"} {
				if strings.Contains(body, sensitive) {
					t.Fatalf("response leaked %q: %s", sensitive, body)
				}
			}
		})
	}
}

func newAITestAPI(t *testing.T, ai AIService, role workspace.Role) (*API, string) {
	t.Helper()
	const secret = "test-jwt-secret-that-is-at-least-32-characters"
	user := workspace.User{ID: 42, Username: "editor", Role: role, Status: "active"}
	store := aiTestStore{user: user}
	cfg := config.Config{Auth: config.AuthConfig{JWTSecret: secret}}
	authService := authn.New(store, nil, cfg.Auth)
	api := New(store, authService, nil, nil, ai, cfg)
	claims := authn.Claims{Role: role, RegisteredClaims: jwt.RegisteredClaims{
		Issuer: "weavepress", Subject: "42", ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
	}}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(secret))
	if err != nil {
		t.Fatal(err)
	}
	return api, token
}

func performRequest(t *testing.T, api *API, method, path, body, token string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	api.Register(router)
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" || method == http.MethodPost {
		request.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	return recorder
}

func assertStatus(t *testing.T, recorder *httptest.ResponseRecorder, want int) {
	t.Helper()
	if recorder.Code != want {
		var decoded any
		_ = json.Unmarshal(recorder.Body.Bytes(), &decoded)
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, want, recorder.Body.String())
	}
}

func validGenerationJSON() string {
	return `{"angleId":"A1","audience":"产品经理","tone":"analytical","targetWords":1200,"additionalInstructions":"突出风险","idempotencyKey":"request-123"}`
}
