package mysqlstore

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	"github.com/chenbb0128/weavepress/server/internal/modules/aiwriting"
	"github.com/chenbb0128/weavepress/server/internal/modules/editorial"
	"github.com/chenbb0128/weavepress/server/internal/modules/workspace"
)

const (
	aiJobColumns = `id, job_type, article_id, parent_job_id, requested_by, status, attempts,
		manual_retries, reuse_key, idempotency_hash, input_fingerprint, provider, model,
		prompt_version, input_tokens, output_tokens, total_tokens, error_code, error_message,
		retryable, started_at, finished_at, created_at, updated_at`
	aiAnalysisColumns = `id, job_id, article_id, summary, facts_json, viewpoints_json,
		quotes_json, risks_json, angles_json, created_at`
	aiGenerationColumns = `id, job_id, analysis_id, angle_id, audience, tone, target_words,
		additional_instructions, title, digest, blocks_json, fact_map_json, content_html,
		draft_id, created_at, updated_at`
)

// AIStore is separate from Store because the collection and AI domains expose
// several same-named methods with different Go signatures.
type AIStore struct{ db *sql.DB }

func NewAIStore(db *sql.DB) *AIStore { return &AIStore{db: db} }

var _ aiwriting.Store = (*AIStore)(nil)

func (s *AIStore) CreateAnalysisJob(ctx context.Context, input aiwriting.CreateAnalysisJobInput) (aiwriting.Job, bool, error) {
	material := []byte("analysis\x00" + strconv.FormatUint(input.ArticleID, 10) + "\x00" + input.PromptVersion + "\x00")
	material = append(material, input.InputFingerprint[:]...)
	reuseKey := sha256.Sum256(material)
	var storedReuseKey any = reuseKey[:]
	if input.Force {
		storedReuseKey = nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return aiwriting.Job{}, false, err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `INSERT INTO ai_jobs
		(job_type, article_id, parent_job_id, requested_by, status, reuse_key, idempotency_hash,
		 input_fingerprint, provider, model, prompt_version)
		VALUES ('analysis', ?, NULL, ?, 'queued', ?, NULL, ?, ?, ?, ?)`,
		input.ArticleID, input.RequestedBy, storedReuseKey, input.InputFingerprint[:], input.Provider, input.Model, input.PromptVersion)
	if err != nil {
		if duplicate(err) && !input.Force {
			_ = tx.Rollback()
			job, findErr := getAIJobByReuseKey(ctx, s.db, reuseKey)
			return job, true, findErr
		}
		return aiwriting.Job{}, false, err
	}
	jobID, err := result.LastInsertId()
	if err != nil {
		return aiwriting.Job{}, false, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO ai_job_events (job_id, status, message) VALUES (?, 'queued', 'AI 分析任务已创建')`, jobID); err != nil {
		return aiwriting.Job{}, false, err
	}
	if err = tx.Commit(); err != nil {
		return aiwriting.Job{}, false, err
	}
	job, err := s.GetJob(ctx, uint64(jobID), false)
	return job, false, err
}

func (s *AIStore) CreateGenerationJob(ctx context.Context, input aiwriting.CreateGenerationJobInput) (aiwriting.Generation, aiwriting.Job, bool, error) {
	idempotencyMaterial := strconv.FormatUint(input.RequestedBy, 10) + "\x00" + input.Params.IdempotencyKey
	idempotencyHash := sha256.Sum256([]byte(idempotencyMaterial))
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return aiwriting.Generation{}, aiwriting.Job{}, false, err
	}
	defer tx.Rollback()

	existingJob, err := getAIJobByIdempotency(ctx, tx, input.RequestedBy, idempotencyHash)
	if err == nil {
		existingGeneration, findErr := getAIGenerationByJobID(ctx, tx, existingJob.ID)
		if findErr != nil {
			return aiwriting.Generation{}, aiwriting.Job{}, true, findErr
		}
		if err = tx.Commit(); err != nil {
			return aiwriting.Generation{}, aiwriting.Job{}, true, err
		}
		return existingGeneration, existingJob, true, nil
	}
	if !errors.Is(err, workspace.ErrNotFound) {
		return aiwriting.Generation{}, aiwriting.Job{}, false, err
	}

	var analysisJobID, articleID uint64
	var analysisStatus string
	err = tx.QueryRowContext(ctx, `SELECT a.job_id, a.article_id, j.status
		FROM ai_analyses a JOIN ai_jobs j ON j.id = a.job_id
		WHERE a.id = ? FOR UPDATE`, input.AnalysisID).Scan(&analysisJobID, &articleID, &analysisStatus)
	if errors.Is(err, sql.ErrNoRows) {
		return aiwriting.Generation{}, aiwriting.Job{}, false, workspace.ErrNotFound
	}
	if err != nil {
		return aiwriting.Generation{}, aiwriting.Job{}, false, err
	}
	if analysisStatus != aiwriting.JobCompleted {
		return aiwriting.Generation{}, aiwriting.Job{}, false, aiwriting.ErrAnalysisNotReady
	}

	jobResult, err := tx.ExecContext(ctx, `INSERT INTO ai_jobs
		(job_type, article_id, parent_job_id, requested_by, status, reuse_key, idempotency_hash,
		 input_fingerprint, provider, model, prompt_version)
		VALUES ('generation', ?, ?, ?, 'queued', NULL, ?, ?, ?, ?, ?)`,
		articleID, analysisJobID, input.RequestedBy, idempotencyHash[:], input.InputFingerprint[:], input.Provider, input.Model, input.PromptVersion)
	if err != nil {
		if duplicate(err) {
			_ = tx.Rollback()
			return s.findGenerationByIdempotency(ctx, input.RequestedBy, idempotencyHash)
		}
		return aiwriting.Generation{}, aiwriting.Job{}, false, err
	}
	jobID, err := jobResult.LastInsertId()
	if err != nil {
		return aiwriting.Generation{}, aiwriting.Job{}, false, err
	}
	generationResult, err := tx.ExecContext(ctx, `INSERT INTO ai_generations
		(job_id, analysis_id, angle_id, audience, tone, target_words, additional_instructions,
		 title, digest, blocks_json, fact_map_json, content_html)
		VALUES (?, ?, ?, ?, ?, ?, ?, '', '', JSON_ARRAY(), JSON_OBJECT(), '')`,
		jobID, input.AnalysisID, input.Params.AngleID, input.Params.Audience, input.Params.Tone,
		input.Params.TargetWords, input.Params.AdditionalInstructions)
	if err != nil {
		return aiwriting.Generation{}, aiwriting.Job{}, false, err
	}
	generationID, err := generationResult.LastInsertId()
	if err != nil {
		return aiwriting.Generation{}, aiwriting.Job{}, false, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO ai_job_events (job_id, status, message) VALUES (?, 'queued', 'AI 稿件生成任务已创建')`, jobID); err != nil {
		return aiwriting.Generation{}, aiwriting.Job{}, false, err
	}
	if err = tx.Commit(); err != nil {
		return aiwriting.Generation{}, aiwriting.Job{}, false, err
	}
	generation, err := s.GetGeneration(ctx, uint64(generationID))
	if err != nil {
		return aiwriting.Generation{}, aiwriting.Job{}, false, err
	}
	return generation, *generation.Job, false, nil
}

