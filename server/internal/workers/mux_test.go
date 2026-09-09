package workers

import (
	"context"
	"testing"

	"github.com/hibiken/asynq"

	"github.com/chenbb0128/weavepress/server/internal/config"
	"github.com/chenbb0128/weavepress/server/internal/modules/aiwriting"
	"github.com/chenbb0128/weavepress/server/internal/modules/content"
	"github.com/chenbb0128/weavepress/server/internal/modules/editorial"
	"github.com/chenbb0128/weavepress/server/internal/modules/workspace"
)

var _ func(*content.Service, *editorial.Service, *aiwriting.Service) *asynq.ServeMux = NewMux

type completedAIStore struct {
	aiwriting.Store
	jobType         string
	setRunningCalls int
}

func (s *completedAIStore) SetJobRunning(context.Context, uint64) error {
	s.setRunningCalls++
	return nil
}
func (s *completedAIStore) GetJob(context.Context, uint64, bool) (aiwriting.Job, error) {
	return aiwriting.Job{ID: 7, Type: s.jobType, Status: aiwriting.JobCompleted}, nil
}

type unusedArticles struct{ aiwriting.ArticleStore }

func (unusedArticles) GetArticle(context.Context, uint64) (workspace.Article, error) {
	return workspace.Article{}, workspace.ErrNotFound
}

func TestNewMuxRegistersAnalyzeHandler(t *testing.T) {
	store := &completedAIStore{jobType: aiwriting.JobTypeAnalysis}
	service := aiwriting.New(store, unusedArticles{}, nil, nil, config.AIConfig{})
	mux := NewMux(nil, nil, service)
	if err := mux.ProcessTask(context.Background(), asynq.NewTask(aiwriting.TaskAnalyze, []byte(`{"jobId":7}`))); err != nil {
		t.Fatalf("AI analyze handler error = %v", err)
	}
	if store.setRunningCalls != 1 {
		t.Fatalf("SetJobRunning calls = %d, want 1", store.setRunningCalls)
	}
}

func TestNewMuxRegistersGenerateHandler(t *testing.T) {
	store := &completedAIStore{jobType: aiwriting.JobTypeGeneration}
	service := aiwriting.New(store, unusedArticles{}, nil, nil, config.AIConfig{})
	mux := NewMux(nil, nil, service)
	if err := mux.ProcessTask(context.Background(), asynq.NewTask(aiwriting.TaskGenerate, []byte(`{"jobId":7}`))); err != nil {
		t.Fatalf("AI generate handler error = %v", err)
	}
	if store.setRunningCalls != 1 {
		t.Fatalf("SetJobRunning calls = %d, want 1", store.setRunningCalls)
	}
}
