package aiwriting

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/hibiken/asynq"

	"github.com/chenbb0128/weavepress/server/internal/config"
	"github.com/chenbb0128/weavepress/server/internal/modules/editorial"
	"github.com/chenbb0128/weavepress/server/internal/modules/workspace"
	"github.com/chenbb0128/weavepress/server/internal/platform/llm"
	queueplatform "github.com/chenbb0128/weavepress/server/internal/platform/queue"
)

type fakeAIStore struct {
	Store
	createAnalysisCalls     int
	createdAnalysis         CreateAnalysisJobInput
	job                     Job
	reuseAnalysis           bool
	setRunningCalls         int
	analysis                Analysis
	analysesPage            Page[Analysis]
	generation              Generation
	jobsPage                Page[Job]
	createGenerationCalls   int
	createdGeneration       CreateGenerationJobInput
	reuseGeneration         bool
	failures                []JobFailureInput
	failureErr              error
	retryJob                Job
	retryErr                error
	setRunningErr           error
	completeAnalysisCalls   int
	completedAnalysis       AnalysisOutput
	completeGenerationCalls int
	completedGeneration     GenerationOutput
	completedDraft          editorial.GeneratedDraftInput
	events                  []JobEvent
	eventErr                error
}

func (s *fakeAIStore) CreateAnalysisJob(_ context.Context, input CreateAnalysisJobInput) (Job, bool, error) {
	s.createAnalysisCalls++
	s.createdAnalysis = input
	return s.job, s.reuseAnalysis, nil
}

func (s *fakeAIStore) SetJobRunning(context.Context, uint64) error {
	s.setRunningCalls++
	if s.setRunningErr != nil {
		return s.setRunningErr
	}
	if s.job.Status != JobCompleted {
		s.job.Status = JobRunning
		s.job.Attempts++
	}
	return nil
}

func (s *fakeAIStore) CreateGenerationJob(_ context.Context, input CreateGenerationJobInput) (Generation, Job, bool, error) {
	s.createGenerationCalls++
	s.createdGeneration = input
	return s.generation, s.job, s.reuseGeneration, nil
}

func (s *fakeAIStore) GetJob(context.Context, uint64, bool) (Job, error) { return s.job, nil }
func (s *fakeAIStore) ListJobs(context.Context, JobFilter, int, int) (Page[Job], error) {
	return s.jobsPage, nil
}
func (s *fakeAIStore) GetAnalysis(context.Context, uint64) (Analysis, error) { return s.analysis, nil }
func (s *fakeAIStore) ListAnalyses(context.Context, uint64, int, int) (Page[Analysis], error) {
	return s.analysesPage, nil
}
func (s *fakeAIStore) GetGeneration(context.Context, uint64) (Generation, error) {
	return s.generation, nil
}
func (s *fakeAIStore) GetGenerationByJobID(context.Context, uint64) (Generation, error) {
	return s.generation, nil
}
func (s *fakeAIStore) CompleteAnalysis(_ context.Context, _ uint64, output AnalysisOutput, usage TokenUsage) (Analysis, error) {
	s.completeAnalysisCalls++
	s.completedAnalysis = output
	s.job.Status = JobCompleted
	s.job.TokenUsage = sumUsageForTest(s.job.TokenUsage, usage)
	return Analysis{ID: 3, JobID: s.job.ID, ArticleID: s.job.ArticleID, AnalysisOutput: output}, nil
}
func (s *fakeAIStore) CompleteGeneration(_ context.Context, _ uint64, output GenerationOutput, draft editorial.GeneratedDraftInput, usage TokenUsage) (Generation, error) {
	s.completeGenerationCalls++
	s.completedGeneration = output
	s.completedDraft = draft
	s.job.Status = JobCompleted
	s.job.TokenUsage = sumUsageForTest(s.job.TokenUsage, usage)
	return s.generation, nil
}
func (s *fakeAIStore) SetJobFailure(_ context.Context, _ uint64, input JobFailureInput) error {
	s.failures = append(s.failures, input)
	s.job.TokenUsage = sumUsageForTest(s.job.TokenUsage, input.Usage)
	if input.Requeue {
		s.job.Status = JobQueued
	} else {
		s.job.Status = JobFailed
	}
	s.job.Retryable = input.Retryable
	s.job.ErrorCode = input.Code
	s.job.ErrorMessage = input.Message
	return s.failureErr
}
func (s *fakeAIStore) AddJobEvent(_ context.Context, jobID uint64, status, message string) error {
	if s.eventErr != nil {
		return s.eventErr
	}
	s.events = append(s.events, JobEvent{JobID: jobID, Status: status, Message: message})
	return nil
}
func (s *fakeAIStore) RetryJob(context.Context, uint64, uint64) (Job, error) {
	return s.retryJob, s.retryErr
}

type fakeAIArticles struct {
	article  workspace.Article
	err      error
	assets   map[uint64]workspace.Asset
	assetIDs []uint64
	assetErr error
}

func (s *fakeAIArticles) GetArticle(context.Context, uint64) (workspace.Article, error) {
	return s.article, s.err
}

func (s *fakeAIArticles) GetAsset(_ context.Context, id uint64) (workspace.Asset, error) {
	s.assetIDs = append(s.assetIDs, id)
	if s.assetErr != nil {
		return workspace.Asset{}, s.assetErr
	}
	asset, ok := s.assets[id]
	if !ok {
		return workspace.Asset{}, workspace.ErrNotFound
	}
	return asset, nil
}

type fakeAIProvider struct {
	responses []llm.Response
	errors    []error
	requests  []llm.Request
}

func (p *fakeAIProvider) Complete(_ context.Context, request llm.Request) (llm.Response, error) {
	index := len(p.requests)
	p.requests = append(p.requests, request)
	if index >= len(p.responses) && index >= len(p.errors) {
		return llm.Response{}, errors.New("unexpected provider call")
	}
	var response llm.Response
	if index < len(p.responses) {
		response = p.responses[index]
	}
	if index < len(p.errors) {
		return response, p.errors[index]
	}
	return response, nil
}

type fakeAIEnqueuer struct {
	calls   int
	task    *asynq.Task
	options []asynq.Option
	err     error
}

func (q *fakeAIEnqueuer) EnqueueContext(_ context.Context, task *asynq.Task, options ...asynq.Option) (*asynq.TaskInfo, error) {
	q.calls++
	q.task = task
	q.options = append([]asynq.Option(nil), options...)
	return nil, q.err
}

type reusedJobEnqueueCase struct {
	name        string
	status      string
	queueErr    error
	wantCalls   int
	wantErr     error
	wantFailure bool
}

func reusedJobEnqueueCases() []reusedJobEnqueueCase {
	queueErr := errors.New("redis unavailable")
	return []reusedJobEnqueueCase{
		{name: "queued", status: JobQueued, wantCalls: 1},
		{name: "queued task ID conflict", status: JobQueued, queueErr: asynq.ErrTaskIDConflict, wantCalls: 1},
		{name: "queued enqueue failure", status: JobQueued, queueErr: queueErr, wantCalls: 1, wantErr: queueErr, wantFailure: true},
		{name: "running", status: JobRunning},
		{name: "completed", status: JobCompleted},
		{name: "failed", status: JobFailed},
	}
}

func assertReusedJobEnqueue(t *testing.T, test reusedJobEnqueueCase, store *fakeAIStore, queue *fakeAIEnqueuer, err error) {
	t.Helper()
	if test.wantErr != nil {
		if !errors.Is(err, test.wantErr) {
			t.Fatalf("start error = %v, want %v", err, test.wantErr)
		}
	} else if err != nil {
		t.Fatal(err)
	}
	if queue.calls != test.wantCalls {
		t.Fatalf("enqueue calls = %d, want %d", queue.calls, test.wantCalls)
	}
	if test.wantFailure {
		if len(store.failures) != 1 || store.job.Status != JobFailed || !store.failures[0].Retryable || store.failures[0].Requeue {
			t.Fatalf("job=%#v failures=%#v", store.job, store.failures)
		}
	} else if len(store.failures) != 0 {
		t.Fatalf("failures = %#v", store.failures)
	}
}

