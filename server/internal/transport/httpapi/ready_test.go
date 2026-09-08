package httpapi_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/chenbb0128/weavepress/server/internal/config"
	"github.com/chenbb0128/weavepress/server/internal/transport/httpapi"
	"github.com/chenbb0128/weavepress/server/internal/transport/httpapi/response"
)

func TestReadyFailsWhenDependencyFails(t *testing.T) {
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
				AllowedMethods: []string{"GET", "OPTIONS"},
				AllowedHeaders: []string{"Content-Type", "X-Request-ID"},
			},
		},
		Logger:       slog.New(slog.NewTextHandler(io.Discard, nil)),
		ReadyTimeout: time.Second,
		ReadinessChecks: []httpapi.ReadyCheck{{
			Name: "mysql",
			Check: func(context.Context) error {
				return errors.New("dsn password should not leak")
			},
		}},
	})
	if err != nil {
		t.Fatalf("NewRouter() error = %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/ready", nil)
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}

	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["code"] != float64(response.CodeDependencyUnavailable) {
		t.Fatalf("code = %v, want %d", body["code"], response.CodeDependencyUnavailable)
	}
	if _, ok := body["data"]; ok {
		t.Fatalf("error response contains data: %v", body)
	}
	if strings.Contains(rec.Body.String(), "dsn password should not leak") {
		t.Fatalf("response leaked dependency error: %s", rec.Body.String())
	}
}
