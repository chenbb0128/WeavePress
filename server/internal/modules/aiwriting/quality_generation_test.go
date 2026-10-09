package aiwriting

import (
	"context"
	"github.com/hibiken/asynq"
	"strings"
	"testing"
	"time"

	"github.com/chenbb0128/weavepress/server/internal/platform/llm"
)

func TestGenerationQualityRepairsOnlyOnceBeforeCreatingDraft(t *testing.T) {
	for _, fixed := range []bool{true, false} {
		t.Run(map[bool]string{true: "fixed", false: "still invalid"}[fixed], func(t *testing.T) {
			article := readyAIArticle()
			analysis := validAnalysis()
			analysis.ID, analysis.ArticleID = 3, article.ID
			analysis.Job = &Job{Status: JobCompleted}
			store := &fakeAIStore{
				job:        Job{ID: 9, Type: JobTypeGeneration, ArticleID: article.ID, RequestedBy: 5, Status: JobQueued},
				generation: Generation{ID: 4, JobID: 9, AnalysisID: 3, AngleID: "SOURCE", Audience: "读者", Tone: "source", TargetWords: 1000},
				analysis:   analysis,
			}
			initial := GenerationOutput{Title: article.Title, Digest: "摘要", Blocks: []GeneratedBlock{{Type: "paragraph", Text: "转述正文"}}}
			title := "重新表达的标题"
			if !fixed {
				title = article.Title
			}
			provider := &fakeAIProvider{responses: []llm.Response{
				{Content: encodeJSONForTest(t, initial), Usage: llm.Usage{TotalTokens: 10}},
				{Content: `{"title":"` + title + `","blocks":[]}`, Usage: llm.Usage{TotalTokens: 5}},
			}}
			service := New(store, &fakeAIArticles{article: article}, nil, provider, testAIConfig())
			err := service.HandleGenerateTask(context.Background(), jobTask(TaskGenerate, 9))
			if len(provider.requests) != 2 {
				t.Fatalf("provider calls=%d, want one bounded quality repair", len(provider.requests))
			}
			if fixed {
				if err != nil || store.completeGenerationCalls != 1 {
					t.Fatalf("err=%v completes=%d", err, store.completeGenerationCalls)
				}
				if store.completedGeneration.Title != title || store.completedGeneration.Blocks[0].Text != "转述正文" || store.job.TotalTokens != 15 {
					t.Fatalf("output=%#v usage=%#v", store.completedGeneration, store.job.TokenUsage)
				}
			} else if err == nil || store.completeGenerationCalls != 0 || len(store.failures) != 1 || store.failures[0].Retryable {
				t.Fatalf("unfixed quality must not create a draft or auto-retry: err=%v completes=%d failures=%#v", err, store.completeGenerationCalls, store.failures)
			}
			if !strings.Contains(provider.requests[1].Messages[0].Content, "局部") {
				t.Fatal("quality repair must request a local patch")
			}
		})
	}
}

func TestGenerationQueueBudgetIncludesBoundedRepairs(t *testing.T) {
	article := readyAIArticle()
	analysis := validAnalysis()
	analysis.ID, analysis.ArticleID, analysis.Job = 3, article.ID, &Job{Status: JobCompleted}
	store := &fakeAIStore{analysis: analysis, generation: Generation{ID: 4}, job: Job{ID: 9, Type: JobTypeGeneration, Status: JobQueued}}
	queue := &fakeAIEnqueuer{}
	service := New(store, &fakeAIArticles{article: article}, queue, &fakeAIProvider{}, testAIConfig())
	if _, _, _, err := service.StartGeneration(context.Background(), 3, 5, validGenerationParams()); err != nil {
		t.Fatal(err)
	}
	for _, option := range queue.options {
		if option.Type() == asynq.TimeoutOpt {
			if option.Value() != 90*time.Second {
				t.Fatalf("task timeout=%v, need initial+format+quality requests", option.Value())
			}
			return
		}
	}
	t.Fatal("task deadline missing")
}

func TestGenerationQualityRepairsOverlapAfterFormatRepair(t *testing.T) {
	article := readyAIArticle()
	article.PlainText = strings.Repeat("原文", 40)
	article.Blocks = nil
	analysis := validAnalysis()
	analysis.ID, analysis.ArticleID, analysis.Job = 3, article.ID, &Job{Status: JobCompleted}
	store := &fakeAIStore{job: Job{ID: 9, Type: JobTypeGeneration, ArticleID: article.ID, RequestedBy: 5, Status: JobQueued}, generation: Generation{ID: 4, JobID: 9, AnalysisID: 3, AngleID: "SOURCE", Audience: "读者", Tone: "source", TargetWords: 1000}, analysis: analysis}
	provider := &fakeAIProvider{responses: []llm.Response{
		{Content: `{"title":`},
		{Content: encodeJSONForTest(t, GenerationOutput{Title: "新标题", Digest: "摘要", Blocks: []GeneratedBlock{{Type: "paragraph", Text: article.PlainText}}})},
		{Content: `{"blocks":[{"index":0,"type":"paragraph","text":"已重新组织的正文"}]}`},
	}}
	service := New(store, &fakeAIArticles{article: article}, nil, provider, testAIConfig())
	err := service.HandleGenerateTask(context.Background(), jobTask(TaskGenerate, 9))
	if err != nil || len(provider.requests) != 3 || store.completeGenerationCalls != 1 || store.completedGeneration.Blocks[0].Text != "已重新组织的正文" {
		t.Fatalf("err=%v calls=%d completes=%d output=%#v", err, len(provider.requests), store.completeGenerationCalls, store.completedGeneration)
	}
}