func testAIConfig() config.AIConfig {
	return config.AIConfig{
		Enabled:         true,
		Provider:        "openai-compatible",
		Model:           "test-model",
		RequestTimeout:  20 * time.Second,
		MaxInputChars:   60_000,
		MaxOutputTokens: 6_000,
		Temperature:     .4,
	}
}

func readyAIArticle() workspace.Article {
	return workspace.Article{
		ID:        12,
		Status:    "ready",
		Title:     "来源标题",
		PlainText: "原文事实和逐字引用\n作者认为需要谨慎",
		Blocks: []workspace.Block{
			{Type: "paragraph", Text: "原文事实和逐字引用"},
			{Type: "paragraph", Text: "作者认为需要谨慎"},
		},
	}
}

func TestStartAnalysisDisabledDoesNotCreateJob(t *testing.T) {
	store := &fakeAIStore{}
	cfg := testAIConfig()
	cfg.Enabled = false
	service := New(store, &fakeAIArticles{article: readyAIArticle()}, &fakeAIEnqueuer{}, &fakeAIProvider{}, cfg)

	_, _, err := service.StartAnalysis(context.Background(), 12, 5, false)
	if !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("StartAnalysis() error = %v, want %v", err, ErrNotConfigured)
	}
	if store.createAnalysisCalls != 0 {
		t.Fatalf("CreateAnalysisJob calls = %d, want 0", store.createAnalysisCalls)
	}
}

func TestQueueClientImplementsAIEnqueuer(t *testing.T) {
	var _ Enqueuer = (*queueplatform.Client)(nil)
}

func TestStartAnalysisEnqueuesWithDeterministicOptions(t *testing.T) {
	store := &fakeAIStore{job: Job{ID: 7, Type: JobTypeAnalysis, ArticleID: 12, Attempts: 2, ManualRetries: 1}}
	queue := &fakeAIEnqueuer{}
	cfg := testAIConfig()
	service := New(store, &fakeAIArticles{article: readyAIArticle()}, queue, &fakeAIProvider{}, cfg)

	job, reused, err := service.StartAnalysis(context.Background(), 12, 5, false)
	if err != nil {
		t.Fatal(err)
	}
	if reused || job.ID != 7 || queue.calls != 1 {
		t.Fatalf("job=%#v reused=%t enqueue calls=%d", job, reused, queue.calls)
	}
	if queue.task.Type() != TaskAnalyze || string(queue.task.Payload()) != `{"jobId":7}` {
		t.Fatalf("task type=%q payload=%s", queue.task.Type(), queue.task.Payload())
	}
	wantOptions := map[asynq.OptionType]any{
		asynq.TaskIDOpt:   "ai:analysis:7:2:1",
		asynq.QueueOpt:    "ai",
		asynq.MaxRetryOpt: 3,
		asynq.TimeoutOpt:  cfg.RequestTimeout + 30*time.Second,
	}
	for _, option := range queue.options {
		if want, ok := wantOptions[option.Type()]; ok {
			if got := option.Value(); got != want {
				t.Fatalf("option %v = %v, want %v", option.Type(), got, want)
			}
			delete(wantOptions, option.Type())
		}
	}
	if len(wantOptions) != 0 {
		t.Fatalf("missing enqueue options: %v", wantOptions)
	}
	if store.createdAnalysis.Provider != cfg.Provider || store.createdAnalysis.Model != cfg.Model || store.createdAnalysis.PromptVersion != AnalysisPromptV1 {
		t.Fatalf("CreateAnalysisJob input = %#v", store.createdAnalysis)
	}
}

func TestStartAnalysisReusedJobReenqueuesOnlyWhenQueued(t *testing.T) {
	for _, test := range reusedJobEnqueueCases() {
		t.Run(test.name, func(t *testing.T) {
			store := &fakeAIStore{job: Job{ID: 7, Type: JobTypeAnalysis, Status: test.status}, reuseAnalysis: true}
			queue := &fakeAIEnqueuer{err: test.queueErr}
			service := New(store, &fakeAIArticles{article: readyAIArticle()}, queue, &fakeAIProvider{}, testAIConfig())

			job, reused, err := service.StartAnalysis(context.Background(), 12, 5, false)
			assertReusedJobEnqueue(t, test, store, queue, err)
			if test.wantErr == nil && (!reused || job.ID != 7) {
				t.Fatalf("job=%#v reused=%t error=%v", job, reused, err)
			}
		})
	}
}

func TestHandleAnalyzeTaskRejectsInvalidPayloadWithoutTouchingStore(t *testing.T) {
	for _, raw := range []string{"", `{}`, `{"jobId":0}`, `{"jobId":7,"extra":true}`, `{"jobId":7}{"jobId":8}`, `{"jobId":7,"jobId":8}`} {
		store := &fakeAIStore{}
		service := New(store, &fakeAIArticles{}, nil, &fakeAIProvider{}, testAIConfig())
		task := asynq.NewTask(TaskAnalyze, []byte(raw))

		err := service.HandleAnalyzeTask(context.Background(), task)
		if !errors.Is(err, asynq.SkipRetry) {
			t.Fatalf("payload %q: HandleAnalyzeTask() error = %v, want SkipRetry", raw, err)
		}
		if store.setRunningCalls != 0 {
			t.Fatalf("payload %q: SetJobRunning calls = %d, want 0", raw, store.setRunningCalls)
		}
	}
}

func TestStartAnalysisRejectsArticleThatIsNotReady(t *testing.T) {
	store := &fakeAIStore{}
	article := readyAIArticle()
	article.Status = "collecting"
	service := New(store, &fakeAIArticles{article: article}, &fakeAIEnqueuer{}, &fakeAIProvider{}, testAIConfig())

	_, _, err := service.StartAnalysis(context.Background(), article.ID, 5, false)
	if !errors.Is(err, ErrArticleNotReady) {
		t.Fatalf("StartAnalysis() error = %v, want %v", err, ErrArticleNotReady)
	}
	if store.createAnalysisCalls != 0 {
		t.Fatalf("CreateAnalysisJob calls = %d, want 0", store.createAnalysisCalls)
	}
}

func TestStartAnalysisChecksInputLimitInRunes(t *testing.T) {
	store := &fakeAIStore{}
	article := readyAIArticle()
	article.PlainText = strings.Repeat("界", 5)
	article.Blocks = []workspace.Block{{Type: "paragraph", Text: article.PlainText}}
	cfg := testAIConfig()
	cfg.MaxInputChars = 4
	service := New(store, &fakeAIArticles{article: article}, &fakeAIEnqueuer{}, &fakeAIProvider{}, cfg)

	_, _, err := service.StartAnalysis(context.Background(), article.ID, 5, false)
	if !errors.Is(err, ErrInputTooLarge) {
		t.Fatalf("StartAnalysis() error = %v, want %v", err, ErrInputTooLarge)
	}
	if store.createAnalysisCalls != 0 {
		t.Fatalf("CreateAnalysisJob calls = %d, want 0", store.createAnalysisCalls)
	}
}

func TestStartAnalysisForceAlwaysCreatesAndEnqueues(t *testing.T) {
	store := &fakeAIStore{job: Job{ID: 8, Type: JobTypeAnalysis, ArticleID: 12}}
	queue := &fakeAIEnqueuer{}
	service := New(store, &fakeAIArticles{article: readyAIArticle()}, queue, &fakeAIProvider{}, testAIConfig())

	_, reused, err := service.StartAnalysis(context.Background(), 12, 5, true)
	if err != nil {
		t.Fatal(err)
	}
	if reused || !store.createdAnalysis.Force || queue.calls != 1 {
		t.Fatalf("reused=%t force=%t enqueue calls=%d", reused, store.createdAnalysis.Force, queue.calls)
	}
}

