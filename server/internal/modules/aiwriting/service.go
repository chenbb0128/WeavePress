package aiwriting

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/hibiken/asynq"

	"github.com/chenbb0128/weavepress/server/internal/config"
	"github.com/chenbb0128/weavepress/server/internal/modules/editorial"
	"github.com/chenbb0128/weavepress/server/internal/modules/workspace"
	"github.com/chenbb0128/weavepress/server/internal/platform/llm"
)

type Enqueuer interface {
	EnqueueContext(context.Context, *asynq.Task, ...asynq.Option) (*asynq.TaskInfo, error)
}

type Service struct {
	store    Store
	articles ArticleStore
	queue    Enqueuer
	provider llm.Provider
	cfg      config.AIConfig
}

func New(store Store, articles ArticleStore, queue Enqueuer, provider llm.Provider, cfg config.AIConfig) *Service {
	return &Service{store: store, articles: articles, queue: queue, provider: provider, cfg: cfg}
}

func shouldDispatchReusedJob(job Job) bool {
	return job.Status == JobQueued && job.ErrorCode == ""
}

func (s *Service) StartAnalysis(ctx context.Context, articleID, userID uint64, force bool) (Job, bool, error) {
	if !s.cfg.Enabled {
		return Job{}, false, ErrNotConfigured
	}
	article, err := s.articles.GetArticle(ctx, articleID)
	if err != nil {
		return Job{}, false, err
	}
	if article.Status != "ready" {
		return Job{}, false, ErrArticleNotReady
	}
	source := BuildSourceDocument(article)
	if utf8.RuneCountInString(source.PlainText) > s.cfg.MaxInputChars {
		return Job{}, false, ErrInputTooLarge
	}
	fingerprint := fingerprint(struct {
		Title      string
		SourceName string
		PlainText  string
		Blocks     []SourceBlock
	}{article.Title, article.SourceName, source.PlainText, source.Blocks})
	job, reused, err := s.store.CreateAnalysisJob(ctx, CreateAnalysisJobInput{
		ArticleID:        article.ID,
		RequestedBy:      userID,
		Provider:         s.cfg.Provider,
		Model:            s.cfg.Model,
		PromptVersion:    AnalysisPromptV1,
		InputFingerprint: fingerprint,
		Force:            force,
	})
	if err != nil {
		return Job{}, false, err
	}
	if reused && !shouldDispatchReusedJob(job) {
		return job, true, nil
	}
	if err := s.enqueue(ctx, job); err != nil {
		if reused {
			return job, true, err
		}
		return Job{}, false, s.recordQueueFailure(ctx, job.ID, err)
	}
	return job, reused, nil
}

func (s *Service) Status() Status {
	return Status{Enabled: s.cfg.Enabled, Provider: s.cfg.Provider, Model: s.cfg.Model}
}

func (s *Service) Analyses(ctx context.Context, articleID uint64, page, pageSize int) (Page[Analysis], error) {
	if _, err := s.articles.GetArticle(ctx, articleID); err != nil {
		return Page[Analysis]{}, err
	}
	return s.store.ListAnalyses(ctx, articleID, page, pageSize)
}

func (s *Service) Analysis(ctx context.Context, id uint64) (Analysis, error) {
	return s.store.GetAnalysis(ctx, id)
}

