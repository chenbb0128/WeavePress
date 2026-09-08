package httpapi_test

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/chenbb0128/weavepress/server/internal/config"
	"github.com/chenbb0128/weavepress/server/internal/platform/requestid"
	"github.com/chenbb0128/weavepress/server/internal/transport/httpapi"
	"github.com/chenbb0128/weavepress/server/internal/transport/httpapi/response"
)

func TestRouterHealth(t *testing.T) {
	router := newTestRouter(t)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got := rec.Header().Get(requestid.Header); !requestid.IsValid(got) {
		t.Fatalf("response request id invalid: %q", got)
	}

	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["code"] != float64(response.CodeSuccess) {
		t.Fatalf("code = %v, want %d", body["code"], response.CodeSuccess)
	}
}

func TestRouterKeepsValidRequestID(t *testing.T) {
	router := newTestRouter(t)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	req.Header.Set(requestid.Header, "req-123.test")
	router.ServeHTTP(rec, req)

	if got := rec.Header().Get(requestid.Header); got != "req-123.test" {
		t.Fatalf("response request id = %q, want req-123.test", got)
	}
}

func TestRouterReplacesInvalidRequestID(t *testing.T) {
	router := newTestRouter(t)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	req.Header.Set(requestid.Header, "!!!")
	router.ServeHTTP(rec, req)

	got := rec.Header().Get(requestid.Header)
	if got == "!!!" {
		t.Fatal("invalid request id was echoed")
	}
	if !requestid.IsValid(got) {
		t.Fatalf("replacement request id invalid: %q", got)
	}
}

func TestRouterContextCarriesRequestID(t *testing.T) {
	router := newTestRouter(t)
	router.GET("/ctx", func(c *gin.Context) {
		fromContext, ok := requestid.FromContext(c.Request.Context())
		if !ok {
			response.Error(c, response.Internal(nil))
			return
		}
		fromGin, ok := c.Get(requestid.GinKey)
		if !ok || fromGin != fromContext {
			response.Error(c, response.Internal(nil))
			return
		}
		response.OK(c, gin.H{"request_id": fromContext})
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/ctx", nil)
	req.Header.Set(requestid.Header, "req-context")
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
}

func TestRouterNotFoundAndMethodNotAllowedUseEnvelope(t *testing.T) {
	router := newTestRouter(t)

	tests := []struct {
		name       string
		method     string
		path       string
		wantStatus int
		wantCode   int
	}{
		{name: "not found", method: http.MethodGet, path: "/missing", wantStatus: http.StatusNotFound, wantCode: response.CodeNotFound},
		{name: "method not allowed", method: http.MethodPost, path: "/health", wantStatus: http.StatusMethodNotAllowed, wantCode: response.CodeMethodNotAllowed},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(tt.method, tt.path, nil)
			router.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
			var body map[string]any
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode body: %v", err)
			}
			if body["code"] != float64(tt.wantCode) {
				t.Fatalf("code = %v, want %d", body["code"], tt.wantCode)
			}
			if got := rec.Header().Get(requestid.Header); !requestid.IsValid(got) {
				t.Fatalf("response request id invalid: %q", got)
			}
		})
	}
}

func TestRouterRecoveryUsesSafeEnvelope(t *testing.T) {
	router := newTestRouter(t)
	router.GET("/panic", func(c *gin.Context) {
		panic("secret stack detail")
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/panic", nil)
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
	if body := rec.Body.String(); body == "" || bodyContains(body, "secret stack detail") {
		t.Fatalf("response leaked panic detail or was empty: %s", body)
	}
}

func newTestRouter(t *testing.T) *gin.Engine {
	t.Helper()
	router, err := httpapi.NewRouter(httpapi.RouterOptions{
		HTTP: config.HTTPConfig{
			Addr:              ":0",
			ReadHeaderTimeout: time.Second,
			ReadTimeout:       time.Second,
			WriteTimeout:      time.Second,
			IdleTimeout:       time.Second,
			ShutdownTimeout:   time.Second,
			MaxHeaderBytes:    1 << 20,
			MaxBodyBytes:      1 << 20,
			TrustedProxies:    []string{},
			CORS: config.CORSConfig{
				AllowedOrigins: []string{"http://localhost:3000"},
				AllowedMethods: []string{"GET", "POST", "OPTIONS"},
				AllowedHeaders: []string{"Content-Type", "X-Request-ID"},
			},
		},
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("NewRouter() error = %v", err)
	}
	return router
}

func bodyContains(body string, value string) bool {
	return len(value) > 0 && len(body) >= len(value) && json.Valid([]byte(body)) && stringContains(body, value)
}

func stringContains(s string, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