func TestStartGenerationValidatesRequestBeforeCreatingJob(t *testing.T) {
	store := &fakeAIStore{analysis: Analysis{ID: 3, ArticleID: 12, AnalysisOutput: validAnalysisOutput(), Job: &Job{Status: JobCompleted}}}
	service := New(store, &fakeAIArticles{article: readyAIArticle()}, &fakeAIEnqueuer{}, &fakeAIProvider{}, testAIConfig())
	params := validGenerationParams()
	params.AngleID = "A999"

	_, _, _, err := service.StartGeneration(context.Background(), 3, 5, params)
	if !errors.Is(err, ErrInvalidParameters) {
		t.Fatalf("StartGeneration() error = %v, want %v", err, ErrInvalidParameters)
	}
	if store.createGenerationCalls != 0 {
		t.Fatalf("CreateGenerationJob calls = %d, want 0", store.createGenerationCalls)
	}
}

func TestStartGenerationRequiresCompletedAnalysis(t *testing.T) {
	store := &fakeAIStore{analysis: Analysis{ID: 3, ArticleID: 12, AnalysisOutput: validAnalysisOutput(), Job: &Job{Status: JobRunning}}}
	service := New(store, &fakeAIArticles{article: readyAIArticle()}, &fakeAIEnqueuer{}, &fakeAIProvider{}, testAIConfig())

	_, _, _, err := service.StartGeneration(context.Background(), 3, 5, validGenerationParams())
	if !errors.Is(err, ErrAnalysisNotReady) {
		t.Fatalf("StartGeneration() error = %v, want %v", err, ErrAnalysisNotReady)
	}
}

func TestStartGenerationRejectsArticleThatIsNotReadyBeforeCreatingJob(t *testing.T) {
	article := readyAIArticle()
	article.Status = "collecting"
	analysis := Analysis{ID: 3, ArticleID: article.ID, AnalysisOutput: validAnalysisOutput(), Job: &Job{Status: JobCompleted}}
	store := &fakeAIStore{
		analysis:   analysis,
		generation: Generation{ID: 4, JobID: 9},
		job:        Job{ID: 9, Type: JobTypeGeneration, ArticleID: article.ID},
	}
	queue := &fakeAIEnqueuer{}
	service := New(store, &fakeAIArticles{article: article}, queue, &fakeAIProvider{}, testAIConfig())

	_, _, _, err := service.StartGeneration(context.Background(), analysis.ID, 5, validGenerationParams())
	if !errors.Is(err, ErrArticleNotReady) {
		t.Fatalf("StartGeneration() error = %v, want %v", err, ErrArticleNotReady)
	}
	if store.createGenerationCalls != 0 || queue.calls != 0 {
		t.Fatalf("CreateGenerationJob calls=%d enqueue calls=%d, want 0/0", store.createGenerationCalls, queue.calls)
	}
}

func TestStartGenerationReusedJobReenqueuesOnlyWhenQueued(t *testing.T) {
	for _, test := range reusedJobEnqueueCases() {
		t.Run(test.name, func(t *testing.T) {
			analysis := Analysis{ID: 3, ArticleID: 12, AnalysisOutput: validAnalysisOutput(), Job: &Job{Status: JobCompleted}}
			store := &fakeAIStore{
				analysis:        analysis,
				generation:      Generation{ID: 4, JobID: 9},
				job:             Job{ID: 9, Type: JobTypeGeneration, ArticleID: 12, Status: test.status},
				reuseGeneration: true,
			}
			queue := &fakeAIEnqueuer{err: test.queueErr}
			service := New(store, &fakeAIArticles{article: readyAIArticle()}, queue, &fakeAIProvider{}, testAIConfig())

			generation, job, reused, err := service.StartGeneration(context.Background(), analysis.ID, 5, validGenerationParams())
			assertReusedJobEnqueue(t, test, store, queue, err)
			if test.wantErr == nil && (!reused || generation.ID != 4 || job.ID != 9) {
				t.Fatalf("generation=%#v job=%#v reused=%t error=%v", generation, job, reused, err)
			}
		})
	}
}

func TestStartGenerationCreatesAndEnqueues(t *testing.T) {
	analysis := Analysis{ID: 3, ArticleID: 12, AnalysisOutput: validAnalysisOutput(), Job: &Job{Status: JobCompleted}}
	store := &fakeAIStore{
		analysis:   analysis,
		generation: Generation{ID: 4, JobID: 9},
		job:        Job{ID: 9, Type: JobTypeGeneration, ArticleID: 12},
	}
	queue := &fakeAIEnqueuer{}
	cfg := testAIConfig()
	service := New(store, &fakeAIArticles{article: readyAIArticle()}, queue, &fakeAIProvider{}, cfg)

	_, _, reused, err := service.StartGeneration(context.Background(), analysis.ID, 5, validGenerationParams())
	if err != nil {
		t.Fatal(err)
	}
	if reused || queue.calls != 1 || queue.task.Type() != TaskGenerate {
		t.Fatalf("reused=%t enqueue calls=%d task=%v", reused, queue.calls, queue.task)
	}
	if got := enqueueOption(queue.options, asynq.TaskIDOpt); got != "ai:generation:9:0:0" {
		t.Fatalf("TaskID = %v", got)
	}
	if store.createdGeneration.Provider != cfg.Provider || store.createdGeneration.Model != cfg.Model || store.createdGeneration.PromptVersion != GenerationPromptV1 {
		t.Fatalf("CreateGenerationJob input = %#v", store.createdGeneration)
	}
}

func TestStartAnalysisRecordsQueueFailureForManualRetry(t *testing.T) {
	queueErr := errors.New("redis unavailable")
	store := &fakeAIStore{job: Job{ID: 7, Type: JobTypeAnalysis}}
	service := New(store, &fakeAIArticles{article: readyAIArticle()}, &fakeAIEnqueuer{err: queueErr}, &fakeAIProvider{}, testAIConfig())

	_, _, err := service.StartAnalysis(context.Background(), 12, 5, false)
	if !errors.Is(err, queueErr) {
		t.Fatalf("StartAnalysis() error = %v, want queue error", err)
	}
	if len(store.failures) != 1 {
		t.Fatalf("SetJobFailure calls = %d, want 1", len(store.failures))
	}
	failure := store.failures[0]
	if failure.Code != "AI_QUEUE_UNAVAILABLE" || !failure.Retryable || failure.Requeue {
		t.Fatalf("failure = %#v", failure)
	}
}

func TestStartAnalysisReturnsQueueAndFailureWriteErrors(t *testing.T) {
	queueErr := errors.New("redis unavailable")
	writeErr := errors.New("database unavailable")
	store := &fakeAIStore{job: Job{ID: 7, Type: JobTypeAnalysis}, failureErr: writeErr}
	service := New(store, &fakeAIArticles{article: readyAIArticle()}, &fakeAIEnqueuer{err: queueErr}, &fakeAIProvider{}, testAIConfig())

	_, _, err := service.StartAnalysis(context.Background(), 12, 5, false)
	if !errors.Is(err, queueErr) || !errors.Is(err, writeErr) {
		t.Fatalf("StartAnalysis() error = %v, want queue and store errors", err)
	}
}

func TestStartAnalysisTreatsDeterministicTaskIDConflictAsSuccess(t *testing.T) {
	store := &fakeAIStore{job: Job{ID: 7, Type: JobTypeAnalysis}}
	service := New(store, &fakeAIArticles{article: readyAIArticle()}, &fakeAIEnqueuer{err: asynq.ErrTaskIDConflict}, &fakeAIProvider{}, testAIConfig())

	job, reused, err := service.StartAnalysis(context.Background(), 12, 5, false)
	if err != nil || reused || job.ID != 7 || len(store.failures) != 0 {
		t.Fatalf("job=%#v reused=%t error=%v failures=%v", job, reused, err, store.failures)
	}
}