func (s *Service) StartGeneration(ctx context.Context, analysisID, userID uint64, params GenerationParams) (Generation, Job, bool, error) {
	if !s.cfg.Enabled {
		return Generation{}, Job{}, false, ErrNotConfigured
	}
	analysis, err := s.store.GetAnalysis(ctx, analysisID)
	if err != nil {
		return Generation{}, Job{}, false, err
	}
	if analysis.Job == nil || analysis.Job.Status != JobCompleted {
		return Generation{}, Job{}, false, ErrAnalysisNotReady
	}
	if err := ValidateGenerationRequest(analysis, params); err != nil {
		return Generation{}, Job{}, false, err
	}
	article, err := s.articles.GetArticle(ctx, analysis.ArticleID)
	if err != nil {
		return Generation{}, Job{}, false, err
	}
	if article.Status != "ready" {
		return Generation{}, Job{}, false, ErrArticleNotReady
	}
	source := BuildSourceDocument(article)
	if utf8.RuneCountInString(source.PlainText) > s.cfg.MaxInputChars {
		return Generation{}, Job{}, false, ErrInputTooLarge
	}
	inputFingerprint := fingerprint(struct {
		Source   SourceDocument
		Analysis AnalysisOutput
		Params   GenerationParams
	}{source, analysis.AnalysisOutput, params})
	generation, job, reused, err := s.store.CreateGenerationJob(ctx, CreateGenerationJobInput{
		AnalysisID:       analysisID,
		RequestedBy:      userID,
		Params:           params,
		Provider:         s.cfg.Provider,
		Model:            s.cfg.Model,
		PromptVersion:    GenerationPromptV1,
		InputFingerprint: inputFingerprint,
	})
	if err != nil {
		return Generation{}, Job{}, false, err
	}
	if reused && !shouldDispatchReusedJob(job) {
		return generation, job, true, nil
	}
	if err := s.enqueue(ctx, job); err != nil {
		if reused {
			return generation, job, true, err
		}
		return Generation{}, Job{}, false, s.recordQueueFailure(ctx, job.ID, err)
	}
	return generation, job, reused, nil
}

func (s *Service) Generation(ctx context.Context, id uint64) (Generation, error) {
	return s.store.GetGeneration(ctx, id)
}

func (s *Service) Job(ctx context.Context, id uint64) (Job, error) {
	return s.store.GetJob(ctx, id, true)
}

func (s *Service) Jobs(ctx context.Context, filter JobFilter, page, pageSize int) (Page[Job], error) {
	return s.store.ListJobs(ctx, filter, page, pageSize)
}

func (s *Service) Retry(ctx context.Context, jobID, userID uint64) (Job, error) {
	if !s.cfg.Enabled {
		return Job{}, ErrNotConfigured
	}
	job, err := s.store.RetryJob(ctx, jobID, userID)
	if err != nil {
		return Job{}, err
	}
	if err := s.enqueue(ctx, job); err != nil {
		return Job{}, s.recordQueueFailure(ctx, job.ID, err)
	}
	return job, nil
}

func (s *Service) enqueue(ctx context.Context, job Job) error {
	if s.queue == nil {
		return fmt.Errorf("AI queue unavailable")
	}
	payload, err := json.Marshal(struct {
		JobID uint64 `json:"jobId"`
	}{JobID: job.ID})
	if err != nil {
		return err
	}
	taskType := TaskAnalyze
	if job.Type == JobTypeGeneration {
		taskType = TaskGenerate
	}
	taskID := fmt.Sprintf("ai:%s:%d:%d:%d", job.Type, job.ID, job.Attempts, job.ManualRetries)
	_, err = s.queue.EnqueueContext(ctx, asynq.NewTask(taskType, payload),
		asynq.TaskID(taskID),
		asynq.Queue("ai"),
		asynq.MaxRetry(3),
		asynq.Timeout(s.cfg.RequestTimeout+30*time.Second),
	)
	if errors.Is(err, asynq.ErrTaskIDConflict) {
		return nil
	}
	return err
}

func (s *Service) recordQueueFailure(ctx context.Context, jobID uint64, queueErr error) error {
	failureErr := s.store.SetJobFailure(ctx, jobID, JobFailureInput{
		Code:      "AI_QUEUE_UNAVAILABLE",
		Message:   "AI 任务队列暂不可用",
		Retryable: true,
		Requeue:   false,
	})
	if failureErr != nil {
		return errors.Join(queueErr, failureErr)
	}
	return queueErr
}

