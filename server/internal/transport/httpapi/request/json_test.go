package request_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/chenbb0128/weavepress/server/internal/transport/httpapi/middleware"
	"github.com/chenbb0128/weavepress/server/internal/transport/httpapi/request"
	"github.com/chenbb0128/weavepress/server/internal/transport/httpapi/response"
)

type sampleDTO struct {
	Username string `json:"username"`
	Age      int    `json:"age"`
}

func (s *sampleDTO) Validate() []response.ValidationDetail {
	if s.Username == "" {
		return []response.ValidationDetail{{Field: "username", Reason: "required"}}
	}
	return nil
}

func TestBindJSON(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		content    string
		accept     string
		limit      int64
		wantStatus int
		wantCode   int
	}{
		{name: "ok", body: `{"username":"hua","age":18}`, content: "application/json", accept: "application/json", limit: 1024, wantStatus: http.StatusOK, wantCode: response.CodeSuccess},
		{name: "missing content type", body: `{"username":"hua"}`, accept: "application/json", limit: 1024, wantStatus: http.StatusUnsupportedMediaType, wantCode: response.CodeUnsupportedMediaType},
		{name: "not acceptable", body: `{"username":"hua"}`, content: "application/json", accept: "text/html", limit: 1024, wantStatus: http.StatusNotAcceptable, wantCode: response.CodeNotAcceptable},
		{name: "unknown field", body: `{"username":"hua","extra":true}`, content: "application/json", accept: "application/json", limit: 1024, wantStatus: http.StatusUnprocessableEntity, wantCode: response.CodeValidationFailed},
		{name: "trailing value", body: `{"username":"hua"} {"username":"next"}`, content: "application/json", accept: "application/json", limit: 1024, wantStatus: http.StatusBadRequest, wantCode: response.CodeBadRequest},
		{name: "array body", body: `[{"username":"hua"}]`, content: "application/json", accept: "application/json", limit: 1024, wantStatus: http.StatusBadRequest, wantCode: response.CodeBadRequest},
		{name: "validation failed", body: `{"age":18}`, content: "application/json", accept: "application/json", limit: 1024, wantStatus: http.StatusUnprocessableEntity, wantCode: response.CodeValidationFailed},
		{name: "invalid type", body: `{"username":"hua","age":"old"}`, content: "application/json", accept: "application/json", limit: 1024, wantStatus: http.StatusUnprocessableEntity, wantCode: response.CodeValidationFailed},
		{name: "too large", body: `{"username":"hua"}`, content: "application/json", accept: "application/json", limit: 8, wantStatus: http.StatusRequestEntityTooLarge, wantCode: response.CodePayloadTooLarge},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router := newJSONRouter(tt.limit)
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/json", strings.NewReader(tt.body))
			if tt.content != "" {
				req.Header.Set("Content-Type", tt.content)
			}
			if tt.accept != "" {
				req.Header.Set("Accept", tt.accept)
			}

			router.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d: %s", rec.Code, tt.wantStatus, rec.Body.String())
			}

			var body map[string]any
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if body["code"] != float64(tt.wantCode) {
				t.Fatalf("code = %v, want %d", body["code"], tt.wantCode)
			}
		})
	}
}

func newJSONRouter(limit int64) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(middleware.BodyLimit(limit))
	router.POST("/json", func(c *gin.Context) {
		var dto sampleDTO
		if err := request.BindJSON(c, &dto); err != nil {
			response.Error(c, err)
			return
		}
		response.OK(c, gin.H{"username": dto.Username})
	})
	return router
}