func TestRetryOnlyRequeuesRetryableFailedJob(t *testing.T) {
	store := &fakeAIStore{retryJob: Job{ID: 7, Type: JobTypeAnalysis, Status: JobQueued, Attempts: 4, ManualRetries: 1}}
	queue := &fakeAIEnqueuer{}
	service := New(store, &fakeAIArticles{}, queue, &fakeAIProvider{}, testAIConfig())

	job, err := service.Retry(context.Background(), 7, 5)
	if err != nil || job.ID != 7 || queue.calls != 1 {
		t.Fatalf("job=%#v error=%v enqueue calls=%d", job, err, queue.calls)
	}
	if got := enqueueOption(queue.options, asynq.TaskIDOpt); got != "ai:analysis:7:4:1" {
		t.Fatalf("TaskID = %v", got)
	}

	notRetryable := &fakeAIStore{retryErr: ErrJobNotRetryable}
	queue = &fakeAIEnqueuer{}
	service = New(notRetryable, &fakeAIArticles{}, queue, &fakeAIProvider{}, testAIConfig())
	if _, err = service.Retry(context.Background(), 7, 5); !errors.Is(err, ErrJobNotRetryable) {
		t.Fatalf("Retry() error = %v, want %v", err, ErrJobNotRetryable)
	}
	if queue.calls != 0 {
		t.Fatalf("enqueue calls = %d, want 0", queue.calls)
	}
}

func TestServiceReadMethodsDelegateToStore(t *testing.T) {
	store := &fakeAIStore{
		analysis:     Analysis{ID: 3},
		analysesPage: Page[Analysis]{Total: 1},
		generation:   Generation{ID: 4},
		job:          Job{ID: 7},
		jobsPage:     Page[Job]{Total: 2},
	}
	service := New(store, &fakeAIArticles{}, nil, nil, testAIConfig())
	if status := service.Status(); !status.Enabled || status.Provider != "openai-compatible" || status.Model != "test-model" {
		t.Fatalf("Status() = %#v", status)
	}
	if got, _ := service.Analysis(context.Background(), 3); got.ID != 3 {
		t.Fatalf("Analysis() = %#v", got)
	}
	if got, _ := service.Analyses(context.Background(), 12, 1, 20); got.Total != 1 {
		t.Fatalf("Analyses() = %#v", got)
	}
	if got, _ := service.Generation(context.Background(), 4); got.ID != 4 {
		t.Fatalf("Generation() = %#v", got)
	}
	if got, _ := service.Job(context.Background(), 7); got.ID != 7 {
		t.Fatalf("Job() = %#v", got)
	}
	if got, _ := service.Jobs(context.Background(), JobFilter{}, 1, 20); got.Total != 2 {
		t.Fatalf("Jobs() = %#v", got)
	}
}

func TestHandleAnalyzeTaskRepairsInvalidJSONOnceAndAccumulatesUsage(t *testing.T) {
	validJSON := encodeJSONForTest(t, validAnalysisOutput())
	store := &fakeAIStore{job: Job{ID: 7, Type: JobTypeAnalysis, ArticleID: 12, Status: JobQueued}}
	provider := &fakeAIProvider{responses: []llm.Response{
		{Content: "not-json", Usage: llm.Usage{InputTokens: 7, OutputTokens: 3, TotalTokens: 10}},
		{Content: validJSON, Usage: llm.Usage{InputTokens: 2, OutputTokens: 2, TotalTokens: 4}},
	}}
	service := New(store, &fakeAIArticles{article: readyAIArticle()}, nil, provider, testAIConfig())

	if err := service.HandleAnalyzeTask(context.Background(), jobTask(TaskAnalyze, 7)); err != nil {
		t.Fatal(err)
	}
	if len(provider.requests) != 2 || store.completeAnalysisCalls != 1 {
		t.Fatalf("provider calls=%d complete calls=%d", len(provider.requests), store.completeAnalysisCalls)
	}
	if len(store.events) != 1 || store.events[0].Status != "format_repair" {
		t.Fatalf("events = %#v", store.events)
	}
	if provider.requests[1].Temperature != 0 || !provider.requests[1].JSON {
		t.Fatalf("repair request = %#v", provider.requests[1])
	}
	if store.job.TokenUsage != (TokenUsage{InputTokens: 9, OutputTokens: 5, TotalTokens: 14}) {
		t.Fatalf("usage = %#v", store.job.TokenUsage)
	}
}

func TestHandleAnalyzeTaskRepairsInvalidJSONFromRealProvider(t *testing.T) {
	validJSON := encodeJSONForTest(t, validAnalysisOutput())
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		calls++
		content := "not-json"
		usage := llm.Usage{InputTokens: 7, OutputTokens: 3, TotalTokens: 10}
		if calls == 2 {
			content = validJSON
			usage = llm.Usage{InputTokens: 2, OutputTokens: 2, TotalTokens: 4}
		}
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(map[string]any{
			"choices": []any{map[string]any{"message": map[string]any{"content": content}}},
			"usage": map[string]any{
				"prompt_tokens": usage.InputTokens, "completion_tokens": usage.OutputTokens, "total_tokens": usage.TotalTokens,
			},
		})
	}))
	defer server.Close()

	store := &fakeAIStore{job: Job{ID: 7, Type: JobTypeAnalysis, ArticleID: 12, Status: JobQueued}}
	provider := llm.NewOpenAICompatible(server.URL, "test-key", "test-model", time.Second)
	service := New(store, &fakeAIArticles{article: readyAIArticle()}, nil, provider, testAIConfig())

	if err := service.HandleAnalyzeTask(context.Background(), jobTask(TaskAnalyze, 7)); err != nil {
		t.Fatal(err)
	}
	if calls != 2 || len(store.events) != 1 || store.completeAnalysisCalls != 1 || store.job.TotalTokens != 14 {
		t.Fatalf("provider calls=%d events=%#v complete=%d usage=%#v", calls, store.events, store.completeAnalysisCalls, store.job.TokenUsage)
	}
}

func TestHandleAnalyzeTaskFailsPermanentlyWhenRepairIsStillInvalid(t *testing.T) {
	store := &fakeAIStore{job: Job{ID: 7, Type: JobTypeAnalysis, ArticleID: 12, Status: JobQueued}}
	provider := &fakeAIProvider{responses: []llm.Response{
		{Content: "not-json", Usage: llm.Usage{TotalTokens: 10}},
		{Content: `{"summary":"still wrong"}`, Usage: llm.Usage{TotalTokens: 4}},
	}}
	service := New(store, &fakeAIArticles{article: readyAIArticle()}, nil, provider, testAIConfig())

	err := service.HandleAnalyzeTask(context.Background(), jobTask(TaskAnalyze, 7))
	if !errors.Is(err, asynq.SkipRetry) {
		t.Fatalf("HandleAnalyzeTask() error = %v, want SkipRetry", err)
	}
	if len(provider.requests) != 2 || store.completeAnalysisCalls != 0 || len(store.failures) != 1 {
		t.Fatalf("provider=%d complete=%d failures=%#v", len(provider.requests), store.completeAnalysisCalls, store.failures)
	}
	failure := store.failures[0]
	if failure.Code != "AI_OUTPUT_INVALID" || failure.Retryable || failure.Requeue || failure.Usage.TotalTokens != 14 {
		t.Fatalf("failure = %#v", failure)
	}
}

func TestHandleAnalyzeTaskPreservesProviderErrorsFromRepair(t *testing.T) {
	for _, test := range []struct {
		name      string
		err       *llm.Error
		retryable bool
	}{
		{
			name:      "temporary",
			err:       &llm.Error{Code: llm.ErrorCodeRateLimited, Message: "AI 模型请求受到限流", Retryable: true, Cause: ErrOutputInvalid},
			retryable: true,
		},
		{
			name: "permanent",
			err:  &llm.Error{Code: llm.ErrorCodeAuthFailed, Message: "AI 模型认证失败", Retryable: false},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := &fakeAIStore{job: Job{ID: 7, Type: JobTypeAnalysis, ArticleID: 12, Status: JobQueued}}
			provider := &fakeAIProvider{
				responses: []llm.Response{
					{Content: "not-json", Usage: llm.Usage{InputTokens: 2, OutputTokens: 2, TotalTokens: 4}},
					{Usage: llm.Usage{InputTokens: 3, OutputTokens: 3, TotalTokens: 6}},
				},
				errors: []error{nil, test.err},
			}
			service := New(store, &fakeAIArticles{article: readyAIArticle()}, nil, provider, testAIConfig())

			err := service.HandleAnalyzeTask(context.Background(), jobTask(TaskAnalyze, 7))
			if !errors.Is(err, asynq.SkipRetry) {
				t.Fatalf("HandleAnalyzeTask() error = %v, want exhausted SkipRetry", err)
			}
			if len(provider.requests) != 2 || len(store.failures) != 1 {
				t.Fatalf("provider calls=%d failures=%#v", len(provider.requests), store.failures)
			}
			failure := store.failures[0]
			if failure.Code != test.err.Code || failure.Message != test.err.Message || failure.Retryable != test.retryable || failure.Requeue {
				t.Fatalf("failure = %#v", failure)
			}
			if failure.Usage != (TokenUsage{InputTokens: 5, OutputTokens: 5, TotalTokens: 10}) {
				t.Fatalf("usage = %#v", failure.Usage)
			}
		})
	}
}