func (s *AIStore) findGenerationByIdempotency(ctx context.Context, userID uint64, hash [32]byte) (aiwriting.Generation, aiwriting.Job, bool, error) {
	job, err := getAIJobByIdempotency(ctx, s.db, userID, hash)
	if err != nil {
		return aiwriting.Generation{}, aiwriting.Job{}, true, err
	}
	generation, err := s.GetGenerationByJobID(ctx, job.ID)
	if err != nil {
		return aiwriting.Generation{}, aiwriting.Job{}, true, err
	}
	return generation, job, true, nil
}

func (s *AIStore) GetJob(ctx context.Context, id uint64, withDetails bool) (aiwriting.Job, error) {
	job, err := getAIJobByID(ctx, s.db, id)
	if err != nil {
		return job, err
	}
	if !withDetails {
		return job, nil
	}
	article, err := New(s.db).GetArticle(ctx, job.ArticleID)
	if err != nil {
		return job, err
	}
	job.Article = &article
	job.Events, err = queryAIJobEvents(ctx, s.db, job.ID)
	return job, err
}

func (s *AIStore) ListJobs(ctx context.Context, filter aiwriting.JobFilter, page, pageSize int) (aiwriting.Page[aiwriting.Job], error) {
	where, args := ` WHERE 1=1`, []any{}
	if filter.Type != "" {
		where += ` AND job_type = ?`
		args = append(args, filter.Type)
	}
	if filter.Status != "" {
		where += ` AND status = ?`
		args = append(args, filter.Status)
	}
	if filter.ArticleID != 0 {
		where += ` AND article_id = ?`
		args = append(args, filter.ArticleID)
	}
	var total int64
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM ai_jobs`+where, args...).Scan(&total); err != nil {
		return aiwriting.Page[aiwriting.Job]{}, err
	}
	queryArgs := append(append([]any{}, args...), pageSize, (page-1)*pageSize)
	rows, err := s.db.QueryContext(ctx, `SELECT `+aiJobColumns+` FROM ai_jobs`+where+` ORDER BY created_at DESC, id DESC LIMIT ? OFFSET ?`, queryArgs...)
	if err != nil {
		return aiwriting.Page[aiwriting.Job]{}, err
	}
	items := make([]aiwriting.Job, 0)
	for rows.Next() {
		job, scanErr := scanAIJob(rows)
		if scanErr != nil {
			rows.Close()
			return aiwriting.Page[aiwriting.Job]{}, scanErr
		}
		items = append(items, job)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return aiwriting.Page[aiwriting.Job]{}, err
	}
	if err = rows.Close(); err != nil {
		return aiwriting.Page[aiwriting.Job]{}, err
	}
	for index := range items {
		var article workspace.Article
		err = s.db.QueryRowContext(ctx, `SELECT id, title, source_name FROM articles WHERE id = ?`, items[index].ArticleID).Scan(&article.ID, &article.Title, &article.SourceName)
		if errors.Is(err, sql.ErrNoRows) {
			return aiwriting.Page[aiwriting.Job]{}, workspace.ErrNotFound
		}
		if err != nil {
			return aiwriting.Page[aiwriting.Job]{}, err
		}
		items[index].Article = &article
	}
	return aiwriting.Page[aiwriting.Job]{Items: items, Total: total, Page: page, PageSize: pageSize}, nil
}

func (s *AIStore) GetAnalysis(ctx context.Context, id uint64) (aiwriting.Analysis, error) {
	analysis, err := getAIAnalysisByID(ctx, s.db, id)
	if err != nil {
		return analysis, err
	}
	job, err := s.GetJob(ctx, analysis.JobID, true)
	if err != nil {
		return analysis, err
	}
	analysis.Job = &job
	return analysis, nil
}

func (s *AIStore) ListAnalyses(ctx context.Context, articleID uint64, page, pageSize int) (aiwriting.Page[aiwriting.Analysis], error) {
	var total int64
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM ai_analyses WHERE article_id = ?`, articleID).Scan(&total); err != nil {
		return aiwriting.Page[aiwriting.Analysis]{}, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+aiAnalysisColumns+` FROM ai_analyses WHERE article_id = ? ORDER BY created_at DESC, id DESC LIMIT ? OFFSET ?`, articleID, pageSize, (page-1)*pageSize)
	if err != nil {
		return aiwriting.Page[aiwriting.Analysis]{}, err
	}
	defer rows.Close()
	items := make([]aiwriting.Analysis, 0)
	for rows.Next() {
		analysis, scanErr := scanAIAnalysis(rows)
		if scanErr != nil {
			return aiwriting.Page[aiwriting.Analysis]{}, scanErr
		}
		items = append(items, analysis)
	}
	return aiwriting.Page[aiwriting.Analysis]{Items: items, Total: total, Page: page, PageSize: pageSize}, rows.Err()
}

