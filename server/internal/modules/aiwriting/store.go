package aiwriting

import (
	"context"

	"github.com/chenbb0128/weavepress/server/internal/modules/editorial"
	"github.com/chenbb0128/weavepress/server/internal/modules/workspace"
)

type ArticleStore interface {
	GetArticle(context.Context, uint64) (workspace.Article, error)
	GetAsset(context.Context, uint64) (workspace.Asset, error)
}

type CreateAnalysisJobInput struct {
	ArticleID        uint64
	RequestedBy      uint64
	Provider         string
	Model            string
	PromptVersion    string
	InputFingerprint [32]byte
	Force            bool
}

type CreateGenerationJobInput struct {
	AnalysisID       uint64
	RequestedBy      uint64
	Params           GenerationParams
	Provider         string
	Model            string
	PromptVersion    string
	InputFingerprint [32]byte
}

type JobFailureInput struct {
	Code      string
	Message   string
	Retryable bool
	Requeue   bool
	Usage     TokenUsage
}

type Store interface {
	CreateAnalysisJob(context.Context, CreateAnalysisJobInput) (Job, bool, error)
	CreateGenerationJob(context.Context, CreateGenerationJobInput) (Generation, Job, bool, error)
	GetJob(context.Context, uint64, bool) (Job, error)
	ListJobs(context.Context, JobFilter, int, int) (Page[Job], error)
	GetAnalysis(context.Context, uint64) (Analysis, error)
	ListAnalyses(context.Context, uint64, int, int) (Page[Analysis], error)
	GetGeneration(context.Context, uint64) (Generation, error)
	GetGenerationByJobID(context.Context, uint64) (Generation, error)
	SetJobRunning(context.Context, uint64) error
	CompleteAnalysis(context.Context, uint64, AnalysisOutput, TokenUsage) (Analysis, error)
	CompleteGeneration(context.Context, uint64, GenerationOutput, editorial.GeneratedDraftInput, TokenUsage) (Generation, error)
	SetJobFailure(context.Context, uint64, JobFailureInput) error
	AddJobEvent(context.Context, uint64, string, string) error
	RetryJob(context.Context, uint64, uint64) (Job, error)
}
