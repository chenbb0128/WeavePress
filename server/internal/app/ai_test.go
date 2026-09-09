package app

import (
	"testing"
	"time"

	"github.com/chenbb0128/weavepress/server/internal/config"
)

func TestNewAIProviderOnlyWhenEnabled(t *testing.T) {
	disabled := config.AIConfig{Enabled: false}
	if provider := newAIProvider(disabled); provider != nil {
		t.Fatal("disabled AI created a provider")
	}

	enabled := config.AIConfig{
		Enabled:        true,
		BaseURL:        "https://llm.example.com",
		APIKey:         "test-key",
		Model:          "test-model",
		RequestTimeout: time.Second,
	}
	if provider := newAIProvider(enabled); provider == nil {
		t.Fatal("enabled AI did not create a provider")
	}
}