func fingerprint(value any) [sha256.Size]byte {
	encoded, _ := json.Marshal(value)
	return sha256.Sum256(encoded)
}

func (s *Service) HandleAnalyzeTask(ctx context.Context, task *asynq.Task) error {
	jobID, err := decodeJobPayload(task)
	if err != nil {
		return fmt.Errorf("%w: invalid AI analyze payload", asynq.SkipRetry)
	}
	if err := s.store.SetJobRunning(ctx, jobID); err != nil {
		return err
	}
	job, err := s.store.GetJob(ctx, jobID, false)
	if err != nil {
		return s.handleTaskErrorFromContext(ctx, jobID, err)
	}
	if job.Status == JobCompleted {
		return nil
	}
	err = s.processAnalysis(ctx, job)
	if err == nil {
		return nil
	}
	return s.handleTaskErrorFromContext(ctx, jobID, err)
}

func (s *Service) processAnalysis(ctx context.Context, job Job) error {
	if job.Type != JobTypeAnalysis || job.Status != JobRunning {
		return ErrInvalidParameters
	}
	if s.provider == nil {
		return ErrNotConfigured
	}
	article, err := s.articles.GetArticle(ctx, job.ArticleID)
	if err != nil {
		return err
	}
	if article.Status != "ready" {
		return ErrArticleNotReady
	}
	source := BuildSourceDocument(article)
	if utf8.RuneCountInString(source.PlainText) > s.cfg.MaxInputChars {
		return ErrInputTooLarge
	}

	response, err := s.provider.Complete(ctx, llm.Request{
		Messages:    BuildAnalysisMessages(source),
		MaxTokens:   s.cfg.MaxOutputTokens,
		Temperature: s.cfg.Temperature,
		JSON:        true,
	})
	usage := tokenUsage(response.Usage)
	if err != nil {
		return withUsage(err, usage)
	}
	output, validationErr := DecodeAnalysisOutput(response.Content)
	if validationErr == nil {
		validationErr = ValidateAnalysis(source, output)
	}
	if validationErr != nil {
		if !errors.Is(validationErr, ErrOutputInvalid) {
			return withUsage(validationErr, usage)
		}
		if err := s.store.AddJobEvent(ctx, job.ID, "format_repair", "AI 分析输出格式无效，正在执行一次格式修复"); err != nil {
			return withUsage(err, usage)
		}
		output, usage, validationErr = s.repairAnalysis(ctx, source, response.Content, usage)
		if validationErr != nil {
			return withUsage(normalizeRepairError(validationErr), usage)
		}
	}
	_, err = s.store.CompleteAnalysis(ctx, job.ID, output, usage)
	if err != nil {
		return withUsage(err, usage)
	}
	return nil
}

func (s *Service) repairAnalysis(ctx context.Context, source SourceDocument, raw string, usage TokenUsage) (AnalysisOutput, TokenUsage, error) {
	response, err := s.provider.Complete(ctx, llm.Request{
		Messages:    BuildRepairMessages(JobTypeAnalysis, raw),
		MaxTokens:   s.cfg.MaxOutputTokens,
		Temperature: 0,
		JSON:        true,
	})
	usage = addUsage(usage, tokenUsage(response.Usage))
	if err != nil {
		return AnalysisOutput{}, usage, err
	}
	output, err := DecodeAnalysisOutput(response.Content)
	if err != nil {
		return AnalysisOutput{}, usage, err
	}
	if err := ValidateAnalysis(source, output); err != nil {
		return AnalysisOutput{}, usage, err
	}
	return output, usage, nil
}

