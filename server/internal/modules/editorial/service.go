package editorial

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/hibiken/asynq"

	"github.com/chenbb0128/weavepress/server/internal/platform/queue"
)

const TaskPublishWeChat = "publishing:wechat:draft"

type Service struct {
	store     Store
	articles  ArticleStore
	queue     *queue.Client
	publisher Publisher
	enabled   bool
}

func New(store Store, articles ArticleStore, queueClient *queue.Client, publisher Publisher, enabled bool) *Service {
	return &Service{store: store, articles: articles, queue: queueClient, publisher: publisher, enabled: enabled}
}

func (s *Service) CreateFromArticle(ctx context.Context, articleID, userID uint64) (Draft, error) {
	article, err := s.articles.GetArticle(ctx, articleID)
	if err != nil {
		return Draft{}, err
	}
	if article.Status != "ready" {
		return Draft{}, ErrDraftStateConflict
	}
	var coverID *uint64
	for _, asset := range article.Assets {
		if asset.DownloadStatus != "completed" {
			continue
		}
		id := asset.ID
		if coverID == nil || asset.IsCover {
			coverID = &id
		}
		if asset.IsCover {
			break
		}
	}
	return s.store.CreateDraft(ctx, article.ID, userID, truncate(article.Title, 255), truncate(article.Author, 64), truncate(article.PlainText, 120), RenderArticle(article), coverID)
}

func (s *Service) Get(ctx context.Context, id uint64) (Draft, error) {
	return s.store.GetDraft(ctx, id, true)
}

func (s *Service) List(ctx context.Context, keyword, status string, page, pageSize int) (Page[Draft], error) {
	return s.store.ListDrafts(ctx, strings.TrimSpace(keyword), strings.TrimSpace(status), page, pageSize)
}

func (s *Service) Versions(ctx context.Context, id uint64) ([]DraftVersion, error) {
	return s.store.ListDraftVersions(ctx, id)
}

func (s *Service) RestoreVersion(ctx context.Context, id, userID uint64, version, expectedVersion uint) (Draft, error) {
	if version == 0 || expectedVersion == 0 {
		return Draft{}, ErrDraftVersionConflict
	}
	historical, err := s.store.GetDraftVersion(ctx, id, version)
	if err != nil {
		return Draft{}, err
	}
	if err := s.validateAssets(ctx, id, historical.ContentHTML, historical.CoverAssetID); err != nil {
		return Draft{}, err
	}
	return s.store.RestoreDraftVersion(ctx, id, userID, version, expectedVersion)
}

func (s *Service) Preflight(ctx context.Context, id uint64) (PreflightResult, error) {
	draft, err := s.store.GetDraft(ctx, id, true)
	if err != nil {
		return PreflightResult{}, err
	}
	return ValidateWeChatDraft(draft), nil
}

func (s *Service) Update(ctx context.Context, id, userID uint64, input UpdateInput) (Draft, error) {
	input.Title = strings.TrimSpace(input.Title)
	input.Author = strings.TrimSpace(input.Author)
	input.Digest = strings.TrimSpace(input.Digest)
	input.ChangeNote = strings.TrimSpace(input.ChangeNote)
	input.ContentHTML = SanitizeHTML(input.ContentHTML)
	if input.Title == "" || utf8.RuneCountInString(input.Title) > 255 || utf8.RuneCountInString(input.Author) > 64 || utf8.RuneCountInString(input.Digest) > 255 || input.ContentHTML == "" || input.ExpectedVersion == 0 {
		return Draft{}, ErrDraftStateConflict
	}
	if err := s.validateAssets(ctx, id, input.ContentHTML, input.CoverAssetID); err != nil {
		return Draft{}, err
	}
	return s.store.UpdateDraft(ctx, id, userID, input)
}

func (s *Service) SubmitReview(ctx context.Context, id, userID uint64) (Draft, error) {
	return s.store.SetDraftStatus(ctx, id, userID, StatusEditing, StatusInReview, "提交审核")
}

func (s *Service) Review(ctx context.Context, id, userID uint64, approved bool, note string) (Draft, error) {
	target, action := StatusEditing, "审核退回"
	if approved {
		target, action = StatusApproved, "审核通过"
	}
	if note = strings.TrimSpace(note); note != "" {
		action += "：" + truncate(note, 200)
	}
	return s.store.SetDraftStatus(ctx, id, userID, StatusInReview, target, action)
}

func (s *Service) Publish(ctx context.Context, id, userID uint64) (PublishJob, error) {
	if !s.enabled {
		return PublishJob{}, ErrPublisherDisabled
	}
	draft, err := s.store.GetDraft(ctx, id, true)
	if err != nil {
		return PublishJob{}, err
	}
	preflight := ValidateWeChatDraft(draft)
	if !preflight.Valid {
		return PublishJob{}, &PreflightError{Result: preflight}
	}
	job, err := s.store.CreatePublishJob(ctx, id, userID)
	if err != nil {
		return PublishJob{}, err
	}
	if err := s.enqueue(job); err != nil {
		_ = s.store.SetPublishJobFailure(ctx, job.ID, "QUEUE_UNAVAILABLE", "发布队列暂不可用", false)
		return PublishJob{}, err
	}
	return job, nil
}

