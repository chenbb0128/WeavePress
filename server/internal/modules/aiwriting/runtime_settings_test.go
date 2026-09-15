package aiwriting

import (
	"context"
	"errors"
	"testing"

	"github.com/hibiken/asynq"

	"github.com/chenbb0128/weavepress/server/internal/modules/aisettings"
	"github.com/chenbb0128/weavepress/server/internal/platform/llm"
)

type fakeRuntimeSettings struct {
	status      aisettings.RuntimeStatus
	active      aisettings.RuntimeConfig
	forJob      aisettings.RuntimeConfig
	statusErr   error
	activeErr   error
	forJobErr   error
	jobProvider string
	jobModel    string
}

func (s *fakeRuntimeSettings) Status(context.Context) (aisettings.RuntimeStatus, error) {
	return s.status, s.statusErr
}

func (s *fakeRuntimeSettings) Active(context.Context) (aisettings.RuntimeConfig, error) {
	return s.active, s.activeErr
}

func (s *fakeRuntimeSettings) ForJob(_ context.Context, provider, model string) (aisettings.RuntimeConfig, error) {
	s.jobProvider, s.jobModel = provider, model
	return s.forJob, s.forJobErr
}

func TestStartAnalysisRecordsActiveProviderAndModel(t *testing.T) {
	store := &fakeAIStore{job: Job{ID: 9, ArticleID: 12, Status: JobQueued}}
	settings := &fakeRuntimeSettings{active: aisettings.RuntimeConfig{
		Enabled: true, Provider: aisettings.ProviderZhipu, Model: "glm-5.3-flash",
		BaseURL: "https://open.bigmodel.cn/api/paas/v4", APIKey: "secret",
	}}
	service := NewWithSettings(store, &fakeAIArticles{article: readyAIArticle()}, &fakeAIEnqueuer{}, settings, nil, DefaultLimits())
	if _, _, err := service.StartAnalysis(context.Background(), 12, 5, false); err != nil {
		t.Fatal(err)
	}
	if store.createdAnalysis.Provider != aisettings.ProviderZhipu || store.createdAnalysis.Model != "glm-5.3-flash" {
		t.Fatalf("input = %#v", store.createdAnalysis)
	}
}

func TestHandleAnalyzeTaskUsesJobProviderModelAndLatestKey(t *testing.T) {
	store := &fakeAIStore{job: Job{ID: 9, Type: JobTypeAnalysis, ArticleID: 12, Status: JobQueued, Provider: aisettings.ProviderOpenAI, Model: "recorded-model"}}
	settings := &fakeRuntimeSettings{forJob: aisettings.RuntimeConfig{
		Enabled: true, Provider: aisettings.ProviderOpenAI, Model: "recorded-model",
		BaseURL: "https://api.openai.com/v1", APIKey: "latest-key",
	}}
	provider := &fakeAIProvider{responses: []llm.Response{{Content: encodeJSONForTest(t, validAnalysisOutput())}}}
	var factoryInput aisettings.RuntimeConfig
	service := NewWithSettings(store, &fakeAIArticles{article: readyAIArticle()}, nil, settings, func(input aisettings.RuntimeConfig) llm.Provider {
		factoryInput = input
		return provider
	}, DefaultLimits())
	task := asynq.NewTask(TaskAnalyze, []byte(`{"jobId":9}`))
	if err := service.HandleAnalyzeTask(context.Background(), task); err != nil {
		t.Fatal(err)
	}
	if settings.jobProvider != aisettings.ProviderOpenAI || settings.jobModel != "recorded-model" {
		t.Fatalf("ForJob(%q, %q)", settings.jobProvider, settings.jobModel)
	}
	if factoryInput.APIKey != "latest-key" || factoryInput.Model != "recorded-model" {
		t.Fatalf("factory input = %#v", factoryInput)
	}
}

func TestHandleAnalyzeTaskFailsPermanentlyWhenRuntimeDisabled(t *testing.T) {
	store := &fakeAIStore{job: Job{ID: 9, Type: JobTypeAnalysis, ArticleID: 12, Status: JobQueued, Provider: aisettings.ProviderOpenAI, Model: "gpt-5"}}
	settings := &fakeRuntimeSettings{forJobErr: aisettings.ErrNotConfigured}
	service := NewWithSettings(store, &fakeAIArticles{article: readyAIArticle()}, nil, settings, nil, DefaultLimits())
	err := service.HandleAnalyzeTask(context.Background(), asynq.NewTask(TaskAnalyze, []byte(`{"jobId":9}`)))
	if err == nil || len(store.failures) != 1 {
		t.Fatalf("error = %v failures = %#v", err, store.failures)
	}
	if store.failures[0].Code != "AI_NOT_CONFIGURED" || store.failures[0].Retryable {
		t.Fatalf("failure = %#v", store.failures[0])
	}
}

func TestStatusReadsRuntimeSettings(t *testing.T) {
	settings := &fakeRuntimeSettings{status: aisettings.RuntimeStatus{Enabled: true, Provider: aisettings.ProviderOpenAI, Model: "gpt-5-mini"}}
	service := NewWithSettings(&fakeAIStore{}, &fakeAIArticles{}, nil, settings, nil, DefaultLimits())
	status, err := service.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !status.Enabled || status.Provider != aisettings.ProviderOpenAI || status.Model != "gpt-5-mini" {
		t.Fatalf("status = %#v", status)
	}
}

func TestStartAnalysisMapsDisabledRuntimeToNotConfigured(t *testing.T) {
	settings := &fakeRuntimeSettings{activeErr: aisettings.ErrNotConfigured}
	service := NewWithSettings(&fakeAIStore{}, &fakeAIArticles{}, &fakeAIEnqueuer{}, settings, nil, DefaultLimits())
	_, _, err := service.StartAnalysis(context.Background(), 12, 5, false)
	if !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("error = %v, want %v", err, ErrNotConfigured)
	}
}