func (s *Service) HandleGenerateTask(ctx context.Context, task *asynq.Task) error {
	jobID, err := decodeJobPayload(task)
	if err != nil {
		return fmt.Errorf("%w: invalid AI generate payload", asynq.SkipRetry)
	}
	if err := s.store.SetJobRunning(ctx, jobID); err != nil {
		return err
	}
	job, err := s.store.GetJob(ctx, jobID, false)
	if err != nil {
		return s.handleTaskErrorFromContext(ctx, jobID, err)
	}
	if job.Status == JobCompleted {
		return nil
	}
	err = s.processGeneration(ctx, job)
	if err == nil {
		return nil
	}
	return s.handleTaskErrorFromContext(ctx, jobID, err)
}

func (s *Service) processGeneration(ctx context.Context, job Job) error {
	if job.Type != JobTypeGeneration || job.Status != JobRunning {
		return ErrInvalidParameters
	}
	if s.provider == nil {
		return ErrNotConfigured
	}
	generation, err := s.store.GetGenerationByJobID(ctx, job.ID)
	if err != nil {
		return err
	}
	analysis, err := s.store.GetAnalysis(ctx, generation.AnalysisID)
	if err != nil {
		return err
	}
	if analysis.Job == nil || analysis.Job.Status != JobCompleted {
		return ErrAnalysisNotReady
	}
	article, err := s.articles.GetArticle(ctx, job.ArticleID)
	if err != nil {
		return err
	}
	if article.Status != "ready" {
		return ErrArticleNotReady
	}
	source := BuildSourceDocument(article)
	if utf8.RuneCountInString(source.PlainText) > s.cfg.MaxInputChars {
		return ErrInputTooLarge
	}
	params := storedGenerationParams(generation)
	messages, err := BuildGenerationMessages(source, analysis, params)
	if err != nil {
		return err
	}
	response, err := s.provider.Complete(ctx, llm.Request{
		Messages:    messages,
		MaxTokens:   s.cfg.MaxOutputTokens,
		Temperature: s.cfg.Temperature,
		JSON:        true,
	})
	usage := tokenUsage(response.Usage)
	if err != nil {
		return withUsage(err, usage)
	}
	output, validationErr := s.decodeAndValidateGeneration(ctx, source, analysis, response.Content)
	if validationErr != nil {
		if !errors.Is(validationErr, ErrOutputInvalid) {
			return withUsage(validationErr, usage)
		}
		if err := s.store.AddJobEvent(ctx, job.ID, "format_repair", "AI 稿件输出格式无效，正在执行一次格式修复"); err != nil {
			return withUsage(err, usage)
		}
		output, usage, validationErr = s.repairGeneration(ctx, source, analysis, response.Content, usage)
		if validationErr != nil {
			return withUsage(normalizeRepairError(validationErr), usage)
		}
	}
	contentHTML := RenderGeneration(article, output.Blocks)
	draftInput := editorial.GeneratedDraftInput{
		SourceArticleID: article.ID,
		CreatedBy:       job.RequestedBy,
		Title:           output.Title,
		Digest:          output.Digest,
		ContentHTML:     contentHTML,
		CoverAssetID:    preferredCover(article),
		ChangeNote:      "AI 合规采编生成",
	}
	_, err = s.store.CompleteGeneration(ctx, job.ID, output, draftInput, usage)
	if err != nil {
		return withUsage(err, usage)
	}
	return nil
}

func (s *Service) repairGeneration(ctx context.Context, source SourceDocument, analysis Analysis, raw string, usage TokenUsage) (GenerationOutput, TokenUsage, error) {
	response, err := s.provider.Complete(ctx, llm.Request{
		Messages:    BuildRepairMessages(JobTypeGeneration, raw),
		MaxTokens:   s.cfg.MaxOutputTokens,
		Temperature: 0,
		JSON:        true,
	})
	usage = addUsage(usage, tokenUsage(response.Usage))
	if err != nil {
		return GenerationOutput{}, usage, err
	}
	output, err := s.decodeAndValidateGeneration(ctx, source, analysis, response.Content)
	if err != nil {
		return GenerationOutput{}, usage, err
	}
	return output, usage, nil
}

