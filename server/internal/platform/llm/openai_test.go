package llm

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestOpenAICompleteMapsRequestAndUsage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", request.Method)
		}
		if request.URL.Path != "/v1/chat/completions" {
			t.Errorf("path = %s, want /v1/chat/completions", request.URL.Path)
		}
		if got := request.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Errorf("Authorization = %q, want Bearer test-key", got)
		}
		if got := request.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("Content-Type = %q, want application/json", got)
		}
		var body struct {
			Model          string    `json:"model"`
			Messages       []Message `json:"messages"`
			MaxTokens      int       `json:"max_tokens"`
			Temperature    float64   `json:"temperature"`
			ResponseFormat struct {
				Type string `json:"type"`
			} `json:"response_format"`
		}
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)
		}
		if body.Model != "test-model" {
			t.Errorf("model = %q, want test-model", body.Model)
		}
		if len(body.Messages) != 1 || body.Messages[0].Role != "system" || body.Messages[0].Content != "rules" {
			t.Errorf("messages = %#v", body.Messages)
		}
		if body.MaxTokens != 6000 {
			t.Errorf("max_tokens = %d, want 6000", body.MaxTokens)
		}
		if body.Temperature != 0.4 {
			t.Errorf("temperature = %v, want 0.4", body.Temperature)
		}
		if body.ResponseFormat.Type != "json_object" {
			t.Errorf("response_format.type = %q, want json_object", body.ResponseFormat.Type)
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(writer, `{"choices":[{"message":{"content":"{\"summary\":\"ok\"}"}}],"usage":{"prompt_tokens":11,"completion_tokens":7,"total_tokens":18}}`)
	}))
	defer server.Close()

	provider := NewOpenAICompatible(" "+server.URL+"/ ", "test-key", "test-model", time.Second)
	got, err := provider.Complete(context.Background(), Request{
		Messages:    []Message{{Role: "system", Content: "rules"}},
		MaxTokens:   6000,
		Temperature: 0.4,
		JSON:        true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Content != `{"summary":"ok"}` {
		t.Fatalf("content = %q", got.Content)
	}
	if got.Usage != (Usage{InputTokens: 11, OutputTokens: 7, TotalTokens: 18}) {
		t.Fatalf("usage = %#v", got.Usage)
	}
}

func TestOpenAICompleteMapsHTTPError(t *testing.T) {
	tests := []struct {
		name      string
		status    int
		code      string
		retryable bool
	}{
		{name: "unauthorized", status: http.StatusUnauthorized, code: "AI_PROVIDER_AUTH_FAILED"},
		{name: "forbidden", status: http.StatusForbidden, code: "AI_PROVIDER_AUTH_FAILED"},
		{name: "rate limited", status: http.StatusTooManyRequests, code: "AI_PROVIDER_RATE_LIMITED", retryable: true},
		{name: "bad request", status: http.StatusUnprocessableEntity, code: "AI_PROVIDER_REQUEST_FAILED"},
		{name: "server error", status: http.StatusInternalServerError, code: "AI_PROVIDER_UNAVAILABLE", retryable: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.WriteHeader(tt.status)
				_, _ = io.WriteString(writer, strings.Repeat("x", 128<<10))
			}))
			defer server.Close()

			provider := NewOpenAICompatible(server.URL, "test-key", "test-model", time.Second)
			_, err := provider.Complete(context.Background(), Request{})
			assertProviderError(t, err, tt.code, tt.retryable)
		})
	}
}

func TestOpenAICompleteMapsTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		time.Sleep(100 * time.Millisecond)
	}))
	defer server.Close()

	provider := NewOpenAICompatible(server.URL, "test-key", "test-model", 20*time.Millisecond)
	_, err := provider.Complete(context.Background(), Request{})
	assertProviderError(t, err, "AI_PROVIDER_TIMEOUT", true)
}

func TestOpenAICompleteMapsNetworkError(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	baseURL := "http://" + listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}

	provider := NewOpenAICompatible(baseURL, "test-key", "test-model", time.Second)
	_, err = provider.Complete(context.Background(), Request{})
	assertProviderError(t, err, "AI_PROVIDER_UNAVAILABLE", true)
}

func TestOpenAICompleteRejectsMalformedSuccessResponse(t *testing.T) {
	tests := []struct {
		name string
		body string
		json bool
	}{
		{name: "invalid response json", body: `{`, json: false},
		{name: "empty choices", body: `{"choices":[]}`, json: false},
		{name: "empty content", body: `{"choices":[{"message":{"content":"  "}}]}`, json: false},
		{name: "invalid json content", body: `{"choices":[{"message":{"content":"not-json"}}]}`, json: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(writer, tt.body)
			}))
			defer server.Close()

			provider := NewOpenAICompatible(server.URL, "test-key", "test-model", time.Second)
			_, err := provider.Complete(context.Background(), Request{JSON: tt.json})
			assertProviderError(t, err, "AI_PROVIDER_REQUEST_FAILED", false)
		})
	}
}

func assertProviderError(t *testing.T, err error, code string, retryable bool) {
	t.Helper()
	var providerErr *Error
	if !errors.As(err, &providerErr) {
		t.Fatalf("error = %#v, want *Error", err)
	}
	if providerErr.Code != code || providerErr.Retryable != retryable {
		t.Fatalf("error = %#v, want code=%s retryable=%v", providerErr, code, retryable)
	}
}
