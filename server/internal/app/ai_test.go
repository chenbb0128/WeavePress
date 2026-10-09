package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/chenbb0128/weavepress/server/internal/modules/aisettings"
	"github.com/chenbb0128/weavepress/server/internal/modules/aiwriting"
	"github.com/chenbb0128/weavepress/server/internal/modules/workspace"
	"github.com/chenbb0128/weavepress/server/internal/platform/llm"
)

type apiQualityArticles struct{ aiwriting.ArticleStore }

func (apiQualityArticles) GetArticle(context.Context, uint64) (workspace.Article, error) {
	return workspace.Article{ID: 1, Status: "ready", Title: "原文标题", PlainText: "原文事实"}, nil
}

type apiQualitySettings struct{ aiwriting.SettingsResolver }

func (apiQualitySettings) Active(context.Context) (aisettings.RuntimeConfig, error) {
	// Invalid HTTP URL is rejected by netguard locally; this test never sends data.
	return aisettings.RuntimeConfig{Enabled: true, BaseURL: "http://localhost", Model: "test-model"}, nil
}

func TestAPIAIWritingServiceCreatesProviderForQualityAndRewrite(t *testing.T) {
	service := newAPIAIWritingService(nil, apiQualityArticles{}, nil, apiQualitySettings{})
	input := aiwriting.QualityInput{Title: "成稿标题", Blocks: []aiwriting.QualityBlock{{Type: "paragraph", Text: "成稿事实"}}, Semantic: true}
	for _, operation := range []string{"quality", "rewrite"} {
		t.Run(operation, func(t *testing.T) {
			var err error
			if operation == "quality" {
				_, err = service.Quality(context.Background(), 1, input)
			} else {
				_, err = service.Rewrite(context.Background(), 1, aiwriting.RewriteInput{QualityInput: input, TargetIndex: -1})
			}
			var providerErr *llm.Error
			if errors.Is(err, aiwriting.ErrNotConfigured) || !errors.As(err, &providerErr) {
				t.Fatalf("expected provider to enforce outbound URL policy, got %v", err)
			}
		})
	}
}

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