func normalizeRepairError(err error) error {
	var providerErr *llm.Error
	if errors.As(err, &providerErr) {
		return err
	}
	if errors.Is(err, ErrOutputInvalid) {
		return ErrOutputInvalid
	}
	return err
}

func (s *Service) decodeAndValidateGeneration(ctx context.Context, source SourceDocument, analysis Analysis, raw string) (GenerationOutput, error) {
	output, err := DecodeGenerationOutput(raw)
	if err != nil {
		return GenerationOutput{}, err
	}
	if err := validateGenerationStructure(source, analysis, output); err != nil {
		return GenerationOutput{}, err
	}
	assets, err := s.loadReferencedAssets(ctx, output)
	if err != nil {
		return GenerationOutput{}, err
	}
	if err := validateGenerationAssets(source, assets, output); err != nil {
		return GenerationOutput{}, err
	}
	return output, nil
}

func (s *Service) loadReferencedAssets(ctx context.Context, output GenerationOutput) (map[uint64]workspace.Asset, error) {
	assets := make(map[uint64]workspace.Asset)
	for _, block := range output.Blocks {
		if block.Type != "image" || block.AssetID == nil || *block.AssetID == 0 {
			continue
		}
		id := *block.AssetID
		if _, ok := assets[id]; ok {
			continue
		}
		asset, err := s.articles.GetAsset(ctx, id)
		if err != nil {
			if errors.Is(err, workspace.ErrNotFound) {
				return nil, fmt.Errorf("%w: asset %d", ErrAssetInvalid, id)
			}
			return nil, err
		}
		assets[id] = asset
	}
	return assets, nil
}

func storedGenerationParams(generation Generation) GenerationParams {
	return GenerationParams{
		AngleID:                generation.AngleID,
		Audience:               generation.Audience,
		Tone:                   generation.Tone,
		TargetWords:            generation.TargetWords,
		AdditionalInstructions: generation.AdditionalInstructions,
		IdempotencyKey:         "stored-generation",
	}
}

func preferredCover(article workspace.Article) *uint64 {
	var fallback *uint64
	for _, asset := range article.Assets {
		if asset.ID == 0 || asset.ArticleID != article.ID || asset.DownloadStatus != "completed" {
			continue
		}
		id := asset.ID
		if asset.IsCover {
			return &id
		}
		if fallback == nil {
			fallback = &id
		}
	}
	return fallback
}

func (s *Service) handleTaskErrorFromContext(ctx context.Context, jobID uint64, err error) error {
	retryCount, _ := asynq.GetRetryCount(ctx)
	maxRetry, _ := asynq.GetMaxRetry(ctx)
	return s.handleTaskError(ctx, jobID, err, usageOf(err), retryCount, maxRetry)
}

func (s *Service) handleTaskError(ctx context.Context, jobID uint64, err error, usage TokenUsage, retryCount, maxRetry int) error {
	code, message, retryable := classifyAIError(err)
	requeue := retryable && retryCount < maxRetry
	if failureErr := s.store.SetJobFailure(ctx, jobID, JobFailureInput{
		Code:      code,
		Message:   message,
		Retryable: retryable,
		Requeue:   requeue,
		Usage:     usage,
	}); failureErr != nil {
		return errors.Join(err, failureErr)
	}
	if requeue {
		return err
	}
	return fmt.Errorf("%w: %s", asynq.SkipRetry, message)
}

