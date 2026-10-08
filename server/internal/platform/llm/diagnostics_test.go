package llm

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestTimeoutIdentifiesResponsePhaseWithoutLeakingCredentials(t *testing.T) {
	for _, test := range []struct {
		name, phase string
		headers     bool
	}{
		{"waiting for headers", "waiting_response", false},
		{"reading body", "reading_response", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.Copy(io.Discard, r.Body)
				if test.headers {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(200)
					fmt.Fprint(w, `{"choices":[`)
					w.(http.Flusher).Flush()
				}
				select {
				case <-r.Context().Done():
				case <-time.After(time.Second):
				}
			}))
			defer server.Close()
			provider := newTestOpenAICompatible(server.URL, "private-api-key", "test-model", 100*time.Millisecond)
			_, err := provider.Complete(context.Background(), Request{Messages: []Message{{Role: "user", Content: "private article text"}}, MaxTokens: 50})
			var target *Error
			if !errors.As(err, &target) || target.Code != ErrorCodeTimeout {
				t.Fatalf("error = %v", err)
			}
			if !strings.Contains(target.Message, test.phase) {
				t.Fatalf("phase missing: %s", target.Message)
			}
			if strings.Contains(target.Message, "private-api-key") || strings.Contains(target.Message, "private article text") || strings.Contains(target.Message, server.URL) {
				t.Fatalf("diagnostics leaked request: %s", target.Message)
			}
		})
	}
}