func TestHandleAnalyzeTaskDoesNotRepairSourceValidationErrors(t *testing.T) {
	invalid := validAnalysisOutput()
	invalid.Facts[0].SourceBlockIDs = []string{"B999"}
	store := &fakeAIStore{job: Job{ID: 7, Type: JobTypeAnalysis, ArticleID: 12, Status: JobQueued}}
	provider := &fakeAIProvider{responses: []llm.Response{{Content: encodeJSONForTest(t, invalid), Usage: llm.Usage{TotalTokens: 6}}}}
	service := New(store, &fakeAIArticles{article: readyAIArticle()}, nil, provider, testAIConfig())

	err := service.HandleAnalyzeTask(context.Background(), jobTask(TaskAnalyze, 7))
	if !errors.Is(err, asynq.SkipRetry) {
		t.Fatalf("HandleAnalyzeTask() error = %v, want SkipRetry", err)
	}
	if len(provider.requests) != 1 || len(store.events) != 0 || store.failures[0].Code != "AI_SOURCE_REFERENCE_INVALID" {
		t.Fatalf("provider=%d events=%#v failures=%#v", len(provider.requests), store.events, store.failures)
	}
}

func TestHandleAnalyzeTaskDoesNotRepairQuoteMismatch(t *testing.T) {
	invalid := validAnalysisOutput()
	invalid.Quotes[0].Text = "原文中不存在的引文"
	store := &fakeAIStore{job: Job{ID: 7, Type: JobTypeAnalysis, ArticleID: 12, Status: JobQueued}}
	provider := &fakeAIProvider{responses: []llm.Response{{Content: encodeJSONForTest(t, invalid)}}}
	service := New(store, &fakeAIArticles{article: readyAIArticle()}, nil, provider, testAIConfig())

	err := service.HandleAnalyzeTask(context.Background(), jobTask(TaskAnalyze, 7))
	if !errors.Is(err, asynq.SkipRetry) || len(provider.requests) != 1 || len(store.events) != 0 {
		t.Fatalf("error=%v provider=%d events=%#v", err, len(provider.requests), store.events)
	}
	if store.failures[0].Code != "AI_QUOTE_MISMATCH" {
		t.Fatalf("failure = %#v", store.failures[0])
	}
}

func TestHandleAnalyzeTaskDoesNotHideFormatRepairEventWriteFailure(t *testing.T) {
	eventErr := errors.New("event insert failed")
	store := &fakeAIStore{job: Job{ID: 7, Type: JobTypeAnalysis, ArticleID: 12, Status: JobQueued}, eventErr: eventErr}
	provider := &fakeAIProvider{responses: []llm.Response{{Content: "not-json", Usage: llm.Usage{TotalTokens: 4}}}}
	service := New(store, &fakeAIArticles{article: readyAIArticle()}, nil, provider, testAIConfig())

	err := service.HandleAnalyzeTask(context.Background(), jobTask(TaskAnalyze, 7))
	if !errors.Is(err, asynq.SkipRetry) {
		t.Fatalf("HandleAnalyzeTask() error = %v, want exhausted SkipRetry", err)
	}
	if len(provider.requests) != 1 || store.failures[0].Code != "AI_PROCESSING_FAILED" || !store.failures[0].Retryable || store.failures[0].Usage.TotalTokens != 4 {
		t.Fatalf("provider=%d failures=%#v", len(provider.requests), store.failures)
	}
}

func TestHandleAnalyzeTaskClassifiesProviderFailureWithoutLeakingCause(t *testing.T) {
	providerErr := &llm.Error{
		Code:      llm.ErrorCodeRateLimited,
		Message:   "AI 模型请求受到限流",
		Retryable: true,
		Cause:     errors.New("response body contains api-key-secret"),
	}
	store := &fakeAIStore{job: Job{ID: 7, Type: JobTypeAnalysis, ArticleID: 12, Status: JobQueued}}
	provider := &fakeAIProvider{
		responses: []llm.Response{{Usage: llm.Usage{TotalTokens: 5}}},
		errors:    []error{providerErr},
	}
	service := New(store, &fakeAIArticles{article: readyAIArticle()}, nil, provider, testAIConfig())

	err := service.HandleAnalyzeTask(context.Background(), jobTask(TaskAnalyze, 7))
	if !errors.Is(err, asynq.SkipRetry) {
		t.Fatalf("HandleAnalyzeTask() error = %v, want SkipRetry on exhausted attempt", err)
	}
	failure := store.failures[0]
	if failure.Code != providerErr.Code || failure.Message != providerErr.Message || !failure.Retryable || failure.Requeue {
		t.Fatalf("failure = %#v", failure)
	}
	if strings.Contains(failure.Message, "api-key-secret") || failure.Usage.TotalTokens != 5 {
		t.Fatalf("failure leaked cause or lost usage: %#v", failure)
	}
}

func TestHandleTaskErrorRequeuesRetryableProviderErrorsWhileAttemptsRemain(t *testing.T) {
	for _, code := range []string{llm.ErrorCodeRateLimited, llm.ErrorCodeUnavailable, llm.ErrorCodeTimeout} {
		store := &fakeAIStore{job: Job{ID: 7, Status: JobRunning}}
		service := New(store, &fakeAIArticles{}, nil, nil, testAIConfig())
		providerErr := &llm.Error{Code: code, Message: "temporary", Retryable: true}

		err := service.handleTaskError(context.Background(), 7, providerErr, TokenUsage{TotalTokens: 3}, 0, 3)
		if err == nil || errors.Is(err, asynq.SkipRetry) {
			t.Fatalf("code %s: error = %v, want ordinary retry error", code, err)
		}
		failure := store.failures[0]
		if failure.Code != code || !failure.Retryable || !failure.Requeue || failure.Usage.TotalTokens != 3 {
			t.Fatalf("code %s: failure = %#v", code, failure)
		}
	}
}

func TestHandleTaskErrorStopsRetryableErrorsAtOrPastRetryLimit(t *testing.T) {
	for _, test := range []struct {
		name       string
		retryCount int
	}{
		{name: "at limit", retryCount: 3},
		{name: "past limit", retryCount: 4},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := &fakeAIStore{job: Job{ID: 7, Status: JobRunning}}
			service := New(store, &fakeAIArticles{}, nil, nil, testAIConfig())
			providerErr := &llm.Error{Code: llm.ErrorCodeUnavailable, Message: "temporary", Retryable: true}

			err := service.handleTaskError(context.Background(), 7, providerErr, TokenUsage{TotalTokens: 3}, test.retryCount, 3)
			if !errors.Is(err, asynq.SkipRetry) {
				t.Fatalf("handleTaskError() error = %v, want SkipRetry", err)
			}
			failure := store.failures[0]
			if store.job.Status != JobFailed || !failure.Retryable || failure.Requeue || failure.Usage.TotalTokens != 3 {
				t.Fatalf("job=%#v failure=%#v", store.job, failure)
			}
		})
	}
}

