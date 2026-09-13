package editorial

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/hibiken/asynq"

	"github.com/chenbb0128/weavepress/server/internal/modules/workspace"
	"github.com/chenbb0128/weavepress/server/internal/platform/queue"
)

const TaskPublishWeChat = "publishing:wechat:draft"

type Service struct {
	store     Store
	articles  ArticleStore
	queue     *queue.Client
	publisher Publisher
	objects   AssetObjects
	enabled   bool
}

func New(store Store, articles ArticleStore, queueClient *queue.Client, publisher Publisher, objects AssetObjects, enabled bool) *Service {
	return &Service{store: store, articles: articles, queue: queueClient, publisher: publisher, objects: objects, enabled: enabled}
}

func (s *Service) Themes() []ThemeSummary { return ListThemes() }

func (s *Service) Assets(ctx context.Context, draftID uint64) ([]DraftAsset, error) {
	return s.store.ListDraftAssets(ctx, draftID)
}

func (s *Service) UploadAsset(ctx context.Context, draftID, userID uint64, filename, declared string, body []byte) (DraftAsset, error) {
	draft, err := s.store.GetDraft(ctx, draftID, false)
	if err != nil {
		return DraftAsset{}, err
	}
	if draft.Status != StatusEditing {
		return DraftAsset{}, ErrDraftNotEditable
	}
	inspected, err := InspectDraftImage(body, declared)
	if err != nil {
		return DraftAsset{}, err
	}
	if s.objects == nil {
		return DraftAsset{}, fmt.Errorf("draft asset object store unavailable")
	}
	now := time.Now().UTC()
	key := fmt.Sprintf("drafts/%04d/%02d/%s.%s", now.Year(), now.Month(), hex.EncodeToString(inspected.SHA256[:]), inspected.Extension)
	if err := s.objects.Put(ctx, key, body, inspected.MediaType); err != nil {
		return DraftAsset{}, err
	}
	return s.store.CreateUploadedDraftAsset(ctx, draftID, userID, NewDraftAsset{
		ObjectKey: key,
		MediaType: inspected.MediaType,
		ByteSize:  inspected.ByteSize,
		Width:     inspected.Width,
		Height:    inspected.Height,
		SHA256:    inspected.SHA256,
	})
}

func (s *Service) AssetObject(ctx context.Context, id uint64) (string, string, error) {
	asset, err := s.store.GetDraftAssetByID(ctx, id)
	return asset.ObjectKey, asset.MediaType, err
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
	draft, err := s.store.GetDraft(ctx, id, true)
	if err != nil {
		return Draft{}, err
	}
	return s.hydrateDraft(ctx, draft)
}

func (s *Service) hydrateDraft(ctx context.Context, draft Draft) (Draft, error) {
	if err := s.store.EnsureArticleAssets(ctx, draft.ID, draft.SourceArticleID, draft.CreatedBy); err != nil {
		return Draft{}, err
	}
	assets, err := s.store.ListDraftAssets(ctx, draft.ID)
	if err != nil {
		return Draft{}, err
	}
	draft.Assets = assets
	if draft.EditorDocument != nil {
		return draft, nil
	}
	draft.MigrationNeeded = true
	draft.ThemeID, draft.ThemeVersion = DefaultThemeID, DefaultThemeVersion
	mapping := make(map[uint64]uint64)
	// An old application image may have changed the article cover after migration.
	draft.CoverAssetID = nil
	for _, asset := range assets {
		if asset.ArticleAssetID != nil {
			mapping[*asset.ArticleAssetID] = asset.ID
			if draft.LegacyCoverAssetID != nil && *asset.ArticleAssetID == *draft.LegacyCoverAssetID {
				id := asset.ID
				draft.CoverAssetID = &id
			}
		}
	}
	converted, err := ConvertLegacyHTML(draft.ContentHTML, mapping)
	if err != nil {
		draft.MigrationWarnings = []string{"旧稿正文转换失败，请保留原文并重新整理后编辑"}
		return draft, nil
	}
	theme, err := ResolveTheme(draft.ThemeID, draft.ThemeVersion)
	if err != nil {
		return Draft{}, err
	}
	rendered, err := RenderDocument(converted.Document, theme)
	if err != nil {
		return Draft{}, err
	}
	draft.EditorDocument = &converted.Document
	draft.ContentHTML = rendered
	draft.MigrationWarnings = converted.Warnings
	return draft, nil
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
	draft, err := s.store.GetDraft(ctx, id, false)
	if err != nil {
		return Draft{}, err
	}
	if draft.CurrentVersion != expectedVersion || version >= draft.CurrentVersion {
		return Draft{}, ErrDraftVersionConflict
	}
	draft.Title, draft.Author, draft.Digest = historical.Title, historical.Author, historical.Digest
	draft.EditorDocument, draft.ContentHTML = historical.EditorDocument, historical.ContentHTML
	draft.ThemeID, draft.ThemeVersion, draft.CoverAssetID = historical.ThemeID, historical.ThemeVersion, historical.CoverAssetID
	draft.LegacyCoverAssetID = historical.LegacyCoverAssetID
	draft, err = s.hydrateDraft(ctx, draft)
	if err != nil {
		return Draft{}, err
	}
	if draft.EditorDocument == nil {
		return Draft{}, ErrLegacyConvertFailed
	}
	theme, err := ResolveTheme(draft.ThemeID, draft.ThemeVersion)
	if err != nil {
		return Draft{}, err
	}
	if err = ValidateDocument(*draft.EditorDocument); err != nil {
		return Draft{}, err
	}
	if err = s.validateDraftAssets(ctx, id, *draft.EditorDocument, draft.CoverAssetID); err != nil {
		return Draft{}, err
	}
	rendered, err := RenderDocument(*draft.EditorDocument, theme)
	if err != nil {
		return Draft{}, err
	}
	return s.store.RestoreDraftVersion(ctx, id, userID, RestoreInput{
		TargetVersion: version, ExpectedVersion: expectedVersion,
		Title: draft.Title, Author: draft.Author, Digest: draft.Digest,
		EditorDocument: draft.EditorDocument, ThemeID: draft.ThemeID, ThemeVersion: draft.ThemeVersion,
		ContentHTML: rendered, CoverAssetID: draft.CoverAssetID,
	})
}