func (s *AIStore) GetGeneration(ctx context.Context, id uint64) (aiwriting.Generation, error) {
	generation, err := getAIGenerationByID(ctx, s.db, id)
	if err != nil {
		return generation, err
	}
	job, err := s.GetJob(ctx, generation.JobID, true)
	if err != nil {
		return generation, err
	}
	generation.Job = &job
	return generation, nil
}

func (s *AIStore) GetGenerationByJobID(ctx context.Context, jobID uint64) (aiwriting.Generation, error) {
	generation, err := getAIGenerationByJobID(ctx, s.db, jobID)
	if err != nil {
		return generation, err
	}
	job, err := s.GetJob(ctx, generation.JobID, true)
	if err != nil {
		return generation, err
	}
	generation.Job = &job
	return generation, nil
}

func (s *AIStore) SetJobRunning(ctx context.Context, id uint64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var status string
	err = tx.QueryRowContext(ctx, `SELECT status FROM ai_jobs WHERE id = ? FOR UPDATE`, id).Scan(&status)
	if errors.Is(err, sql.ErrNoRows) {
		return workspace.ErrNotFound
	}
	if err != nil {
		return err
	}
	if status == aiwriting.JobCompleted {
		return tx.Commit()
	}
	message := "AI 任务开始执行"
	if status == aiwriting.JobRunning {
		message = "AI 任务恢复执行"
	} else if status != aiwriting.JobQueued {
		return workspace.ErrJobStateConflict
	}
	if _, err = tx.ExecContext(ctx, `UPDATE ai_jobs SET status = 'running', attempts = attempts + 1,
		started_at = COALESCE(started_at, UTC_TIMESTAMP(3)), error_code = '', error_message = '', finished_at = NULL
		WHERE id = ?`, id); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO ai_job_events (job_id, status, message) VALUES (?, 'running', ?)`, id, message); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *AIStore) CompleteAnalysis(ctx context.Context, jobID uint64, output aiwriting.AnalysisOutput, usage aiwriting.TokenUsage) (aiwriting.Analysis, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return aiwriting.Analysis{}, err
	}
	defer tx.Rollback()
	job, err := getAIJobForUpdate(ctx, tx, jobID)
	if err != nil {
		return aiwriting.Analysis{}, err
	}
	if job.Status == aiwriting.JobCompleted {
		existing, findErr := getAIAnalysisByJobID(ctx, tx, jobID)
		if findErr != nil {
			return aiwriting.Analysis{}, findErr
		}
		if err = tx.Commit(); err != nil {
			return aiwriting.Analysis{}, err
		}
		return s.GetAnalysis(ctx, existing.ID)
	}
	if job.Type != aiwriting.JobTypeAnalysis || job.Status != aiwriting.JobRunning {
		return aiwriting.Analysis{}, workspace.ErrJobStateConflict
	}
	factsJSON, err := json.Marshal(output.Facts)
	if err != nil {
		return aiwriting.Analysis{}, err
	}
	viewpointsJSON, err := json.Marshal(output.Viewpoints)
	if err != nil {
		return aiwriting.Analysis{}, err
	}
	quotesJSON, err := json.Marshal(output.Quotes)
	if err != nil {
		return aiwriting.Analysis{}, err
	}
	risksJSON, err := json.Marshal(output.Risks)
	if err != nil {
		return aiwriting.Analysis{}, err
	}
	anglesJSON, err := json.Marshal(output.Angles)
	if err != nil {
		return aiwriting.Analysis{}, err
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO ai_analyses
		(job_id, article_id, summary, facts_json, viewpoints_json, quotes_json, risks_json, angles_json)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, jobID, job.ArticleID, output.Summary, factsJSON, viewpointsJSON, quotesJSON, risksJSON, anglesJSON)
	if err != nil {
		if duplicate(err) {
			existing, findErr := getAIAnalysisByJobID(ctx, tx, jobID)
			if findErr != nil {
				return aiwriting.Analysis{}, findErr
			}
			if err = tx.Commit(); err != nil {
				return aiwriting.Analysis{}, err
			}
			return s.GetAnalysis(ctx, existing.ID)
		}
		return aiwriting.Analysis{}, err
	}
	analysisID, err := result.LastInsertId()
	if err != nil {
		return aiwriting.Analysis{}, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE ai_jobs SET status = 'completed',
		input_tokens = input_tokens + ?, output_tokens = output_tokens + ?, total_tokens = total_tokens + ?,
		error_code = '', error_message = '', retryable = FALSE, finished_at = UTC_TIMESTAMP(3)
		WHERE id = ?`, usage.InputTokens, usage.OutputTokens, usage.TotalTokens, jobID); err != nil {
		return aiwriting.Analysis{}, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO ai_job_events (job_id, status, message) VALUES (?, 'completed', 'AI 分析任务已完成')`, jobID); err != nil {
		return aiwriting.Analysis{}, err
	}
	if err = tx.Commit(); err != nil {
		return aiwriting.Analysis{}, err
	}
	return s.GetAnalysis(ctx, uint64(analysisID))
}

func (s *AIStore) CompleteGeneration(ctx context.Context, jobID uint64, output aiwriting.GenerationOutput, draftInput editorial.GeneratedDraftInput, usage aiwriting.TokenUsage) (aiwriting.Generation, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return aiwriting.Generation{}, err
	}
	defer tx.Rollback()
	job, err := getAIJobForUpdate(ctx, tx, jobID)
	if err != nil {
		return aiwriting.Generation{}, err
	}
	generation, err := getAIGenerationByJobIDForUpdate(ctx, tx, jobID)
	if err != nil {
		return aiwriting.Generation{}, err
	}
	if generation.DraftID != nil || job.Status == aiwriting.JobCompleted {
		if err = tx.Commit(); err != nil {
			return aiwriting.Generation{}, err
		}
		return s.GetGeneration(ctx, generation.ID)
	}
	if job.Type != aiwriting.JobTypeGeneration || job.Status != aiwriting.JobRunning {
		return aiwriting.Generation{}, workspace.ErrJobStateConflict
	}
	if draftInput.SourceArticleID != job.ArticleID || draftInput.CreatedBy != job.RequestedBy {
		return aiwriting.Generation{}, aiwriting.ErrInvalidParameters
	}
	draftResult, err := tx.ExecContext(ctx, `INSERT INTO drafts
		(source_article_id, title, author, digest, content_html, cover_asset_id, status,
		 current_version, created_by, updated_by)
		VALUES (?, ?, '', ?, ?, ?, 'editing', 1, ?, ?)`,
		draftInput.SourceArticleID, draftInput.Title, draftInput.Digest, draftInput.ContentHTML,
		nullableID(draftInput.CoverAssetID), draftInput.CreatedBy, draftInput.CreatedBy)
	if err != nil {
		return aiwriting.Generation{}, err
	}
	draftID, err := draftResult.LastInsertId()
	if err != nil {
		return aiwriting.Generation{}, err
	}
	const changeNote = "AI 合规采编生成"
	if _, err = tx.ExecContext(ctx, `INSERT INTO draft_versions
		(draft_id, version, title, author, digest, content_html, cover_asset_id, change_note, created_by)
		VALUES (?, 1, ?, '', ?, ?, ?, ?, ?)`, draftID, draftInput.Title, draftInput.Digest,
		draftInput.ContentHTML, nullableID(draftInput.CoverAssetID), changeNote, draftInput.CreatedBy); err != nil {
		return aiwriting.Generation{}, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO draft_events
		(draft_id, actor_id, from_status, to_status, note) VALUES (?, ?, '', 'editing', ?)`,
		draftID, draftInput.CreatedBy, changeNote); err != nil {
		return aiwriting.Generation{}, err
	}
	blocksJSON, err := json.Marshal(output.Blocks)
	if err != nil {
		return aiwriting.Generation{}, err
	}
	factMap := buildFactMap(output.Blocks)
	factMapJSON, err := json.Marshal(factMap)
	if err != nil {
		return aiwriting.Generation{}, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE ai_generations SET title = ?, digest = ?, blocks_json = ?,
		fact_map_json = ?, content_html = ?, draft_id = ? WHERE id = ?`,
		output.Title, output.Digest, blocksJSON, factMapJSON, draftInput.ContentHTML, draftID, generation.ID); err != nil {
		return aiwriting.Generation{}, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE ai_jobs SET status = 'completed',
		input_tokens = input_tokens + ?, output_tokens = output_tokens + ?, total_tokens = total_tokens + ?,
		error_code = '', error_message = '', retryable = FALSE, finished_at = UTC_TIMESTAMP(3)
		WHERE id = ?`, usage.InputTokens, usage.OutputTokens, usage.TotalTokens, jobID); err != nil {
		return aiwriting.Generation{}, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO ai_job_events (job_id, status, message) VALUES (?, 'completed', 'AI 稿件生成任务已完成')`, jobID); err != nil {
		return aiwriting.Generation{}, err
	}
	if err = tx.Commit(); err != nil {
		return aiwriting.Generation{}, err
	}
	return s.GetGeneration(ctx, generation.ID)
}

func (s *AIStore) SetJobFailure(ctx context.Context, id uint64, code, message string, retrying bool, usage aiwriting.TokenUsage) error {
	status, retryable, eventMessage := aiwriting.JobFailed, false, message
	if retrying {
		status, retryable, eventMessage = aiwriting.JobQueued, true, "临时错误，等待自动重试："+message
	}
	code = truncateRunes(code, 64)
	message = truncateRunes(message, 1024)
	eventMessage = truncateRunes(eventMessage, 1024)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE ai_jobs SET status = ?, error_code = ?, error_message = ?, retryable = ?,
		input_tokens = input_tokens + ?, output_tokens = output_tokens + ?, total_tokens = total_tokens + ?,
		finished_at = IF(? = 'failed', UTC_TIMESTAMP(3), NULL)
		WHERE id = ? AND status IN ('queued', 'running')`, status, code, message, retryable,
		usage.InputTokens, usage.OutputTokens, usage.TotalTokens, status, id)
	if err != nil {
		return err
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return tx.Commit()
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO ai_job_events (job_id, status, message) VALUES (?, ?, ?)`, id, status, eventMessage); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *AIStore) AddJobEvent(ctx context.Context, jobID uint64, status, message string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO ai_job_events (job_id, status, message) VALUES (?, ?, ?)`, jobID, status, truncateRunes(message, 1024))
	return err
}