func TestHandleTaskErrorStopsPermanentProviderAndDomainErrors(t *testing.T) {
	cases := []struct {
		err  error
		code string
	}{
		{&llm.Error{Code: llm.ErrorCodeAuthFailed, Message: "auth", Retryable: false}, llm.ErrorCodeAuthFailed},
		{&llm.Error{Code: llm.ErrorCodeRequestFailed, Message: "request", Retryable: false}, llm.ErrorCodeRequestFailed},
		{ErrOutputInvalid, "AI_OUTPUT_INVALID"},
		{ErrSourceReferenceInvalid, "AI_SOURCE_REFERENCE_INVALID"},
		{ErrQuoteMismatch, "AI_QUOTE_MISMATCH"},
		{ErrAssetInvalid, "AI_ASSET_INVALID"},
		{ErrExcessiveSourceOverlap, "AI_EXCESSIVE_SOURCE_OVERLAP"},
		{ErrInvalidParameters, "AI_INVALID_PARAMETERS"},
	}
	for _, test := range cases {
		store := &fakeAIStore{job: Job{ID: 7, Status: JobRunning}}
		service := New(store, &fakeAIArticles{}, nil, nil, testAIConfig())
		err := service.handleTaskError(context.Background(), 7, test.err, TokenUsage{}, 0, 3)
		if !errors.Is(err, asynq.SkipRetry) {
			t.Fatalf("%v: error = %v, want SkipRetry", test.err, err)
		}
		if failure := store.failures[0]; failure.Code != test.code || failure.Retryable || failure.Requeue {
			t.Fatalf("%v: failure = %#v", test.err, failure)
		}
	}
}

func TestHandleTaskErrorReturnsStoreFailure(t *testing.T) {
	writeErr := errors.New("database write failed")
	store := &fakeAIStore{job: Job{ID: 7}, failureErr: writeErr}
	service := New(store, &fakeAIArticles{}, nil, nil, testAIConfig())

	err := service.handleTaskError(context.Background(), 7, ErrOutputInvalid, TokenUsage{}, 0, 3)
	if !errors.Is(err, writeErr) {
		t.Fatalf("handleTaskError() error = %v, want store error", err)
	}
}

func TestHandleAnalyzeTaskResumesRunningAndSkipsCompletedJobs(t *testing.T) {
	validJSON := encodeJSONForTest(t, validAnalysisOutput())
	for _, test := range []struct {
		name          string
		status        string
		responses     []llm.Response
		wantCalls     int
		wantCompletes int
	}{
		{"running", JobRunning, []llm.Response{{Content: validJSON}}, 1, 1},
		{"completed", JobCompleted, nil, 0, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := &fakeAIStore{job: Job{ID: 7, Type: JobTypeAnalysis, ArticleID: 12, Status: test.status}}
			provider := &fakeAIProvider{responses: test.responses}
			service := New(store, &fakeAIArticles{article: readyAIArticle()}, nil, provider, testAIConfig())
			if err := service.HandleAnalyzeTask(context.Background(), jobTask(TaskAnalyze, 7)); err != nil {
				t.Fatal(err)
			}
			if len(provider.requests) != test.wantCalls || store.completeAnalysisCalls != test.wantCompletes {
				t.Fatalf("provider calls=%d complete calls=%d", len(provider.requests), store.completeAnalysisCalls)
			}
		})
	}
}

func TestHandleGenerateTaskRejectsInvalidPayloadWithoutTouchingStore(t *testing.T) {
	for _, raw := range []string{"", `{}`, `{"jobId":0}`, `{"jobId":9,"extra":true}`, `{"jobId":9} trailing`, `{"jobId":7,"jobId":8}`} {
		store := &fakeAIStore{}
		service := New(store, &fakeAIArticles{}, nil, &fakeAIProvider{}, testAIConfig())
		err := service.HandleGenerateTask(context.Background(), asynq.NewTask(TaskGenerate, []byte(raw)))
		if !errors.Is(err, asynq.SkipRetry) {
			t.Fatalf("payload %q: error = %v, want SkipRetry", raw, err)
		}
		if store.setRunningCalls != 0 {
			t.Fatalf("payload %q: SetJobRunning calls = %d", raw, store.setRunningCalls)
		}
	}
}

func TestHandleGenerateTaskLoadsOnlyReferencedAssetsAndCompletesDraft(t *testing.T) {
	article := readyAIArticle()
	asset7 := workspace.Asset{ID: 7, ArticleID: article.ID, DownloadStatus: "completed", IsCover: true}
	asset8 := workspace.Asset{ID: 8, ArticleID: article.ID, DownloadStatus: "completed"}
	article.Assets = []workspace.Asset{asset7, asset8}
	analysis := validAnalysis()
	analysis.ID = 3
	analysis.ArticleID = article.ID
	analysis.Job = &Job{Status: JobCompleted}
	store := &fakeAIStore{
		job: Job{ID: 9, Type: JobTypeGeneration, ArticleID: article.ID, RequestedBy: 5, Status: JobQueued},
		generation: Generation{
			ID: 4, JobID: 9, AnalysisID: analysis.ID, AngleID: "A1", Audience: "技术团队",
			Tone: "professional", TargetWords: 1000,
		},
		analysis: analysis,
	}
	articles := &fakeAIArticles{article: article, assets: map[uint64]workspace.Asset{7: asset7, 8: asset8}}
	provider := &fakeAIProvider{responses: []llm.Response{{Content: encodeJSONForTest(t, validGenerationOutput()), Usage: llm.Usage{TotalTokens: 11}}}}
	service := New(store, articles, nil, provider, testAIConfig())

	if err := service.HandleGenerateTask(context.Background(), jobTask(TaskGenerate, 9)); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(articles.assetIDs, []uint64{7}) {
		t.Fatalf("GetAsset IDs = %v, want [7]", articles.assetIDs)
	}
	if store.completeGenerationCalls != 1 || store.completedDraft.SourceArticleID != article.ID || store.completedDraft.CreatedBy != 5 {
		t.Fatalf("complete calls=%d draft=%#v", store.completeGenerationCalls, store.completedDraft)
	}
	if store.completedDraft.Title != "新标题" || store.completedDraft.Digest != "新摘要" || !strings.Contains(store.completedDraft.ContentHTML, "参考来源") {
		t.Fatalf("draft = %#v", store.completedDraft)
	}
	if store.job.TotalTokens != 11 {
		t.Fatalf("job usage = %#v", store.job.TokenUsage)
	}
}

func TestHandleGenerateTaskValidatesStructureBeforeLoadingAssets(t *testing.T) {
	missingAssetID := uint64(999)
	tests := []struct {
		name   string
		mutate func(*GenerationOutput)
	}{
		{
			name: "non-image block carries asset ID",
			mutate: func(output *GenerationOutput) {
				output.Blocks[0].AssetID = &missingAssetID
			},
		},
		{
			name: "invalid title precedes missing image asset",
			mutate: func(output *GenerationOutput) {
				output.Title = ""
				output.Blocks = append(output.Blocks, GeneratedBlock{Type: "image", AssetID: &missingAssetID})
			},
		},
		{
			name: "invalid block precedes missing image asset",
			mutate: func(output *GenerationOutput) {
				output.Blocks = []GeneratedBlock{
					{Type: "image", AssetID: &missingAssetID},
					{Type: "paragraph"},
				}
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			article := readyAIArticle()
			asset := workspace.Asset{ID: 7, ArticleID: article.ID, DownloadStatus: "completed"}
			analysis := validAnalysis()
			analysis.ID, analysis.ArticleID, analysis.Job = 3, article.ID, &Job{Status: JobCompleted}
			store := &fakeAIStore{
				job:        Job{ID: 9, Type: JobTypeGeneration, ArticleID: article.ID, RequestedBy: 5, Status: JobQueued},
				generation: Generation{ID: 4, JobID: 9, AnalysisID: 3, AngleID: "A1", Audience: "技术团队", Tone: "professional", TargetWords: 1000},
				analysis:   analysis,
			}
			invalid := validGenerationOutput()
			test.mutate(&invalid)
			provider := &fakeAIProvider{responses: []llm.Response{
				{Content: encodeJSONForTest(t, invalid)},
				{Content: encodeJSONForTest(t, validGenerationOutput())},
			}}
			articles := &fakeAIArticles{article: article, assets: map[uint64]workspace.Asset{7: asset}}
			service := New(store, articles, nil, provider, testAIConfig())

			if err := service.HandleGenerateTask(context.Background(), jobTask(TaskGenerate, 9)); err != nil {
				t.Fatal(err)
			}
			if len(provider.requests) != 2 || len(store.events) != 1 || store.events[0].Status != "format_repair" {
				t.Fatalf("provider calls=%d events=%#v", len(provider.requests), store.events)
			}
			if !slices.Equal(articles.assetIDs, []uint64{7}) {
				t.Fatalf("GetAsset IDs = %v, want repaired output asset [7] only", articles.assetIDs)
			}
			if store.completeGenerationCalls != 1 {
				t.Fatalf("complete calls = %d", store.completeGenerationCalls)
			}
		})
	}
}

