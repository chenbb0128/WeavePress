package llm

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestOpenAICompleteMapsRequestAndUsage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", request.Method)
		}
		if request.URL.Path != "/proxy/v1/chat/completions" {
			t.Errorf("path = %s, want /proxy/v1/chat/completions", request.URL.Path)
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

	provider := NewOpenAICompatible(" "+server.URL+"/proxy/ ", "test-key", "test-model", time.Second)
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
		{name: "unauthorized", status: http.StatusUnauthorized, code: ErrorCodeAuthFailed},
		{name: "forbidden", status: http.StatusForbidden, code: ErrorCodeAuthFailed},
		{name: "rate limited", status: http.StatusTooManyRequests, code: ErrorCodeRateLimited, retryable: true},
		{name: "bad request", status: http.StatusUnprocessableEntity, code: ErrorCodeRequestFailed},
		{name: "server error", status: http.StatusInternalServerError, code: ErrorCodeUnavailable, retryable: true},
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
	assertProviderError(t, err, ErrorCodeTimeout, true)
}

func TestOpenAICompleteDoesNotFollowRedirects(t *testing.T) {
	var redirectedRequests atomic.Int32
	var receivedAuthorization atomic.Bool
	var receivedBody atomic.Bool
	redirected := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		redirectedRequests.Add(1)
		receivedAuthorization.Store(request.Header.Get("Authorization") != "")
		body, _ := io.ReadAll(request.Body)
		receivedBody.Store(len(body) != 0)
		_, _ = io.WriteString(writer, `{"choices":[{"message":{"content":"ok"}}]}`)
	}))
	defer redirected.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Location", redirected.URL)
		writer.WriteHeader(http.StatusTemporaryRedirect)
	}))
	defer origin.Close()

	provider := NewOpenAICompatible(origin.URL, "test-key", "test-model", time.Second)
	_, err := provider.Complete(context.Background(), Request{Messages: []Message{{Role: "user", Content: "secret body"}}})
	assertProviderError(t, err, ErrorCodeRequestFailed, false)
	if redirectedRequests.Load() != 0 || receivedAuthorization.Load() || receivedBody.Load() {
		t.Fatalf("redirect target requests=%d authorization=%v body=%v", redirectedRequests.Load(), receivedAuthorization.Load(), receivedBody.Load())
	}
}

func TestOpenAICompleteMapsCanceledContext(t *testing.T) {
	provider := providerWithTransport(roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return nil, request.Context().Err()
	}))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := provider.Complete(ctx, Request{})
	assertProviderError(t, err, ErrorCodeRequestFailed, false)
}

func TestOpenAICompleteMapsResponseBodyDeadline(t *testing.T) {
	release := make(chan struct{})
	flushed := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(http.StatusOK)
		writer.(http.Flusher).Flush()
		close(flushed)
		<-release
	}))
	defer server.Close()
	defer close(release)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	result := make(chan error, 1)
	go func() {
		provider := NewOpenAICompatible(server.URL, "test-key", "test-model", time.Second)
		_, err := provider.Complete(ctx, Request{})
		result <- err
	}()
	select {
	case <-flushed:
	case <-time.After(time.Second):
		t.Fatal("response headers were not flushed")
	}
	assertProviderError(t, <-result, ErrorCodeTimeout, true)
}

func TestOpenAICompleteMapsNetworkError(t *testing.T) {
	provider := providerWithTransport(roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("network unavailable")
	}))
	_, err := provider.Complete(context.Background(), Request{})
	assertProviderError(t, err, ErrorCodeUnavailable, true)
}

func TestOpenAICompleteMapsResponseBodyReadError(t *testing.T) {
	body := &errorReadCloser{err: errors.New("response body unavailable")}
	provider := providerWithTransport(roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: body}, nil
	}))

	_, err := provider.Complete(context.Background(), Request{})
	assertProviderError(t, err, ErrorCodeUnavailable, true)
	if !body.closed {
		t.Fatal("response body was not closed")
	}
}

func TestOpenAICompleteLimitsAndClosesResponseBodies(t *testing.T) {
	t.Run("success response", func(t *testing.T) {
		const maxSuccessBytes = int64(16 << 20)
		body := &countingReadCloser{reader: io.LimitReader(repeatedByteReader{}, maxSuccessBytes+2)}
		provider := providerWithTransport(roundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusOK, Body: body}, nil
		}))

		_, err := provider.Complete(context.Background(), Request{})
		assertProviderError(t, err, ErrorCodeRequestFailed, false)
		if body.read != maxSuccessBytes+1 {
			t.Fatalf("success response bytes read = %d, want %d", body.read, maxSuccessBytes+1)
		}
		if !body.closed {
			t.Fatal("success response body was not closed")
		}
	})

	t.Run("error response", func(t *testing.T) {
		body := &countingReadCloser{reader: io.LimitReader(repeatedByteReader{}, maxErrorResponseBytes+1)}
		provider := providerWithTransport(roundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusInternalServerError, Body: body}, nil
		}))

		_, err := provider.Complete(context.Background(), Request{})
		assertProviderError(t, err, ErrorCodeUnavailable, true)
		if body.read != maxErrorResponseBytes {
			t.Fatalf("error response bytes read = %d, want %d", body.read, maxErrorResponseBytes)
		}
		if !body.closed {
			t.Fatal("error response body was not closed")
		}
	})
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
			assertProviderError(t, err, ErrorCodeRequestFailed, false)
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

func providerWithTransport(transport http.RoundTripper) Provider {
	return &openAICompatible{
		endpoint: "http://llm.test/v1/chat/completions",
		apiKey:   "test-key",
		model:    "test-model",
		client:   &http.Client{Transport: transport},
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

type countingReadCloser struct {
	reader io.Reader
	read   int64
	closed bool
}

func (r *countingReadCloser) Read(buffer []byte) (int, error) {
	count, err := r.reader.Read(buffer)
	r.read += int64(count)
	return count, err
}

func (r *countingReadCloser) Close() error {
	r.closed = true
	return nil
}

type errorReadCloser struct {
	err    error
	closed bool
}

func (r *errorReadCloser) Read([]byte) (int, error) {
	return 0, r.err
}

func (r *errorReadCloser) Close() error {
	r.closed = true
	return nil
}

type repeatedByteReader struct{}

func (repeatedByteReader) Read(buffer []byte) (int, error) {
	for index := range buffer {
		buffer[index] = 'x'
	}
	return len(buffer), nil
}
