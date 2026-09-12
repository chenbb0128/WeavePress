package mysqlstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/chenbb0128/weavepress/server/internal/modules/editorial"
	"github.com/chenbb0128/weavepress/server/internal/modules/workspace"
)

const (
	draftColumns = `id, source_article_id, title, author, digest, content_html, cover_asset_id,
		status, current_version, created_by, updated_by, created_at, updated_at,
		editor_document, theme_id, theme_version`
	draftVersionColumns = `id, draft_id, version, title, author, digest, content_html, cover_asset_id,
		change_note, created_by, created_at, editor_document, theme_id, theme_version`
	draftAssetColumns = `id, draft_id, origin, article_asset_id, object_key, media_type,
		byte_size, width, height, sha256, uploaded_by, created_at`
	publishJobColumns = `id, draft_id, requested_by, status, attempts, manual_retries,
		remote_media_id, error_code, error_message, started_at, finished_at, created_at, updated_at`
)

func (s *Store) CreateDraft(ctx context.Context, articleID, userID uint64, title, author, digest, contentHTML string, coverID *uint64) (editorial.Draft, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return editorial.Draft{}, err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `INSERT INTO drafts (source_article_id, title, author, digest, content_html, cover_asset_id, created_by, updated_by) VALUES (?, ?, ?, ?, ?, NULL, ?, ?)`, articleID, title, author, digest, contentHTML, userID, userID)
	if err != nil {
		return editorial.Draft{}, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return editorial.Draft{}, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO draft_versions (draft_id, version, title, author, digest, content_html, cover_asset_id, change_note, created_by) VALUES (?, 1, ?, ?, ?, ?, NULL, '从采集文章创建', ?)`, id, title, author, digest, contentHTML, userID); err != nil {
		return editorial.Draft{}, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO draft_events (draft_id, actor_id, from_status, to_status, note) VALUES (?, ?, '', 'editing', '从采集文章创建')`, id, userID); err != nil {
		return editorial.Draft{}, err
	}
	if err = ensureArticleAssets(ctx, tx, uint64(id), articleID, userID); err != nil {
		return editorial.Draft{}, err
	}
	if coverID != nil {
		mappedCoverID, mapErr := getArticleDraftAssetID(ctx, tx, uint64(id), *coverID)
		if mapErr != nil {
			return editorial.Draft{}, mapErr
		}
		if _, err = tx.ExecContext(ctx, `UPDATE drafts SET cover_asset_id = ? WHERE id = ?`, mappedCoverID, id); err != nil {
			return editorial.Draft{}, err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE draft_versions SET cover_asset_id = ? WHERE draft_id = ? AND version = 1`, mappedCoverID, id); err != nil {
			return editorial.Draft{}, err
		}
	}
	if err = tx.Commit(); err != nil {
		return editorial.Draft{}, err
	}
	return s.GetDraft(ctx, uint64(id), true)
}

func (s *Store) EnsureArticleAssets(ctx context.Context, draftID, articleID, userID uint64) error {
	return ensureArticleAssets(ctx, s.db, draftID, articleID, userID)
}

func (s *Store) ListDraftAssets(ctx context.Context, draftID uint64) ([]editorial.DraftAsset, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+draftAssetColumns+` FROM draft_assets WHERE draft_id = ? ORDER BY id`, draftID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	assets := make([]editorial.DraftAsset, 0)
	for rows.Next() {
		asset, scanErr := scanDraftAsset(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		assets = append(assets, asset)
	}
	return assets, rows.Err()
}

func (s *Store) GetDraftAsset(ctx context.Context, draftID, assetID uint64) (editorial.DraftAsset, error) {
	return scanDraftAsset(s.db.QueryRowContext(ctx, `SELECT `+draftAssetColumns+` FROM draft_assets WHERE id = ? AND draft_id = ?`, assetID, draftID))
}

func (s *Store) GetDraftAssetByID(ctx context.Context, assetID uint64) (editorial.DraftAsset, error) {
	return scanDraftAsset(s.db.QueryRowContext(ctx, `SELECT `+draftAssetColumns+` FROM draft_assets WHERE id = ?`, assetID))
}

func (s *Store) CreateUploadedDraftAsset(ctx context.Context, draftID, userID uint64, input editorial.NewDraftAsset) (editorial.DraftAsset, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return editorial.DraftAsset{}, err
	}
	defer tx.Rollback()
	var status string
	if err := tx.QueryRowContext(ctx, `SELECT status FROM drafts WHERE id = ? FOR UPDATE`, draftID).Scan(&status); errors.Is(err, sql.ErrNoRows) {
		return editorial.DraftAsset{}, workspace.ErrNotFound
	} else if err != nil {
		return editorial.DraftAsset{}, err
	}
	if status != editorial.StatusEditing {
		return editorial.DraftAsset{}, editorial.ErrDraftNotEditable
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO draft_assets
		(draft_id, origin, article_asset_id, object_key, media_type, byte_size, width, height, sha256, uploaded_by)
		VALUES (?, 'upload', NULL, ?, ?, ?, ?, ?, ?, ?)`, draftID, input.ObjectKey, input.MediaType, input.ByteSize, input.Width, input.Height, input.SHA256[:], userID)
	if err != nil {
		return editorial.DraftAsset{}, err
	}
	assetID, err := result.LastInsertId()
	if err != nil {
		return editorial.DraftAsset{}, err
	}
	asset, err := scanDraftAsset(tx.QueryRowContext(ctx, `SELECT `+draftAssetColumns+` FROM draft_assets WHERE id = ?`, assetID))
	if err != nil {
		return editorial.DraftAsset{}, err
	}
	if err = tx.Commit(); err != nil {
		return editorial.DraftAsset{}, err
	}
	return asset, nil
}