func (s *Service) Preflight(ctx context.Context, id uint64) (PreflightResult, error) {
	draft, err := s.Get(ctx, id)
	if err != nil {
		return PreflightResult{}, err
	}
	return ValidateWeChatDraft(draft), nil
}

func (s *Service) Update(ctx context.Context, id, userID uint64, input UpdateInput) (Draft, error) {
	prepared, err := s.prepareUpdate(ctx, id, input)
	if err != nil {
		return Draft{}, err
	}
	return s.store.UpdateDraft(ctx, id, userID, prepared)
}

func (s *Service) prepareUpdate(ctx context.Context, draftID uint64, input UpdateInput) (UpdateInput, error) {
	input.Title = strings.TrimSpace(input.Title)
	input.Author = strings.TrimSpace(input.Author)
	input.Digest = strings.TrimSpace(input.Digest)
	input.ChangeNote = strings.TrimSpace(input.ChangeNote)
	if input.Title == "" || utf8.RuneCountInString(input.Title) > 255 || utf8.RuneCountInString(input.Author) > 64 || utf8.RuneCountInString(input.Digest) > 255 || input.ExpectedVersion == 0 {
		return UpdateInput{}, ErrDraftStateConflict
	}
	if input.EditorDocument == nil {
		return UpdateInput{}, ErrDocumentInvalid
	}
	input.ThemeVersion = DefaultThemeVersion
	theme, err := ResolveTheme(input.ThemeID, input.ThemeVersion)
	if err != nil {
		return UpdateInput{}, err
	}
	if err = ValidateDocument(*input.EditorDocument); err != nil {
		return UpdateInput{}, err
	}
	if err = s.validateDraftAssets(ctx, draftID, *input.EditorDocument, input.CoverAssetID); err != nil {
		return UpdateInput{}, err
	}
	input.ContentHTML, err = RenderDocument(*input.EditorDocument, theme)
	return input, err
}

func (s *Service) SubmitReview(ctx context.Context, id, userID uint64) (Draft, error) {
	draft, err := s.Get(ctx, id)
	if err != nil {
		return Draft{}, err
	}
	if result := ValidateWeChatDraft(draft); !result.Valid {
		return Draft{}, &PreflightError{Result: result}
	}
	return s.store.SetDraftStatus(ctx, id, userID, StatusEditing, StatusInReview, "提交审核", draft.CurrentVersion)
}

func (s *Service) Review(ctx context.Context, id, userID uint64, approved bool, note string) (Draft, error) {
	target, action := StatusEditing, "审核退回"
	if approved {
		target, action = StatusApproved, "审核通过"
	}
	if note = strings.TrimSpace(note); note != "" {
		action += "：" + truncate(note, 200)
	}
	return s.store.SetDraftStatus(ctx, id, userID, StatusInReview, target, action, 0)
}

func (s *Service) Publish(ctx context.Context, id, userID uint64) (PublishJob, error) {
	if !s.enabled {
		return PublishJob{}, ErrPublisherDisabled
	}
	draft, err := s.Get(ctx, id)
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
	draft, err := s.Get(ctx, job.DraftID)
	if err != nil {
		return err
	}
	if result := ValidateWeChatDraft(draft); !result.Valid {
		return &PreflightError{Result: result}
	}
	if job.Status == PublishQueued {
		if err := s.store.SetPublishJobPublishing(ctx, jobID); err != nil {
			return err
		}
	}
	result, err := s.publisher.Publish(ctx, draft)
	if err != nil {
		return err
	}
	return s.store.CompletePublishJob(ctx, jobID, result.RemoteMediaID)
}

func (s *Service) validateDraftAssets(ctx context.Context, draftID uint64, doc Document, coverID *uint64) error {
	validate := func(id uint64, cover bool) error {
		asset, err := s.store.GetDraftAsset(ctx, draftID, id)
		if errors.Is(err, workspace.ErrNotFound) {
			return ErrDraftAssetInvalid
		}
		if err != nil {
			return err
		}
		if asset.DraftID != draftID || asset.ID != id || strings.TrimSpace(asset.ObjectKey) == "" || !isWeChatImageType(asset.MediaType, cover) {
			return ErrDraftAssetInvalid
		}
		if !cover && asset.ByteSize > WeChatMaxContentImageSize {
			return ErrDraftAssetTooLarge
		}
		if cover && (!asset.CoverEligible || asset.ByteSize > WeChatMaxCoverImageSize) || !cover && !asset.BodyEligible {
			return ErrDraftAssetInvalid
		}
		return nil
	}
	for _, id := range ReferencedDraftAssetIDs(doc) {
		if err := validate(id, false); err != nil {
			return err
		}
	}
	if coverID != nil {
		return validate(*coverID, true)
	}
	return nil
}

func classifyPublish(err error) (string, string, bool) {
	var publishErr *PublishError
	if errors.As(err, &publishErr) {
		return publishErr.Code, publishErr.Message, publishErr.Retryable
	}
	switch {
	case errors.Is(err, ErrDraftPreflightFailed):
		return "DRAFT_PREFLIGHT_FAILED", "稿件未通过发布预检", false
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