func classifyAIError(err error) (string, string, bool) {
	var providerErr *llm.Error
	if errors.As(err, &providerErr) {
		return providerErr.Code, providerErr.Message, providerErr.Retryable
	}
	switch {
	case errors.Is(err, ErrNotConfigured):
		return "AI_NOT_CONFIGURED", "AI 服务尚未配置", false
	case errors.Is(err, ErrInputTooLarge):
		return "AI_INPUT_TOO_LARGE", "来源文章超过 AI 输入长度限制", false
	case errors.Is(err, ErrArticleNotReady):
		return "AI_ARTICLE_NOT_READY", "来源文章尚未准备完成", false
	case errors.Is(err, ErrAnalysisNotReady):
		return "AI_ANALYSIS_NOT_READY", "AI 分析尚未完成", false
	case errors.Is(err, ErrInvalidParameters):
		return "AI_INVALID_PARAMETERS", "AI 生成参数无效", false
	case errors.Is(err, ErrOutputInvalid):
		return "AI_OUTPUT_INVALID", "AI 返回内容的结构无效", false
	case errors.Is(err, ErrSourceReferenceInvalid):
		return "AI_SOURCE_REFERENCE_INVALID", "AI 返回内容引用了不存在的来源", false
	case errors.Is(err, ErrQuoteMismatch):
		return "AI_QUOTE_MISMATCH", "AI 返回的直接引用与来源不一致", false
	case errors.Is(err, ErrAssetInvalid):
		return "AI_ASSET_INVALID", "AI 返回内容引用了无效素材", false
	case errors.Is(err, ErrExcessiveSourceOverlap):
		return "AI_EXCESSIVE_SOURCE_OVERLAP", "AI 返回内容与来源文章重合过多", false
	case errors.Is(err, ErrJobNotRetryable):
		return "AI_JOB_NOT_RETRYABLE", "AI 任务不可重试", false
	default:
		return "AI_PROCESSING_FAILED", "AI 任务处理失败", true
	}
}

type usageError struct {
	err   error
	usage TokenUsage
}

func (e *usageError) Error() string { return e.err.Error() }
func (e *usageError) Unwrap() error { return e.err }

func withUsage(err error, usage TokenUsage) error {
	if err == nil {
		return nil
	}
	return &usageError{err: err, usage: usage}
}

func usageOf(err error) TokenUsage {
	var target *usageError
	if errors.As(err, &target) {
		return target.usage
	}
	return TokenUsage{}
}

func tokenUsage(usage llm.Usage) TokenUsage {
	return TokenUsage{InputTokens: usage.InputTokens, OutputTokens: usage.OutputTokens, TotalTokens: usage.TotalTokens}
}

func addUsage(left, right TokenUsage) TokenUsage {
	return TokenUsage{
		InputTokens:  left.InputTokens + right.InputTokens,
		OutputTokens: left.OutputTokens + right.OutputTokens,
		TotalTokens:  left.TotalTokens + right.TotalTokens,
	}
}

func decodeJobPayload(task *asynq.Task) (uint64, error) {
	if task == nil {
		return 0, fmt.Errorf("task is nil")
	}
	decoder := json.NewDecoder(strings.NewReader(string(task.Payload())))
	opening, err := decoder.Token()
	if err != nil {
		return 0, err
	}
	if opening != json.Delim('{') {
		return 0, fmt.Errorf("payload must be one JSON object")
	}

	var jobID uint64
	seenJobID := false
	for decoder.More() {
		fieldToken, err := decoder.Token()
		if err != nil {
			return 0, err
		}
		field, ok := fieldToken.(string)
		if !ok || field != "jobId" {
			return 0, fmt.Errorf("unknown payload field")
		}
		if seenJobID {
			return 0, fmt.Errorf("duplicate jobId")
		}
		seenJobID = true
		if err := decoder.Decode(&jobID); err != nil {
			return 0, err
		}
	}
	closing, err := decoder.Token()
	if err != nil {
		return 0, err
	}
	if closing != json.Delim('}') {
		return 0, fmt.Errorf("payload must be one JSON object")
	}
	if _, err := decoder.Token(); err != io.EOF {
		if err == nil {
			return 0, fmt.Errorf("multiple JSON values")
		}
		return 0, err
	}
	if !seenJobID || jobID == 0 {
		return 0, fmt.Errorf("jobId is required")
	}
	return jobID, nil
}