func (s *Store) GetDraft(ctx context.Context, id uint64, withSource bool) (editorial.Draft, error) {
	draft, err := scanDraft(s.db.QueryRowContext(ctx, `SELECT `+draftColumns+` FROM drafts WHERE id = ?`, id))
	if err != nil {
		return draft, err
	}
	if withSource {
		article, articleErr := s.GetArticle(ctx, draft.SourceArticleID)
		if articleErr != nil {
			return draft, articleErr
		}
		draft.SourceArticle = &article
		rows, queryErr := s.db.QueryContext(ctx, `SELECT id, draft_id, actor_id, from_status, to_status, note, created_at FROM draft_events WHERE draft_id = ? ORDER BY id`, id)
		if queryErr != nil {
			return draft, queryErr
		}
		defer rows.Close()
		draft.Events = make([]editorial.DraftEvent, 0)
		for rows.Next() {
			var event editorial.DraftEvent
			if scanErr := rows.Scan(&event.ID, &event.DraftID, &event.ActorID, &event.FromStatus, &event.ToStatus, &event.Note, &event.CreatedAt); scanErr != nil {
				return draft, scanErr
			}
			draft.Events = append(draft.Events, event)
		}
		if rowsErr := rows.Err(); rowsErr != nil {
			return draft, rowsErr
		}
	}
	return draft, nil
}

