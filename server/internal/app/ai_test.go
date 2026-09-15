package app

import (
	"testing"
	"time"

	"github.com/chenbb0128/weavepress/server/internal/modules/aisettings"
)

func TestNewAIProviderUsesResolvedRuntime(t *testing.T) {
	runtime := aisettings.RuntimeConfig{
		Enabled: true,
		BaseURL: "https://llm.example.com",
		APIKey:  "test-key",
		Model:   "test-model",
	}
	if provider := newAIProvider(runtime, time.Second); provider == nil {
		t.Fatal("resolved AI runtime did not create a provider")
	}
}