func TestHandleGenerateTaskNeverCreatesDraftBeforeAllValidationPasses(t *testing.T) {
	output := validGenerationOutput()
	output.Blocks[0].FactIDs = []string{"F999"}
	output.Blocks = output.Blocks[:3]
	article := readyAIArticle()
	analysis := validAnalysis()
	analysis.ID, analysis.ArticleID, analysis.Job = 3, article.ID, &Job{Status: JobCompleted}
	store := &fakeAIStore{
		job:        Job{ID: 9, Type: JobTypeGeneration, ArticleID: article.ID, RequestedBy: 5, Status: JobQueued},
		generation: Generation{ID: 4, JobID: 9, AnalysisID: 3, AngleID: "A1", Audience: "技术团队", Tone: "professional", TargetWords: 1000},
		analysis:   analysis,
	}
	provider := &fakeAIProvider{responses: []llm.Response{{Content: encodeJSONForTest(t, output)}}}
	service := New(store, &fakeAIArticles{article: article}, nil, provider, testAIConfig())

	err := service.HandleGenerateTask(context.Background(), jobTask(TaskGenerate, 9))
	if !errors.Is(err, asynq.SkipRetry) {
		t.Fatalf("HandleGenerateTask() error = %v, want SkipRetry", err)
	}
	if store.completeGenerationCalls != 0 || len(provider.requests) != 1 || len(store.events) != 0 {
		t.Fatalf("complete=%d provider=%d events=%#v", store.completeGenerationCalls, len(provider.requests), store.events)
	}
	if store.failures[0].Code != "AI_SOURCE_REFERENCE_INVALID" {
		t.Fatalf("failure = %#v", store.failures[0])
	}
}

func TestHandleGenerateTaskDoesNotRepairAssetOrOverlapOrParameterErrors(t *testing.T) {
	baseArticle := readyAIArticle()
	baseAnalysis := validAnalysis()
	baseAnalysis.ID, baseAnalysis.ArticleID, baseAnalysis.Job = 3, baseArticle.ID, &Job{Status: JobCompleted}
	tests := []struct {
		name       string
		article    workspace.Article
		generation Generation
		output     GenerationOutput
		assets     map[uint64]workspace.Asset
		wantCode   string
		wantCalls  int
	}{
		{
			name:       "asset",
			article:    baseArticle,
			generation: Generation{AnalysisID: 3, AngleID: "A1", Audience: "技术团队", Tone: "professional", TargetWords: 1000},
			output:     validGenerationOutput(),
			assets:     map[uint64]workspace.Asset{7: {ID: 7, ArticleID: 999, DownloadStatus: "completed"}},
			wantCode:   "AI_ASSET_INVALID",
			wantCalls:  1,
		},
		{
			name: "overlap",
			article: workspace.Article{
				ID: 12, Status: "ready", PlainText: strings.Repeat("重", 80),
				Blocks: []workspace.Block{{Type: "paragraph", Text: strings.Repeat("重", 80)}},
			},
			generation: Generation{AnalysisID: 3, AngleID: "A1", Audience: "技术团队", Tone: "professional", TargetWords: 1000},
			output:     GenerationOutput{Title: "标题", Digest: "摘要", Blocks: []GeneratedBlock{{Type: "paragraph", Text: strings.Repeat("重", 80)}}},
			wantCode:   "AI_EXCESSIVE_SOURCE_OVERLAP",
			wantCalls:  1,
		},
		{
			name:       "parameters",
			article:    baseArticle,
			generation: Generation{AnalysisID: 3, AngleID: "A1", Audience: "技术团队", Tone: "forbidden", TargetWords: 1000},
			wantCode:   "AI_INVALID_PARAMETERS",
			wantCalls:  0,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			analysis := baseAnalysis
			analysis.ArticleID = test.article.ID
			store := &fakeAIStore{
				job:        Job{ID: 9, Type: JobTypeGeneration, ArticleID: test.article.ID, RequestedBy: 5, Status: JobQueued},
				generation: test.generation,
				analysis:   analysis,
			}
			provider := &fakeAIProvider{}
			if test.wantCalls > 0 {
				provider.responses = []llm.Response{{Content: encodeJSONForTest(t, test.output)}}
			}
			service := New(store, &fakeAIArticles{article: test.article, assets: test.assets}, nil, provider, testAIConfig())
			err := service.HandleGenerateTask(context.Background(), jobTask(TaskGenerate, 9))
			if !errors.Is(err, asynq.SkipRetry) || len(provider.requests) != test.wantCalls || len(store.events) != 0 || store.completeGenerationCalls != 0 {
				t.Fatalf("error=%v provider=%d events=%#v complete=%d", err, len(provider.requests), store.events, store.completeGenerationCalls)
			}
			if store.failures[0].Code != test.wantCode || store.failures[0].Retryable {
				t.Fatalf("failure = %#v", store.failures[0])
			}
		})
	}
}

func TestHandleGenerateTaskKeepsAssetStoreFailuresRetryable(t *testing.T) {
	article := readyAIArticle()
	analysis := validAnalysis()
	analysis.ID, analysis.ArticleID, analysis.Job = 3, article.ID, &Job{Status: JobCompleted}
	store := &fakeAIStore{
		job:        Job{ID: 9, Type: JobTypeGeneration, ArticleID: article.ID, RequestedBy: 5, Status: JobQueued},
		generation: Generation{AnalysisID: 3, AngleID: "A1", Audience: "技术团队", Tone: "professional", TargetWords: 1000},
		analysis:   analysis,
	}
	provider := &fakeAIProvider{responses: []llm.Response{{Content: encodeJSONForTest(t, validGenerationOutput())}}}
	service := New(store, &fakeAIArticles{article: article, assetErr: errors.New("database unavailable")}, nil, provider, testAIConfig())

	err := service.HandleGenerateTask(context.Background(), jobTask(TaskGenerate, 9))
	if !errors.Is(err, asynq.SkipRetry) {
		t.Fatalf("HandleGenerateTask() error = %v, want exhausted SkipRetry", err)
	}
	failure := store.failures[0]
	if failure.Code != "AI_PROCESSING_FAILED" || !failure.Retryable || failure.Requeue {
		t.Fatalf("failure = %#v", failure)
	}
}

func TestHandleGenerateTaskRepairsStructuralOutputOnlyOnce(t *testing.T) {
	article := readyAIArticle()
	asset := workspace.Asset{ID: 7, ArticleID: article.ID, DownloadStatus: "completed"}
	article.Assets = []workspace.Asset{asset}
	analysis := validAnalysis()
	analysis.ID, analysis.ArticleID, analysis.Job = 3, article.ID, &Job{Status: JobCompleted}
	store := &fakeAIStore{
		job:        Job{ID: 9, Type: JobTypeGeneration, ArticleID: article.ID, RequestedBy: 5, Status: JobQueued},
		generation: Generation{ID: 4, JobID: 9, AnalysisID: 3, AngleID: "A1", Audience: "技术团队", Tone: "professional", TargetWords: 1000},
		analysis:   analysis,
	}
	provider := &fakeAIProvider{responses: []llm.Response{
		{Content: `{"title":"缺少字段"}`, Usage: llm.Usage{TotalTokens: 4}},
		{Content: encodeJSONForTest(t, validGenerationOutput()), Usage: llm.Usage{TotalTokens: 6}},
	}}
	service := New(store, &fakeAIArticles{article: article, assets: map[uint64]workspace.Asset{7: asset}}, nil, provider, testAIConfig())

	if err := service.HandleGenerateTask(context.Background(), jobTask(TaskGenerate, 9)); err != nil {
		t.Fatal(err)
	}
	if len(provider.requests) != 2 || provider.requests[1].Temperature != 0 || len(store.events) != 1 || store.job.TotalTokens != 10 {
		t.Fatalf("requests=%#v events=%#v usage=%#v", provider.requests, store.events, store.job.TokenUsage)
	}
}