func (s *Store) ListDrafts(ctx context.Context, keyword, status string, page, pageSize int) (editorial.Page[editorial.Draft], error) {
	where, args := ` WHERE 1=1`, []any{}
	if keyword != "" {
		where += ` AND (title LIKE ? OR author LIKE ?)`
		like := "%" + keyword + "%"
		args = append(args, like, like)
	}
	if status != "" {
		where += ` AND status = ?`
		args = append(args, status)
	}
	var total int64
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM drafts`+where, args...).Scan(&total); err != nil {
		return editorial.Page[editorial.Draft]{}, err
	}
	args = append(args, pageSize, (page-1)*pageSize)
	rows, err := s.db.QueryContext(ctx, `SELECT `+draftColumns+` FROM drafts`+where+` ORDER BY updated_at DESC, id DESC LIMIT ? OFFSET ?`, args...)
	if err != nil {
		return editorial.Page[editorial.Draft]{}, err
	}
	defer rows.Close()
	items := make([]editorial.Draft, 0)
	for rows.Next() {
		draft, scanErr := scanDraft(rows)
		if scanErr != nil {
			return editorial.Page[editorial.Draft]{}, scanErr
		}
		draft.ContentHTML = ""
		items = append(items, draft)
	}
	return editorial.Page[editorial.Draft]{Items: items, Total: total, Page: page, PageSize: pageSize}, rows.Err()
}

func (s *Store) ListDraftVersions(ctx context.Context, draftID uint64) ([]editorial.DraftVersion, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+draftVersionColumns+` FROM draft_versions WHERE draft_id = ? ORDER BY version DESC`, draftID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]editorial.DraftVersion, 0)
	for rows.Next() {
		item, scanErr := scanDraftVersion(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) GetDraftVersion(ctx context.Context, draftID uint64, version uint) (editorial.DraftVersion, error) {
	return scanDraftVersion(s.db.QueryRowContext(ctx, `SELECT `+draftVersionColumns+` FROM draft_versions WHERE draft_id = ? AND version = ?`, draftID, version))
}

func (s *Store) UpdateDraft(ctx context.Context, id, userID uint64, input editorial.UpdateInput) (editorial.Draft, error) {
	editorDocument, err := marshalEditorDocument(input.EditorDocument)
	if err != nil {
		return editorial.Draft{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return editorial.Draft{}, err
	}
	defer tx.Rollback()
	var status string
	var version uint
	if err := tx.QueryRowContext(ctx, `SELECT status, current_version FROM drafts WHERE id = ? FOR UPDATE`, id).Scan(&status, &version); errors.Is(err, sql.ErrNoRows) {
		return editorial.Draft{}, workspace.ErrNotFound
	} else if err != nil {
		return editorial.Draft{}, err
	}
	if status != editorial.StatusEditing {
		return editorial.Draft{}, editorial.ErrDraftNotEditable
	}
	if version != input.ExpectedVersion {
		return editorial.Draft{}, editorial.ErrDraftVersionConflict
	}
	nextVersion := version + 1
	result, err := tx.ExecContext(ctx, `UPDATE drafts SET title = ?, author = ?, digest = ?, content_html = ?, editor_document = ?, theme_id = ?, theme_version = ?, cover_asset_id = ?, current_version = ?, updated_by = ? WHERE id = ? AND status = 'editing' AND current_version = ?`, input.Title, input.Author, input.Digest, input.ContentHTML, editorDocument, input.ThemeID, input.ThemeVersion, nullableID(input.CoverAssetID), nextVersion, userID, id, version)
	if err != nil {
		return editorial.Draft{}, err
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return editorial.Draft{}, editorial.ErrDraftVersionConflict
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO draft_versions (draft_id, version, title, author, digest, content_html, editor_document, theme_id, theme_version, cover_asset_id, change_note, created_by) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, id, nextVersion, input.Title, input.Author, input.Digest, input.ContentHTML, editorDocument, input.ThemeID, input.ThemeVersion, nullableID(input.CoverAssetID), input.ChangeNote, userID); err != nil {
		return editorial.Draft{}, err
	}
	if err = tx.Commit(); err != nil {
		return editorial.Draft{}, err
	}
	return s.GetDraft(ctx, id, true)
}

func (s *Store) RestoreDraftVersion(ctx context.Context, id, userID uint64, targetVersion, expectedVersion uint) (editorial.Draft, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return editorial.Draft{}, err
	}
	defer tx.Rollback()
	var status string
	var currentVersion uint
	if err := tx.QueryRowContext(ctx, `SELECT status, current_version FROM drafts WHERE id = ? FOR UPDATE`, id).Scan(&status, &currentVersion); errors.Is(err, sql.ErrNoRows) {
		return editorial.Draft{}, workspace.ErrNotFound
	} else if err != nil {
		return editorial.Draft{}, err
	}
	if status != editorial.StatusEditing {
		return editorial.Draft{}, editorial.ErrDraftNotEditable
	}
	if currentVersion != expectedVersion || targetVersion >= currentVersion {
		return editorial.Draft{}, editorial.ErrDraftVersionConflict
	}
	historical, err := scanDraftVersion(tx.QueryRowContext(ctx, `SELECT `+draftVersionColumns+` FROM draft_versions WHERE draft_id = ? AND version = ?`, id, targetVersion))
	if err != nil {
		return editorial.Draft{}, err
	}
	nextVersion := currentVersion + 1
	editorDocument, err := marshalEditorDocument(historical.EditorDocument)
	if err != nil {
		return editorial.Draft{}, err
	}
	result, err := tx.ExecContext(ctx, `UPDATE drafts SET title = ?, author = ?, digest = ?, content_html = ?, editor_document = ?, theme_id = ?, theme_version = ?, cover_asset_id = ?, current_version = ?, updated_by = ? WHERE id = ? AND status = 'editing' AND current_version = ?`, historical.Title, historical.Author, historical.Digest, historical.ContentHTML, editorDocument, historical.ThemeID, historical.ThemeVersion, nullableID(historical.CoverAssetID), nextVersion, userID, id, currentVersion)
	if err != nil {
		return editorial.Draft{}, err
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return editorial.Draft{}, editorial.ErrDraftVersionConflict
	}
	changeNote := fmt.Sprintf("恢复自 v%d", targetVersion)
	if _, err = tx.ExecContext(ctx, `INSERT INTO draft_versions (draft_id, version, title, author, digest, content_html, editor_document, theme_id, theme_version, cover_asset_id, change_note, created_by) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, id, nextVersion, historical.Title, historical.Author, historical.Digest, historical.ContentHTML, editorDocument, historical.ThemeID, historical.ThemeVersion, nullableID(historical.CoverAssetID), changeNote, userID); err != nil {
		return editorial.Draft{}, err
	}
	if err = tx.Commit(); err != nil {
		return editorial.Draft{}, err
	}
	return s.GetDraft(ctx, id, true)
}

