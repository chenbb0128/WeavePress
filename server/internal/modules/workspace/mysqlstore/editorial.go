package mysqlstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/chenbb0128/weavepress/server/internal/modules/editorial"
	"github.com/chenbb0128/weavepress/server/internal/modules/workspace"
)

const (
	draftColumns = `id, source_article_id, title, author, digest, content_html, cover_asset_id,
		status, current_version, created_by, updated_by, created_at, updated_at`
	publishJobColumns = `id, draft_id, requested_by, status, attempts, manual_retries,
		remote_media_id, error_code, error_message, started_at, finished_at, created_at, updated_at`
)

func (s *Store) CreateDraft(ctx context.Context, articleID, userID uint64, title, author, digest, contentHTML string, coverID *uint64) (editorial.Draft, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return editorial.Draft{}, err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `INSERT INTO drafts (source_article_id, title, author, digest, content_html, cover_asset_id, created_by, updated_by) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, articleID, title, author, digest, contentHTML, nullableID(coverID), userID, userID)
	if err != nil {
		return editorial.Draft{}, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return editorial.Draft{}, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO draft_versions (draft_id, version, title, author, digest, content_html, cover_asset_id, change_note, created_by) VALUES (?, 1, ?, ?, ?, ?, ?, '从采集文章创建', ?)`, id, title, author, digest, contentHTML, nullableID(coverID), userID); err != nil {
		return editorial.Draft{}, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO draft_events (draft_id, actor_id, from_status, to_status, note) VALUES (?, ?, '', 'editing', '从采集文章创建')`, id, userID); err != nil {
		return editorial.Draft{}, err
	}
	if err = tx.Commit(); err != nil {
		return editorial.Draft{}, err
	}
	return s.GetDraft(ctx, uint64(id), true)
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
	rows, err := s.db.QueryContext(ctx, `SELECT id, draft_id, version, title, author, digest, content_html, cover_asset_id, change_note, created_by, created_at FROM draft_versions WHERE draft_id = ? ORDER BY version DESC`, draftID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]editorial.DraftVersion, 0)
	for rows.Next() {
		var item editorial.DraftVersion
		var cover sql.NullInt64
		if err := rows.Scan(&item.ID, &item.DraftID, &item.Version, &item.Title, &item.Author, &item.Digest, &item.ContentHTML, &cover, &item.ChangeNote, &item.CreatedBy, &item.CreatedAt); err != nil {
			return nil, err
		}
		if cover.Valid {
			id := uint64(cover.Int64)
			item.CoverAssetID = &id
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) GetDraftVersion(ctx context.Context, draftID uint64, version uint) (editorial.DraftVersion, error) {
	return scanDraftVersion(s.db.QueryRowContext(ctx, `SELECT id, draft_id, version, title, author, digest, content_html, cover_asset_id, change_note, created_by, created_at FROM draft_versions WHERE draft_id = ? AND version = ?`, draftID, version))
}

func (s *Store) UpdateDraft(ctx context.Context, id, userID uint64, input editorial.UpdateInput) (editorial.Draft, error) {
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
	result, err := tx.ExecContext(ctx, `UPDATE drafts SET title = ?, author = ?, digest = ?, content_html = ?, cover_asset_id = ?, current_version = ?, updated_by = ? WHERE id = ? AND status = 'editing' AND current_version = ?`, input.Title, input.Author, input.Digest, input.ContentHTML, nullableID(input.CoverAssetID), nextVersion, userID, id, version)
	if err != nil {
		return editorial.Draft{}, err
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return editorial.Draft{}, editorial.ErrDraftVersionConflict
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO draft_versions (draft_id, version, title, author, digest, content_html, cover_asset_id, change_note, created_by) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, id, nextVersion, input.Title, input.Author, input.Digest, input.ContentHTML, nullableID(input.CoverAssetID), input.ChangeNote, userID); err != nil {
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
	historical, err := scanDraftVersion(tx.QueryRowContext(ctx, `SELECT id, draft_id, version, title, author, digest, content_html, cover_asset_id, change_note, created_by, created_at FROM draft_versions WHERE draft_id = ? AND version = ?`, id, targetVersion))
	if err != nil {
		return editorial.Draft{}, err
	}
	nextVersion := currentVersion + 1
	result, err := tx.ExecContext(ctx, `UPDATE drafts SET title = ?, author = ?, digest = ?, content_html = ?, cover_asset_id = ?, current_version = ?, updated_by = ? WHERE id = ? AND status = 'editing' AND current_version = ?`, historical.Title, historical.Author, historical.Digest, historical.ContentHTML, nullableID(historical.CoverAssetID), nextVersion, userID, id, currentVersion)
	if err != nil {
		return editorial.Draft{}, err
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return editorial.Draft{}, editorial.ErrDraftVersionConflict
	}
	changeNote := fmt.Sprintf("恢复自 v%d", targetVersion)
	if _, err = tx.ExecContext(ctx, `INSERT INTO draft_versions (draft_id, version, title, author, digest, content_html, cover_asset_id, change_note, created_by) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, id, nextVersion, historical.Title, historical.Author, historical.Digest, historical.ContentHTML, nullableID(historical.CoverAssetID), changeNote, userID); err != nil {
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
	err := row.Scan(&draft.ID, &draft.SourceArticleID, &draft.Title, &draft.Author, &draft.Digest, &draft.ContentHTML, &cover, &draft.Status, &draft.CurrentVersion, &draft.CreatedBy, &draft.UpdatedBy, &draft.CreatedAt, &draft.UpdatedAt)
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
	return draft, nil
}

func scanDraftVersion(row scanner) (editorial.DraftVersion, error) {
	var item editorial.DraftVersion
	var cover sql.NullInt64
	err := row.Scan(&item.ID, &item.DraftID, &item.Version, &item.Title, &item.Author, &item.Digest, &item.ContentHTML, &cover, &item.ChangeNote, &item.CreatedBy, &item.CreatedAt)
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
	return item, nil
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
