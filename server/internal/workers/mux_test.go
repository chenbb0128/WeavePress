package workers

import (
	"context"
	"testing"

	"github.com/hibiken/asynq"

	"github.com/chenbb0128/weavepress/server/internal/config"
	"github.com/chenbb0128/weavepress/server/internal/modules/aiwriting"
	"github.com/chenbb0128/weavepress/server/internal/modules/workspace"
)

type completedAIStore struct{ aiwriting.Store }

func (completedAIStore) SetJobRunning(context.Context, uint64) error { return nil }
func (completedAIStore) GetJob(context.Context, uint64, bool) (aiwriting.Job, error) {
	return aiwriting.Job{ID: 7, Type: aiwriting.JobTypeAnalysis, Status: aiwriting.JobCompleted}, nil
}

type unusedArticles struct{ aiwriting.ArticleStore }

func (unusedArticles) GetArticle(context.Context, uint64) (workspace.Article, error) {
	return workspace.Article{}, workspace.ErrNotFound
}

func TestNewMuxRegistersAIHandlersAndAllowsNilServices(t *testing.T) {
	service := aiwriting.New(completedAIStore{}, unusedArticles{}, nil, nil, config.AIConfig{})
	mux := NewMux(nil, nil, service)
	if err := mux.ProcessTask(context.Background(), asynq.NewTask(aiwriting.TaskAnalyze, []byte(`{"jobId":7}`))); err != nil {
		t.Fatalf("AI analyze handler error = %v", err)
	}
	_ = NewMux(nil, nil)
}