func (s *Store) SetDraftStatus(ctx context.Context, id, userID uint64, from, to, note string) (editorial.Draft, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return editorial.Draft{}, err
	}
	defer tx.Rollback()
	var current string
	if err = tx.QueryRowContext(ctx, `SELECT status FROM drafts WHERE id = ? FOR UPDATE`, id).Scan(&current); errors.Is(err, sql.ErrNoRows) {
		return editorial.Draft{}, workspace.ErrNotFound
	} else if err != nil {
		return editorial.Draft{}, err
	}
	if current != from {
		return editorial.Draft{}, editorial.ErrDraftStateConflict
	}
	if _, err = tx.ExecContext(ctx, `UPDATE drafts SET status = ?, updated_by = ? WHERE id = ?`, to, userID, id); err != nil {
		return editorial.Draft{}, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO draft_events (draft_id, actor_id, from_status, to_status, note) VALUES (?, ?, ?, ?, ?)`, id, userID, from, to, note); err != nil {
		return editorial.Draft{}, err
	}
	if err = tx.Commit(); err != nil {
		return editorial.Draft{}, err
	}
	return s.GetDraft(ctx, id, true)
}

func (s *Store) CreatePublishJob(ctx context.Context, draftID, userID uint64) (editorial.PublishJob, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return editorial.PublishJob{}, err
	}
	defer tx.Rollback()
	var status string
	if err := tx.QueryRowContext(ctx, `SELECT status FROM drafts WHERE id = ? FOR UPDATE`, draftID).Scan(&status); errors.Is(err, sql.ErrNoRows) {
		return editorial.PublishJob{}, workspace.ErrNotFound
	} else if err != nil {
		return editorial.PublishJob{}, err
	}
	if status != editorial.StatusApproved {
		return editorial.PublishJob{}, editorial.ErrDraftStateConflict
	}
	var active int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM wechat_publish_jobs WHERE draft_id = ? AND status IN ('queued','publishing')`, draftID).Scan(&active); err != nil {
		return editorial.PublishJob{}, err
	}
	if active > 0 {
		return editorial.PublishJob{}, editorial.ErrDraftStateConflict
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO wechat_publish_jobs (draft_id, requested_by) VALUES (?, ?)`, draftID, userID)
	if err != nil {
		return editorial.PublishJob{}, err
	}
	jobID, err := result.LastInsertId()
	if err != nil {
		return editorial.PublishJob{}, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE drafts SET status = 'publishing', updated_by = ? WHERE id = ?`, userID, draftID); err != nil {
		return editorial.PublishJob{}, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO draft_events (draft_id, actor_id, from_status, to_status, note) VALUES (?, ?, 'approved', 'publishing', '提交微信公众号草稿箱')`, draftID, userID); err != nil {
		return editorial.PublishJob{}, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO wechat_publish_job_events (job_id, status, message) VALUES (?, 'queued', '微信公众号草稿发布任务已创建')`, jobID); err != nil {
		return editorial.PublishJob{}, err
	}
	if err = tx.Commit(); err != nil {
		return editorial.PublishJob{}, err
	}
	return s.GetPublishJob(ctx, uint64(jobID), false)
}