func (s *Service) RetryPublish(ctx context.Context, id, userID uint64) (PublishJob, error) {
	if !s.enabled {
		return PublishJob{}, ErrPublisherDisabled
	}
	job, err := s.store.RetryPublishJob(ctx, id, userID)
	if err != nil {
		return PublishJob{}, err
	}
	if err := s.enqueue(job); err != nil {
		_ = s.store.SetPublishJobFailure(ctx, job.ID, "QUEUE_UNAVAILABLE", "发布队列暂不可用", false)
		return PublishJob{}, err
	}
	return job, nil
}

func (s *Service) PublishJobs(ctx context.Context, status string, page, pageSize int) (Page[PublishJob], error) {
	return s.store.ListPublishJobs(ctx, strings.TrimSpace(status), page, pageSize)
}

func (s *Service) PublishJob(ctx context.Context, id uint64) (PublishJob, error) {
	return s.store.GetPublishJob(ctx, id, true)
}

func (s *Service) PublishingEnabled() bool { return s.enabled }

func (s *Service) enqueue(job PublishJob) error {
	if s.queue == nil || s.queue.Asynq == nil {
		return fmt.Errorf("publish queue unavailable")
	}
	payload, err := json.Marshal(map[string]uint64{"jobId": job.ID})
	if err != nil {
		return err
	}
	taskID := fmt.Sprintf("wechat-publish:%d:%d:%d", job.ID, job.Attempts, job.ManualRetries)
	_, err = s.queue.Asynq.Enqueue(asynq.NewTask(TaskPublishWeChat, payload), asynq.Queue("publishing"), asynq.TaskID(taskID), asynq.MaxRetry(3), asynq.Timeout(15*time.Minute))
	return err
}

func (s *Service) HandleTask(ctx context.Context, task *asynq.Task) error {
	var payload struct {
		JobID uint64 `json:"jobId"`
	}
	if err := json.Unmarshal(task.Payload(), &payload); err != nil || payload.JobID == 0 {
		return fmt.Errorf("%w: invalid publish payload", asynq.SkipRetry)
	}
	err := s.process(ctx, payload.JobID)
	if err == nil || errors.Is(err, ErrDraftStateConflict) {
		return nil
	}
	code, message, retryable := classifyPublish(err)
	retryCount, _ := asynq.GetRetryCount(ctx)
	maxRetry, _ := asynq.GetMaxRetry(ctx)
	willRetry := retryable && retryCount < maxRetry
	_ = s.store.SetPublishJobFailure(ctx, payload.JobID, code, message, willRetry)
	if !willRetry {
		return fmt.Errorf("%w: %s", asynq.SkipRetry, message)
	}
	return err
}

func (s *Service) process(ctx context.Context, jobID uint64) error {
	if s.publisher == nil {
		return ErrPublisherDisabled
	}
	job, err := s.store.GetPublishJob(ctx, jobID, false)
	if err != nil {
		return err
	}
	if job.Status != PublishQueued && job.Status != PublishPublishing {
		return nil
	}
	if job.Status == PublishQueued {
		if err := s.store.SetPublishJobPublishing(ctx, jobID); err != nil {
			return err
		}
	}
	draft, err := s.store.GetDraft(ctx, job.DraftID, true)
	if err != nil {
		return err
	}
	result, err := s.publisher.Publish(ctx, draft)
	if err != nil {
		return err
	}
	return s.store.CompletePublishJob(ctx, jobID, result.RemoteMediaID)
}

func (s *Service) validateAssets(ctx context.Context, draftID uint64, contentHTML string, coverID *uint64) error {
	draft, err := s.store.GetDraft(ctx, draftID, false)
	if err != nil {
		return err
	}
	ids, err := ReferencedAssetIDs(contentHTML)
	if err != nil {
		return ErrDraftStateConflict
	}
	if coverID != nil {
		ids = append(ids, *coverID)
	}
	for _, id := range ids {
		asset, assetErr := s.articles.GetAsset(ctx, id)
		if assetErr != nil {
			return assetErr
		}
		if asset.ArticleID != draft.SourceArticleID || asset.DownloadStatus != "completed" {
			return ErrDraftStateConflict
		}
	}
	return nil
}

func classifyPublish(err error) (string, string, bool) {
	var publishErr *PublishError
	if errors.As(err, &publishErr) {
		return publishErr.Code, publishErr.Message, publishErr.Retryable
	}
	switch {
	case errors.Is(err, ErrPublisherDisabled):
		return "WECHAT_NOT_CONFIGURED", "微信公众号发布尚未配置", false
	case errors.Is(err, ErrCoverRequired):
		return "WECHAT_COVER_REQUIRED", "发布到微信公众号前必须选择封面", false
	default:
		return "WECHAT_PUBLISH_FAILED", "发布到微信公众号草稿箱失败", true
	}
}

func truncate(value string, max int) string {
	value = strings.TrimSpace(value)
	if utf8.RuneCountInString(value) <= max {
		return value
	}
	runes := []rune(value)
	return string(runes[:max])
}