func TestHandleGenerateTaskPreservesAssetStoreErrorFromRepair(t *testing.T) {
	article := readyAIArticle()
	analysis := validAnalysis()
	analysis.ID, analysis.ArticleID, analysis.Job = 3, article.ID, &Job{Status: JobCompleted}
	store := &fakeAIStore{
		job:        Job{ID: 9, Type: JobTypeGeneration, ArticleID: article.ID, RequestedBy: 5, Status: JobQueued},
		generation: Generation{AnalysisID: 3, AngleID: "A1", Audience: "技术团队", Tone: "professional", TargetWords: 1000},
		analysis:   analysis,
	}
	provider := &fakeAIProvider{responses: []llm.Response{
		{Content: `{"title":"缺少字段"}`, Usage: llm.Usage{TotalTokens: 2}},
		{Content: encodeJSONForTest(t, validGenerationOutput()), Usage: llm.Usage{TotalTokens: 3}},
	}}
	service := New(store, &fakeAIArticles{article: article, assetErr: errors.New("database unavailable")}, nil, provider, testAIConfig())

	err := service.HandleGenerateTask(context.Background(), jobTask(TaskGenerate, 9))
	if !errors.Is(err, asynq.SkipRetry) {
		t.Fatalf("HandleGenerateTask() error = %v, want exhausted SkipRetry", err)
	}
	if len(provider.requests) != 2 || len(store.failures) != 1 {
		t.Fatalf("provider calls=%d failures=%#v", len(provider.requests), store.failures)
	}
	failure := store.failures[0]
	if failure.Code != "AI_PROCESSING_FAILED" || !failure.Retryable || failure.Requeue || failure.Usage.TotalTokens != 5 {
		t.Fatalf("failure = %#v", failure)
	}
}

func TestHandleGenerateTaskPreservesDomainErrorsFromRepair(t *testing.T) {
	baseArticle := readyAIArticle()
	baseAnalysis := validAnalysis()
	baseAnalysis.ID, baseAnalysis.ArticleID, baseAnalysis.Job = 3, baseArticle.ID, &Job{Status: JobCompleted}
	assetID := uint64(7)
	for _, test := range []struct {
		name     string
		article  workspace.Article
		output   GenerationOutput
		assets   map[uint64]workspace.Asset
		wantCode string
	}{
		{
			name:     "source reference",
			article:  baseArticle,
			output:   GenerationOutput{Title: "标题", Digest: "摘要", Blocks: []GeneratedBlock{{Type: "paragraph", Text: "重写正文", FactIDs: []string{"F999"}}}},
			wantCode: "AI_SOURCE_REFERENCE_INVALID",
		},
		{
			name:     "quote mismatch",
			article:  baseArticle,
			output:   GenerationOutput{Title: "标题", Digest: "摘要", Blocks: []GeneratedBlock{{Type: "quote", Text: "错误引文", QuoteID: "Q1"}}},
			wantCode: "AI_QUOTE_MISMATCH",
		},
		{
			name:     "asset invalid",
			article:  baseArticle,
			output:   GenerationOutput{Title: "标题", Digest: "摘要", Blocks: []GeneratedBlock{{Type: "image", AssetID: &assetID}}},
			assets:   map[uint64]workspace.Asset{assetID: {ID: assetID, ArticleID: 999, DownloadStatus: "completed"}},
			wantCode: "AI_ASSET_INVALID",
		},
		{
			name: "source overlap",
			article: workspace.Article{
				ID: 12, Status: "ready", PlainText: strings.Repeat("重", 80),
				Blocks: []workspace.Block{{Type: "paragraph", Text: strings.Repeat("重", 80)}},
			},
			output:   GenerationOutput{Title: "标题", Digest: "摘要", Blocks: []GeneratedBlock{{Type: "paragraph", Text: strings.Repeat("重", 80)}}},
			wantCode: "AI_EXCESSIVE_SOURCE_OVERLAP",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			analysis := baseAnalysis
			analysis.ArticleID = test.article.ID
			store := &fakeAIStore{
				job:        Job{ID: 9, Type: JobTypeGeneration, ArticleID: test.article.ID, RequestedBy: 5, Status: JobQueued},
				generation: Generation{AnalysisID: 3, AngleID: "A1", Audience: "技术团队", Tone: "professional", TargetWords: 1000},
				analysis:   analysis,
			}
			provider := &fakeAIProvider{responses: []llm.Response{
				{Content: `{"title":"缺少字段"}`, Usage: llm.Usage{TotalTokens: 2}},
				{Content: encodeJSONForTest(t, test.output), Usage: llm.Usage{TotalTokens: 3}},
			}}
			service := New(store, &fakeAIArticles{article: test.article, assets: test.assets}, nil, provider, testAIConfig())

			err := service.HandleGenerateTask(context.Background(), jobTask(TaskGenerate, 9))
			if !errors.Is(err, asynq.SkipRetry) {
				t.Fatalf("HandleGenerateTask() error = %v, want SkipRetry", err)
			}
			if len(provider.requests) != 2 || len(store.failures) != 1 || store.failures[0].Code != test.wantCode || store.failures[0].Usage.TotalTokens != 5 {
				t.Fatalf("provider calls=%d failures=%#v", len(provider.requests), store.failures)
			}
		})
	}
}

func TestHandleGenerateTaskResumesRunningAndSkipsCompletedJobs(t *testing.T) {
	article := readyAIArticle()
	analysis := validAnalysis()
	analysis.ID, analysis.ArticleID, analysis.Job = 3, article.ID, &Job{Status: JobCompleted}
	for _, test := range []struct {
		name          string
		status        string
		responses     []llm.Response
		wantCalls     int
		wantCompletes int
	}{
		{"running", JobRunning, []llm.Response{{Content: encodeJSONForTest(t, GenerationOutput{Title: "标题", Digest: "摘要", Blocks: []GeneratedBlock{{Type: "paragraph", Text: "重写正文"}}})}}, 1, 1},
		{"completed", JobCompleted, nil, 0, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := &fakeAIStore{
				job:        Job{ID: 9, Type: JobTypeGeneration, ArticleID: article.ID, RequestedBy: 5, Status: test.status},
				generation: Generation{ID: 4, JobID: 9, AnalysisID: 3, AngleID: "A1", Audience: "技术团队", Tone: "professional", TargetWords: 1000},
				analysis:   analysis,
			}
			provider := &fakeAIProvider{responses: test.responses}
			service := New(store, &fakeAIArticles{article: article}, nil, provider, testAIConfig())
			if err := service.HandleGenerateTask(context.Background(), jobTask(TaskGenerate, 9)); err != nil {
				t.Fatal(err)
			}
			if len(provider.requests) != test.wantCalls || store.completeGenerationCalls != test.wantCompletes {
				t.Fatalf("provider calls=%d complete calls=%d", len(provider.requests), store.completeGenerationCalls)
			}
		})
	}
}

func enqueueOption(options []asynq.Option, optionType asynq.OptionType) any {
	for _, option := range options {
		if option.Type() == optionType {
			return option.Value()
		}
	}
	return nil
}

func encodeJSONForTest(t *testing.T, value any) string {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}

func jobTask(taskType string, jobID uint64) *asynq.Task {
	return asynq.NewTask(taskType, []byte(fmt.Sprintf(`{"jobId":%d}`, jobID)))
}

func sumUsageForTest(left, right TokenUsage) TokenUsage {
	return TokenUsage{
		InputTokens:  left.InputTokens + right.InputTokens,
		OutputTokens: left.OutputTokens + right.OutputTokens,
		TotalTokens:  left.TotalTokens + right.TotalTokens,
	}
}