func (s *Store) GetPublishJob(ctx context.Context, id uint64, withEvents bool) (editorial.PublishJob, error) {
	job, err := scanPublishJob(s.db.QueryRowContext(ctx, `SELECT `+publishJobColumns+` FROM wechat_publish_jobs WHERE id = ?`, id))
	if err != nil {
		return job, err
	}
	draft, draftErr := s.GetDraft(ctx, job.DraftID, false)
	if draftErr == nil {
		draft.ContentHTML = ""
		job.Draft = &draft
	}
	if withEvents {
		rows, queryErr := s.db.QueryContext(ctx, `SELECT id, status, message, created_at FROM wechat_publish_job_events WHERE job_id = ? ORDER BY id`, id)
		if queryErr != nil {
			return job, queryErr
		}
		defer rows.Close()
		job.Events = make([]editorial.PublishJobEvent, 0)
		for rows.Next() {
			var event editorial.PublishJobEvent
			if err := rows.Scan(&event.ID, &event.Status, &event.Message, &event.CreatedAt); err != nil {
				return job, err
			}
			job.Events = append(job.Events, event)
		}
		if err := rows.Err(); err != nil {
			return job, err
		}
	}
	return job, nil
}

func (s *Store) ListPublishJobs(ctx context.Context, status string, page, pageSize int) (editorial.Page[editorial.PublishJob], error) {
	where, args := ` WHERE 1=1`, []any{}
	if status != "" {
		where += ` AND status = ?`
		args = append(args, status)
	}
	var total int64
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM wechat_publish_jobs`+where, args...).Scan(&total); err != nil {
		return editorial.Page[editorial.PublishJob]{}, err
	}
	args = append(args, pageSize, (page-1)*pageSize)
	rows, err := s.db.QueryContext(ctx, `SELECT `+publishJobColumns+` FROM wechat_publish_jobs`+where+` ORDER BY id DESC LIMIT ? OFFSET ?`, args...)
	if err != nil {
		return editorial.Page[editorial.PublishJob]{}, err
	}
	defer rows.Close()
	items := make([]editorial.PublishJob, 0)
	for rows.Next() {
		job, scanErr := scanPublishJob(rows)
		if scanErr != nil {
			return editorial.Page[editorial.PublishJob]{}, scanErr
		}
		items = append(items, job)
	}
	if err := rows.Err(); err != nil {
		return editorial.Page[editorial.PublishJob]{}, err
	}
	if err := rows.Close(); err != nil {
		return editorial.Page[editorial.PublishJob]{}, err
	}
	for index := range items {
		draft, draftErr := s.GetDraft(ctx, items[index].DraftID, false)
		if draftErr != nil {
			return editorial.Page[editorial.PublishJob]{}, draftErr
		}
		draft.ContentHTML = ""
		items[index].Draft = &draft
	}
	return editorial.Page[editorial.PublishJob]{Items: items, Total: total, Page: page, PageSize: pageSize}, nil
}

func (s *Store) SetPublishJobPublishing(ctx context.Context, id uint64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE wechat_publish_jobs SET status = 'publishing', attempts = attempts + 1, started_at = COALESCE(started_at, UTC_TIMESTAMP(3)), error_code = '', error_message = '', finished_at = NULL WHERE id = ? AND status = 'queued'`, id)
	if err != nil {
		return err
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return editorial.ErrDraftStateConflict
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO wechat_publish_job_events (job_id, status, message) VALUES (?, 'publishing', '正在上传素材并写入微信公众号草稿箱')`, id); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) SetPublishJobFailure(ctx context.Context, id uint64, code, message string, retrying bool) error {
	status, draftStatus, eventMessage := editorial.PublishFailed, editorial.StatusPublishFailed, message
	if retrying {
		status, draftStatus, eventMessage = editorial.PublishQueued, editorial.StatusPublishing, "临时错误，等待自动重试："+message
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE wechat_publish_jobs SET status = ?, error_code = ?, error_message = ?, finished_at = IF(? = 'failed', UTC_TIMESTAMP(3), NULL) WHERE id = ? AND status IN ('queued', 'publishing')`, status, code, message, status, id)
	if err != nil {
		return err
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return tx.Commit()
	}
	if _, err = tx.ExecContext(ctx, `UPDATE drafts d JOIN wechat_publish_jobs j ON j.draft_id = d.id SET d.status = ? WHERE j.id = ?`, draftStatus, id); err != nil {
		return err
	}
	if !retrying {
		if _, err = tx.ExecContext(ctx, `INSERT INTO draft_events (draft_id, actor_id, from_status, to_status, note) SELECT draft_id, requested_by, 'publishing', 'publish_failed', ? FROM wechat_publish_jobs WHERE id = ?`, message, id); err != nil {
			return err
		}
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO wechat_publish_job_events (job_id, status, message) VALUES (?, ?, ?)`, id, status, eventMessage); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) CompletePublishJob(ctx context.Context, id uint64, remoteMediaID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE wechat_publish_jobs SET status = 'completed', remote_media_id = ?, error_code = '', error_message = '', finished_at = UTC_TIMESTAMP(3) WHERE id = ? AND status = 'publishing'`, remoteMediaID, id)
	if err != nil {
		return err
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return editorial.ErrDraftStateConflict
	}
	if _, err = tx.ExecContext(ctx, `UPDATE drafts d JOIN wechat_publish_jobs j ON j.draft_id = d.id SET d.status = 'published' WHERE j.id = ?`, id); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO draft_events (draft_id, actor_id, from_status, to_status, note) SELECT draft_id, requested_by, 'publishing', 'published', '已写入微信公众号草稿箱' FROM wechat_publish_jobs WHERE id = ?`, id); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO wechat_publish_job_events (job_id, status, message) VALUES (?, 'completed', '已写入微信公众号草稿箱')`, id); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) RetryPublishJob(ctx context.Context, id, userID uint64) (editorial.PublishJob, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return editorial.PublishJob{}, err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE wechat_publish_jobs SET status = 'queued', requested_by = ?, manual_retries = manual_retries + 1, error_code = '', error_message = '', finished_at = NULL WHERE id = ? AND status = 'failed'`, userID, id)
	if err != nil {
		return editorial.PublishJob{}, err
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return editorial.PublishJob{}, editorial.ErrPublishNotRetryable
	}
	if _, err = tx.ExecContext(ctx, `UPDATE drafts d JOIN wechat_publish_jobs j ON j.draft_id = d.id SET d.status = 'publishing' WHERE j.id = ?`, id); err != nil {
		return editorial.PublishJob{}, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO draft_events (draft_id, actor_id, from_status, to_status, note) SELECT draft_id, requested_by, 'publish_failed', 'publishing', '重试微信公众号发布' FROM wechat_publish_jobs WHERE id = ?`, id); err != nil {
		return editorial.PublishJob{}, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO wechat_publish_job_events (job_id, status, message) VALUES (?, 'queued', '用户手动重试微信公众号发布')`, id); err != nil {
		return editorial.PublishJob{}, err
	}
	if err = tx.Commit(); err != nil {
		return editorial.PublishJob{}, err
	}
	return s.GetPublishJob(ctx, id, false)
}

func scanDraft(row scanner) (editorial.Draft, error) {
	var draft editorial.Draft
	var cover sql.NullInt64
	var editorDocument []byte
	err := row.Scan(&draft.ID, &draft.SourceArticleID, &draft.Title, &draft.Author, &draft.Digest, &draft.ContentHTML, &cover, &draft.Status, &draft.CurrentVersion, &draft.CreatedBy, &draft.UpdatedBy, &draft.CreatedAt, &draft.UpdatedAt, &editorDocument, &draft.ThemeID, &draft.ThemeVersion)
	if errors.Is(err, sql.ErrNoRows) {
		return draft, workspace.ErrNotFound
	}
	if err != nil {
		return draft, err
	}
	if cover.Valid {
		id := uint64(cover.Int64)
		draft.CoverAssetID = &id
	}
	draft.EditorDocument, err = decodeEditorDocument(editorDocument)
	if err != nil {
		return draft, err
	}
	return draft, nil
}

func scanDraftVersion(row scanner) (editorial.DraftVersion, error) {
	var item editorial.DraftVersion
	var cover sql.NullInt64
	var editorDocument []byte
	err := row.Scan(&item.ID, &item.DraftID, &item.Version, &item.Title, &item.Author, &item.Digest, &item.ContentHTML, &cover, &item.ChangeNote, &item.CreatedBy, &item.CreatedAt, &editorDocument, &item.ThemeID, &item.ThemeVersion)
	if errors.Is(err, sql.ErrNoRows) {
		return item, workspace.ErrNotFound
	}
	if err != nil {
		return item, err
	}
	if cover.Valid {
		id := uint64(cover.Int64)
		item.CoverAssetID = &id
	}
	item.EditorDocument, err = decodeEditorDocument(editorDocument)
	if err != nil {
		return item, err
	}
	return item, nil
}

func scanDraftAsset(row scanner) (editorial.DraftAsset, error) {
	var asset editorial.DraftAsset
	var articleAssetID sql.NullInt64
	var sha256Bytes []byte
	err := row.Scan(&asset.ID, &asset.DraftID, &asset.Origin, &articleAssetID, &asset.ObjectKey, &asset.MediaType, &asset.ByteSize, &asset.Width, &asset.Height, &sha256Bytes, &asset.UploadedBy, &asset.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return asset, workspace.ErrNotFound
	}
	if err != nil {
		return asset, err
	}
	if len(sha256Bytes) != len(asset.SHA256) {
		return asset, fmt.Errorf("draft asset %d has invalid sha256 length %d", asset.ID, len(sha256Bytes))
	}
	copy(asset.SHA256[:], sha256Bytes)
	if articleAssetID.Valid {
		id := uint64(articleAssetID.Int64)
		asset.ArticleAssetID = &id
	}
	asset.BodyEligible = asset.ByteSize <= editorial.WeChatMaxContentImageSize
	asset.CoverEligible = asset.ByteSize <= editorial.WeChatMaxCoverImageSize
	return asset, nil
}

func decodeEditorDocument(raw []byte) (*editorial.Document, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var document editorial.Document
	if err := json.Unmarshal(raw, &document); err != nil {
		return nil, fmt.Errorf("decode editor document: %w", err)
	}
	if err := editorial.ValidateDocument(document); err != nil {
		return nil, err
	}
	return &document, nil
}

func marshalEditorDocument(document *editorial.Document) (any, error) {
	if document == nil {
		return nil, nil
	}
	raw, err := json.Marshal(document)
	if err != nil {
		return nil, fmt.Errorf("encode editor document: %w", err)
	}
	return raw, nil
}

type sqlExecer interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

type sqlRowQueryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func ensureArticleAssets(ctx context.Context, exec sqlExecer, draftID, articleID, userID uint64) error {
	_, err := exec.ExecContext(ctx, `INSERT IGNORE INTO draft_assets
		(draft_id, origin, article_asset_id, object_key, media_type, byte_size, width, height, sha256, uploaded_by)
		SELECT ?, 'article', id, object_key, media_type, byte_size, width, height, sha256, ?
		FROM article_assets
		WHERE article_id = ? AND download_status = 'completed' AND object_key <> ''`, draftID, userID, articleID)
	return err
}

func getArticleDraftAssetID(ctx context.Context, queryer sqlRowQueryer, draftID, articleAssetID uint64) (uint64, error) {
	var assetID uint64
	err := queryer.QueryRowContext(ctx, `SELECT id FROM draft_assets WHERE draft_id = ? AND article_asset_id = ?`, draftID, articleAssetID).Scan(&assetID)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, workspace.ErrNotFound
	}
	return assetID, err
}

func scanPublishJob(row scanner) (editorial.PublishJob, error) {
	var job editorial.PublishJob
	var started, finished sql.NullTime
	err := row.Scan(&job.ID, &job.DraftID, &job.RequestedBy, &job.Status, &job.Attempts, &job.ManualRetries, &job.RemoteMediaID, &job.ErrorCode, &job.ErrorMessage, &started, &finished, &job.CreatedAt, &job.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return job, workspace.ErrNotFound
	}
	if err != nil {
		return job, err
	}
	if started.Valid {
		job.StartedAt = &started.Time
	}
	if finished.Valid {
		job.FinishedAt = &finished.Time
	}
	return job, nil
}

func nullableID(id *uint64) any {
	if id == nil {
		return nil
	}
	return *id
}