func (s *AIStore) RetryJob(ctx context.Context, id, requestedBy uint64) (aiwriting.Job, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return aiwriting.Job{}, err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE ai_jobs SET status = 'queued',
		manual_retries = manual_retries + 1, error_code = '', error_message = '', finished_at = NULL
		WHERE id = ? AND status = 'failed' AND retryable = TRUE`, id)
	if err != nil {
		return aiwriting.Job{}, err
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return aiwriting.Job{}, aiwriting.ErrJobNotRetryable
	}
	retryMessage := truncateRunes(fmt.Sprintf("用户 %d 手动重试 AI 任务", requestedBy), 1024)
	if _, err = tx.ExecContext(ctx, `INSERT INTO ai_job_events (job_id, status, message) VALUES (?, 'queued', ?)`, id, retryMessage); err != nil {
		return aiwriting.Job{}, err
	}
	if err = tx.Commit(); err != nil {
		return aiwriting.Job{}, err
	}
	return s.GetJob(ctx, id, false)
}

type aiQueryRower interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

type aiEventQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func getAIJobByID(ctx context.Context, db aiQueryRower, id uint64) (aiwriting.Job, error) {
	return scanAIJob(db.QueryRowContext(ctx, `SELECT `+aiJobColumns+` FROM ai_jobs WHERE id = ?`, id))
}

func getAIJobForUpdate(ctx context.Context, tx *sql.Tx, id uint64) (aiwriting.Job, error) {
	return scanAIJob(tx.QueryRowContext(ctx, `SELECT `+aiJobColumns+` FROM ai_jobs WHERE id = ? FOR UPDATE`, id))
}

func getAIJobByReuseKey(ctx context.Context, db aiQueryRower, key [32]byte) (aiwriting.Job, error) {
	return scanAIJob(db.QueryRowContext(ctx, `SELECT `+aiJobColumns+` FROM ai_jobs WHERE reuse_key = ?`, key[:]))
}

func getAIJobByIdempotency(ctx context.Context, db aiQueryRower, userID uint64, hash [32]byte) (aiwriting.Job, error) {
	return scanAIJob(db.QueryRowContext(ctx, `SELECT `+aiJobColumns+` FROM ai_jobs WHERE requested_by = ? AND idempotency_hash = ?`, userID, hash[:]))
}

func scanAIJob(row scanner) (aiwriting.Job, error) {
	var job aiwriting.Job
	var parent sql.NullInt64
	var reuseKey, idempotencyHash, fingerprint []byte
	var started, finished sql.NullTime
	err := row.Scan(&job.ID, &job.Type, &job.ArticleID, &parent, &job.RequestedBy, &job.Status,
		&job.Attempts, &job.ManualRetries, &reuseKey, &idempotencyHash, &fingerprint, &job.Provider,
		&job.Model, &job.PromptVersion, &job.InputTokens, &job.OutputTokens, &job.TotalTokens,
		&job.ErrorCode, &job.ErrorMessage, &job.Retryable, &started, &finished, &job.CreatedAt, &job.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return job, workspace.ErrNotFound
	}
	if err != nil {
		return job, err
	}
	if parent.Valid {
		value := uint64(parent.Int64)
		job.ParentJobID = &value
	}
	if err := validateBinary32("reuse_key", reuseKey, true); err != nil {
		return job, err
	}
	if err := validateBinary32("idempotency_hash", idempotencyHash, true); err != nil {
		return job, err
	}
	if err := validateBinary32("input_fingerprint", fingerprint, false); err != nil {
		return job, err
	}
	copy(job.InputFingerprint[:], fingerprint)
	if started.Valid {
		job.StartedAt = &started.Time
	}
	if finished.Valid {
		job.FinishedAt = &finished.Time
	}
	return job, nil
}

func queryAIJobEvents(ctx context.Context, db aiEventQueryer, jobID uint64) ([]aiwriting.JobEvent, error) {
	rows, err := db.QueryContext(ctx, `SELECT id, job_id, status, message, created_at FROM ai_job_events WHERE job_id = ? ORDER BY id ASC`, jobID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	events := make([]aiwriting.JobEvent, 0)
	for rows.Next() {
		var event aiwriting.JobEvent
		if err := rows.Scan(&event.ID, &event.JobID, &event.Status, &event.Message, &event.CreatedAt); err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	return events, rows.Err()
}

func getAIAnalysisByID(ctx context.Context, db aiQueryRower, id uint64) (aiwriting.Analysis, error) {
	return scanAIAnalysis(db.QueryRowContext(ctx, `SELECT `+aiAnalysisColumns+` FROM ai_analyses WHERE id = ?`, id))
}

func getAIAnalysisByJobID(ctx context.Context, db aiQueryRower, jobID uint64) (aiwriting.Analysis, error) {
	return scanAIAnalysis(db.QueryRowContext(ctx, `SELECT `+aiAnalysisColumns+` FROM ai_analyses WHERE job_id = ?`, jobID))
}

func scanAIAnalysis(row scanner) (aiwriting.Analysis, error) {
	var analysis aiwriting.Analysis
	var factsJSON, viewpointsJSON, quotesJSON, risksJSON, anglesJSON []byte
	err := row.Scan(&analysis.ID, &analysis.JobID, &analysis.ArticleID, &analysis.Summary,
		&factsJSON, &viewpointsJSON, &quotesJSON, &risksJSON, &anglesJSON, &analysis.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return analysis, workspace.ErrNotFound
	}
	if err != nil {
		return analysis, err
	}
	if err = json.Unmarshal(factsJSON, &analysis.Facts); err != nil {
		return analysis, err
	}
	if err = json.Unmarshal(viewpointsJSON, &analysis.Viewpoints); err != nil {
		return analysis, err
	}
	if err = json.Unmarshal(quotesJSON, &analysis.Quotes); err != nil {
		return analysis, err
	}
	if err = json.Unmarshal(risksJSON, &analysis.Risks); err != nil {
		return analysis, err
	}
	if err = json.Unmarshal(anglesJSON, &analysis.Angles); err != nil {
		return analysis, err
	}
	return analysis, nil
}

func getAIGenerationByID(ctx context.Context, db aiQueryRower, id uint64) (aiwriting.Generation, error) {
	return scanAIGeneration(db.QueryRowContext(ctx, `SELECT `+aiGenerationColumns+` FROM ai_generations WHERE id = ?`, id))
}

func getAIGenerationByJobID(ctx context.Context, db aiQueryRower, jobID uint64) (aiwriting.Generation, error) {
	return scanAIGeneration(db.QueryRowContext(ctx, `SELECT `+aiGenerationColumns+` FROM ai_generations WHERE job_id = ?`, jobID))
}

func getAIGenerationByJobIDForUpdate(ctx context.Context, tx *sql.Tx, jobID uint64) (aiwriting.Generation, error) {
	return scanAIGeneration(tx.QueryRowContext(ctx, `SELECT `+aiGenerationColumns+` FROM ai_generations WHERE job_id = ? FOR UPDATE`, jobID))
}

func scanAIGeneration(row scanner) (aiwriting.Generation, error) {
	var generation aiwriting.Generation
	var blocksJSON, factMapJSON []byte
	var draftID sql.NullInt64
	err := row.Scan(&generation.ID, &generation.JobID, &generation.AnalysisID, &generation.AngleID,
		&generation.Audience, &generation.Tone, &generation.TargetWords, &generation.AdditionalInstructions,
		&generation.Title, &generation.Digest, &blocksJSON, &factMapJSON, &generation.ContentHTML,
		&draftID, &generation.CreatedAt, &generation.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return generation, workspace.ErrNotFound
	}
	if err != nil {
		return generation, err
	}
	if err = json.Unmarshal(blocksJSON, &generation.Blocks); err != nil {
		return generation, err
	}
	if err = json.Unmarshal(factMapJSON, &generation.FactMap); err != nil {
		return generation, err
	}
	if draftID.Valid {
		value := uint64(draftID.Int64)
		generation.DraftID = &value
	}
	return generation, nil
}

func buildFactMap(blocks []aiwriting.GeneratedBlock) map[string][]string {
	result := make(map[string][]string)
	for index, block := range blocks {
		seen := make(map[string]struct{}, len(block.FactIDs))
		for _, factID := range block.FactIDs {
			if _, exists := seen[factID]; exists {
				continue
			}
			seen[factID] = struct{}{}
			result[strconv.Itoa(index+1)] = append(result[strconv.Itoa(index+1)], factID)
		}
	}
	return result
}

func validateBinary32(name string, value []byte, nullable bool) error {
	if value == nil && nullable {
		return nil
	}
	if len(value) != sha256.Size {
		return fmt.Errorf("invalid %s length: got %d, want %d", name, len(value), sha256.Size)
	}
	return nil
}

func truncateRunes(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
}
