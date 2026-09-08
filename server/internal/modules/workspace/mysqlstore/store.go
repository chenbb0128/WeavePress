package mysqlstore

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"

	"github.com/chenbb0128/weavepress/server/internal/modules/workspace"
)

const (
	userColumns    = `id, username, nickname, avatar, role, status, created_at, updated_at`
	articleColumns = `id, original_url, canonical_url, source_type, title, author, source_name, language,
		published_at, plain_text, clean_html, blocks_json, metadata_json, raw_object_key,
		status, duplicate_of_id, created_by, created_at, updated_at`
	jobColumns = `id, article_id, submitted_by, status, attempts, manual_retries, error_code,
		error_message, warnings_json, started_at, finished_at, created_at, updated_at`
)

type Store struct{ db *sql.DB }

func New(db *sql.DB) *Store { return &Store{db: db} }

func (s *Store) CreateUser(ctx context.Context, username, passwordHash, nickname string, role workspace.Role, status string) (workspace.User, error) {
	result, err := s.db.ExecContext(ctx, `INSERT INTO users (username, password_hash, nickname, role, status) VALUES (?, ?, ?, ?, ?)`, username, passwordHash, nickname, role, status)
	if err != nil {
		if duplicate(err) {
			return workspace.User{}, workspace.ErrUsernameTaken
		}
		return workspace.User{}, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return workspace.User{}, err
	}
	return s.GetUserByID(ctx, uint64(id))
}

func (s *Store) GetUserByID(ctx context.Context, id uint64) (workspace.User, error) {
	return scanUser(s.db.QueryRowContext(ctx, `SELECT `+userColumns+` FROM users WHERE id = ?`, id))
}

func (s *Store) GetUserByUsername(ctx context.Context, username string) (workspace.UserWithPassword, error) {
	var result workspace.UserWithPassword
	err := s.db.QueryRowContext(ctx, `SELECT `+userColumns+`, password_hash FROM users WHERE username = ?`, username).Scan(
		&result.ID, &result.Username, &result.Nickname, &result.Avatar, &result.Role, &result.Status,
		&result.CreatedAt, &result.UpdatedAt, &result.PasswordHash,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return workspace.UserWithPassword{}, workspace.ErrNotFound
	}
	return result, err
}

func (s *Store) ListUsers(ctx context.Context, keyword, status string, page, pageSize int) (workspace.Page[workspace.User], error) {
	where, args := ` WHERE 1=1`, []any{}
	if keyword != "" {
		where += ` AND (username LIKE ? OR nickname LIKE ?)`
		like := "%" + keyword + "%"
		args = append(args, like, like)
	}
	if status != "" {
		where += ` AND status = ?`
		args = append(args, status)
	}
	var total int64
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`+where, args...).Scan(&total); err != nil {
		return workspace.Page[workspace.User]{}, err
	}
	args = append(args, pageSize, (page-1)*pageSize)
	rows, err := s.db.QueryContext(ctx, `SELECT `+userColumns+` FROM users`+where+` ORDER BY id DESC LIMIT ? OFFSET ?`, args...)
	if err != nil {
		return workspace.Page[workspace.User]{}, err
	}
	defer rows.Close()
	items := make([]workspace.User, 0)
	for rows.Next() {
		user, err := scanUser(rows)
		if err != nil {
			return workspace.Page[workspace.User]{}, err
		}
		items = append(items, user)
	}
	return workspace.Page[workspace.User]{Items: items, Total: total, Page: page, PageSize: pageSize}, rows.Err()
}

func (s *Store) UpdateUser(ctx context.Context, id uint64, nickname string, role workspace.Role, status string) (workspace.User, error) {
	result, err := s.db.ExecContext(ctx, `UPDATE users SET nickname = ?, role = ?, status = ? WHERE id = ?`, nickname, role, status, id)
	if err != nil {
		return workspace.User{}, err
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return workspace.User{}, workspace.ErrNotFound
	}
	if status == "disabled" {
		_ = s.RevokeUserTokens(ctx, id)
	}
	return s.GetUserByID(ctx, id)
}

func (s *Store) UpdatePassword(ctx context.Context, id uint64, passwordHash string) error {
	result, err := s.db.ExecContext(ctx, `UPDATE users SET password_hash = ? WHERE id = ?`, passwordHash, id)
	if err != nil {
		return err
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return workspace.ErrNotFound
	}
	return s.RevokeUserTokens(ctx, id)
}

func (s *Store) CreateRefreshToken(ctx context.Context, userID uint64, family string, hash [32]byte, expires time.Time, agent, ip string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO auth_refresh_tokens (user_id, family_id, token_hash, user_agent, ip_address, expires_at) VALUES (?, ?, ?, ?, ?, ?)`, userID, family, hash[:], agent, ip, expires)
	return err
}

func (s *Store) RotateRefreshToken(ctx context.Context, oldHash [32]byte, family string, newHash [32]byte, expires time.Time, agent, ip string) (userID uint64, err error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	var storedFamily string
	var storedExpires time.Time
	var revoked sql.NullTime
	err = tx.QueryRowContext(ctx, `SELECT user_id, family_id, expires_at, revoked_at FROM auth_refresh_tokens WHERE token_hash = ? FOR UPDATE`, oldHash[:]).Scan(&userID, &storedFamily, &storedExpires, &revoked)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, workspace.ErrInvalidRefresh
	}
	if err != nil {
		return 0, err
	}
	if revoked.Valid {
		_, _ = tx.ExecContext(ctx, `UPDATE auth_refresh_tokens SET revoked_at = COALESCE(revoked_at, UTC_TIMESTAMP(3)) WHERE family_id = ?`, storedFamily)
		if commitErr := tx.Commit(); commitErr != nil {
			return 0, commitErr
		}
		return 0, workspace.ErrRefreshReuse
	}
	if time.Now().UTC().After(storedExpires) {
		return 0, workspace.ErrInvalidRefresh
	}
	if family == "" {
		family = storedFamily
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO auth_refresh_tokens (user_id, family_id, token_hash, user_agent, ip_address, expires_at) VALUES (?, ?, ?, ?, ?, ?)`, userID, family, newHash[:], agent, ip, expires); err != nil {
		return 0, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE auth_refresh_tokens SET revoked_at = UTC_TIMESTAMP(3), replaced_by_hash = ? WHERE token_hash = ?`, newHash[:], oldHash[:]); err != nil {
		return 0, err
	}
	err = tx.Commit()
	return userID, err
}

func (s *Store) RevokeRefreshToken(ctx context.Context, hash [32]byte) error {
	_, err := s.db.ExecContext(ctx, `UPDATE auth_refresh_tokens SET revoked_at = COALESCE(revoked_at, UTC_TIMESTAMP(3)) WHERE token_hash = ?`, hash[:])
	return err
}

func (s *Store) RevokeUserTokens(ctx context.Context, userID uint64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE auth_refresh_tokens SET revoked_at = COALESCE(revoked_at, UTC_TIMESTAMP(3)) WHERE user_id = ?`, userID)
	return err
}

func (s *Store) CreateArticleJob(ctx context.Context, original, canonical string, canonicalHash [32]byte, sourceType string, userID uint64) (article workspace.Article, job workspace.Job, reused bool, err error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return article, job, false, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	article, err = getArticleByHash(ctx, tx, canonicalHash)
	if err == nil {
		job, err = getLatestJob(ctx, tx, article.ID)
		if err != nil && !errors.Is(err, workspace.ErrNotFound) {
			return article, job, true, err
		}
		if err = tx.Commit(); err != nil {
			return article, job, true, err
		}
		return article, job, true, nil
	}
	if !errors.Is(err, workspace.ErrNotFound) {
		return article, job, false, err
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO articles (original_url, canonical_url, canonical_url_hash, source_type, plain_text, clean_html, blocks_json, metadata_json, created_by) VALUES (?, ?, ?, ?, '', '', JSON_ARRAY(), JSON_OBJECT(), ?)`, original, canonical, canonicalHash[:], sourceType, userID)
	if err != nil {
		if duplicate(err) {
			_ = tx.Rollback()
			article, findErr := getArticleByHash(ctx, s.db, canonicalHash)
			if findErr != nil {
				return article, job, true, findErr
			}
			job, _ = getLatestJob(ctx, s.db, article.ID)
			return article, job, true, nil
		}
		return article, job, false, err
	}
	articleID, err := result.LastInsertId()
	if err != nil {
		return article, job, false, err
	}
	jobResult, err := tx.ExecContext(ctx, `INSERT INTO collection_jobs (article_id, submitted_by, warnings_json) VALUES (?, ?, JSON_ARRAY())`, articleID, userID)
	if err != nil {
		return article, job, false, err
	}
	jobID, err := jobResult.LastInsertId()
	if err != nil {
		return article, job, false, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO collection_job_events (job_id, status, message) VALUES (?, 'queued', '采集任务已创建')`, jobID); err != nil {
		return article, job, false, err
	}
	if err = tx.Commit(); err != nil {
		return article, job, false, err
	}
	article, err = s.GetArticle(ctx, uint64(articleID))
	if err != nil {
		return article, job, false, err
	}
	job, err = s.GetJob(ctx, uint64(jobID), false)
	return article, job, false, err
}

func (s *Store) GetArticle(ctx context.Context, id uint64) (workspace.Article, error) {
	article, err := scanArticle(s.db.QueryRowContext(ctx, `SELECT `+articleColumns+` FROM articles WHERE id = ?`, id))
	if err != nil {
		return article, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, article_id, source_url, object_key, media_type, byte_size, width, height, position, is_cover, download_status, created_at FROM article_assets WHERE article_id = ? ORDER BY position, id`, id)
	if err != nil {
		return article, err
	}
	defer rows.Close()
	article.Assets = make([]workspace.Asset, 0)
	for rows.Next() {
		var asset workspace.Asset
		if err := rows.Scan(&asset.ID, &asset.ArticleID, &asset.SourceURL, &asset.ObjectKey, &asset.MediaType, &asset.ByteSize, &asset.Width, &asset.Height, &asset.Position, &asset.IsCover, &asset.DownloadStatus, &asset.CreatedAt); err != nil {
			return article, err
		}
		article.Assets = append(article.Assets, asset)
	}
	return article, rows.Err()
}

func (s *Store) ListArticles(ctx context.Context, keyword, sourceType, status string, page, pageSize int) (workspace.Page[workspace.Article], error) {
	where, args := ` WHERE 1=1`, []any{}
	if keyword != "" {
		where += ` AND (title LIKE ? OR author LIKE ? OR source_name LIKE ?)`
		like := "%" + keyword + "%"
		args = append(args, like, like, like)
	}
	if sourceType != "" {
		where += ` AND source_type = ?`
		args = append(args, sourceType)
	}
	if status != "" {
		where += ` AND status = ?`
		args = append(args, status)
	}
	var total int64
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM articles`+where, args...).Scan(&total); err != nil {
		return workspace.Page[workspace.Article]{}, err
	}
	args = append(args, pageSize, (page-1)*pageSize)
	rows, err := s.db.QueryContext(ctx, `SELECT `+articleColumns+` FROM articles`+where+` ORDER BY COALESCE(published_at, created_at) DESC LIMIT ? OFFSET ?`, args...)
	if err != nil {
		return workspace.Page[workspace.Article]{}, err
	}
	defer rows.Close()
	items := make([]workspace.Article, 0)
	for rows.Next() {
		article, err := scanArticle(rows)
		if err != nil {
			return workspace.Page[workspace.Article]{}, err
		}
		article.PlainText, article.CleanHTML, article.Blocks, article.Metadata = "", "", nil, nil
		items = append(items, article)
	}
	return workspace.Page[workspace.Article]{Items: items, Total: total, Page: page, PageSize: pageSize}, rows.Err()
}

func (s *Store) GetAsset(ctx context.Context, id uint64) (workspace.Asset, error) {
	var asset workspace.Asset
	err := s.db.QueryRowContext(ctx, `SELECT id, article_id, source_url, object_key, media_type, byte_size, width, height, position, is_cover, download_status, created_at FROM article_assets WHERE id = ?`, id).Scan(&asset.ID, &asset.ArticleID, &asset.SourceURL, &asset.ObjectKey, &asset.MediaType, &asset.ByteSize, &asset.Width, &asset.Height, &asset.Position, &asset.IsCover, &asset.DownloadStatus, &asset.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return asset, workspace.ErrNotFound
	}
	return asset, err
}

func (s *Store) GetJob(ctx context.Context, id uint64, withEvents bool) (workspace.Job, error) {
	job, err := scanJob(s.db.QueryRowContext(ctx, `SELECT `+jobColumns+` FROM collection_jobs WHERE id = ?`, id))
	if err != nil {
		return job, err
	}
	article, err := s.GetArticle(ctx, job.ArticleID)
	if err == nil {
		article.PlainText, article.CleanHTML, article.Blocks, article.Metadata, article.Assets = "", "", nil, nil, nil
		job.Article = &article
	}
	if withEvents {
		rows, queryErr := s.db.QueryContext(ctx, `SELECT id, status, message, created_at FROM collection_job_events WHERE job_id = ? ORDER BY id`, id)
		if queryErr != nil {
			return job, queryErr
		}
		defer rows.Close()
		job.Events = make([]workspace.JobEvent, 0)
		for rows.Next() {
			var event workspace.JobEvent
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

func (s *Store) ListJobs(ctx context.Context, status string, page, pageSize int) (workspace.Page[workspace.Job], error) {
	where, args := ` WHERE 1=1`, []any{}
	if status != "" {
		where += ` AND status = ?`
		args = append(args, status)
	}
	var total int64
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM collection_jobs`+where, args...).Scan(&total); err != nil {
		return workspace.Page[workspace.Job]{}, err
	}
	args = append(args, pageSize, (page-1)*pageSize)
	rows, err := s.db.QueryContext(ctx, `SELECT `+jobColumns+` FROM collection_jobs`+where+` ORDER BY id DESC LIMIT ? OFFSET ?`, args...)
	if err != nil {
		return workspace.Page[workspace.Job]{}, err
	}
	defer rows.Close()
	items := make([]workspace.Job, 0)
	for rows.Next() {
		job, err := scanJob(rows)
		if err != nil {
			return workspace.Page[workspace.Job]{}, err
		}
		items = append(items, job)
	}
	return workspace.Page[workspace.Job]{Items: items, Total: total, Page: page, PageSize: pageSize}, rows.Err()
}

func (s *Store) SetJobStage(ctx context.Context, id uint64, status, message string) error {
	previous, ok := map[string]string{"fetching": "queued", "parsing": "fetching", "storing_assets": "parsing"}[status]
	if !ok {
		return workspace.ErrJobStateConflict
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	query := `UPDATE collection_jobs SET status = ?, error_code = '', error_message = '', finished_at = NULL WHERE id = ? AND status = ?`
	if status == "fetching" {
		query = `UPDATE collection_jobs SET status = ?, attempts = attempts + 1, started_at = COALESCE(started_at, UTC_TIMESTAMP(3)), error_code = '', error_message = '', finished_at = NULL WHERE id = ? AND status = ?`
	}
	result, err := tx.ExecContext(ctx, query, status, id, previous)
	if err != nil {
		return err
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return workspace.ErrJobStateConflict
	}
	if _, err = tx.ExecContext(ctx, `UPDATE articles a JOIN collection_jobs j ON j.article_id = a.id SET a.status = 'processing' WHERE j.id = ?`, id); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO collection_job_events (job_id, status, message) VALUES (?, ?, ?)`, id, status, message); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) SetJobFailure(ctx context.Context, id uint64, code, message string, retrying bool) error {
	status, articleStatus := "failed", "failed"
	if retrying {
		status, articleStatus = "queued", "processing"
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE collection_jobs SET status = ?, error_code = ?, error_message = ?, finished_at = IF(? = 'failed', UTC_TIMESTAMP(3), NULL) WHERE id = ? AND status NOT IN ('completed', 'completed_with_warnings')`, status, code, message, status, id)
	if err != nil {
		return err
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return tx.Commit()
	}
	if _, err = tx.ExecContext(ctx, `UPDATE articles a JOIN collection_jobs j ON j.article_id = a.id SET a.status = ? WHERE j.id = ?`, articleStatus, id); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO collection_job_events (job_id, status, message) VALUES (?, ?, ?)`, id, status, message); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) RetryJob(ctx context.Context, id uint64) (workspace.Job, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return workspace.Job{}, err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE collection_jobs SET status = 'queued', manual_retries = manual_retries + 1, error_code = '', error_message = '', finished_at = NULL WHERE id = ? AND status = 'failed'`, id)
	if err != nil {
		return workspace.Job{}, err
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return workspace.Job{}, workspace.ErrJobNotRetryable
	}
	if _, err = tx.ExecContext(ctx, `UPDATE articles a JOIN collection_jobs j ON j.article_id = a.id SET a.status = 'pending' WHERE j.id = ?`, id); err != nil {
		return workspace.Job{}, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO collection_job_events (job_id, status, message) VALUES (?, 'queued', '用户手动重试')`, id); err != nil {
		return workspace.Job{}, err
	}
	if err = tx.Commit(); err != nil {
		return workspace.Job{}, err
	}
	return s.GetJob(ctx, id, false)
}

func (s *Store) CompleteArticle(ctx context.Context, articleID, jobID uint64, collected workspace.CollectedArticle, rawKey string, contentHash [32]byte, assets []workspace.StoredAsset, warnings []string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var currentStatus string
	if err := tx.QueryRowContext(ctx, `SELECT status FROM collection_jobs WHERE id = ? FOR UPDATE`, jobID).Scan(&currentStatus); errors.Is(err, sql.ErrNoRows) {
		return workspace.ErrNotFound
	} else if err != nil {
		return err
	}
	if currentStatus == "completed" || currentStatus == "completed_with_warnings" {
		return tx.Commit()
	}
	if currentStatus != "storing_assets" {
		return workspace.ErrJobStateConflict
	}
	var duplicateID sql.NullInt64
	if err := tx.QueryRowContext(ctx, `SELECT id FROM articles WHERE content_hash = ? AND id <> ? AND status = 'ready' ORDER BY id LIMIT 1`, contentHash[:], articleID).Scan(&duplicateID); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM article_assets WHERE article_id = ?`, articleID); err != nil {
		return err
	}
	assetIDs := make(map[string]uint64, len(assets))
	for _, asset := range assets {
		if asset.DownloadStatus == "" {
			asset.DownloadStatus = "completed"
		}
		sourceHash := hashString(asset.SourceURL)
		result, execErr := tx.ExecContext(ctx, `INSERT INTO article_assets (article_id, source_url, source_url_hash, object_key, media_type, byte_size, width, height, sha256, position, is_cover, download_status) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, articleID, asset.SourceURL, sourceHash[:], asset.ObjectKey, asset.MediaType, asset.ByteSize, asset.Width, asset.Height, asset.SHA256[:], asset.Position, asset.IsCover, asset.DownloadStatus)
		if execErr != nil {
			return execErr
		}
		id, execErr := result.LastInsertId()
		if execErr != nil {
			return execErr
		}
		assetIDs[asset.SourceURL] = uint64(id)
	}
	for index := range collected.Blocks {
		if id, ok := assetIDs[collected.Blocks[index].SourceURL]; ok {
			collected.Blocks[index].AssetID = &id
		}
	}
	blocksJSON, err := json.Marshal(collected.Blocks)
	if err != nil {
		return err
	}
	metadataJSON, err := json.Marshal(collected.Metadata)
	if err != nil {
		return err
	}
	warningsJSON, err := json.Marshal(warnings)
	if err != nil {
		return err
	}
	var duplicate any
	if duplicateID.Valid {
		duplicate = uint64(duplicateID.Int64)
	}
	_, err = tx.ExecContext(ctx, `UPDATE articles SET title = ?, author = ?, source_name = ?, language = ?, published_at = ?, plain_text = ?, clean_html = ?, blocks_json = ?, metadata_json = ?, raw_object_key = ?, content_hash = ?, status = 'ready', duplicate_of_id = ? WHERE id = ?`, collected.Title, collected.Author, collected.SourceName, collected.Language, collected.PublishedAt, collected.PlainText, collected.CleanHTML, blocksJSON, metadataJSON, rawKey, contentHash[:], duplicate, articleID)
	if err != nil {
		return err
	}
	jobStatus := "completed"
	if len(warnings) > 0 {
		jobStatus = "completed_with_warnings"
	}
	if _, err = tx.ExecContext(ctx, `UPDATE collection_jobs SET status = ?, warnings_json = ?, error_code = '', error_message = '', finished_at = UTC_TIMESTAMP(3) WHERE id = ?`, jobStatus, warningsJSON, jobID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO collection_job_events (job_id, status, message) VALUES (?, ?, ?)`, jobID, jobStatus, "采集完成"); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) Dashboard(ctx context.Context) (workspace.Dashboard, error) {
	var result workspace.Dashboard
	queries := []struct {
		target *int64
		query  string
	}{
		{&result.ArticlesTotal, `SELECT COUNT(*) FROM articles`},
		{&result.CollectedToday, `SELECT COUNT(*) FROM collection_jobs WHERE status IN ('completed','completed_with_warnings') AND finished_at >= UTC_DATE()`},
		{&result.ProcessingJobs, `SELECT COUNT(*) FROM collection_jobs WHERE status IN ('queued','fetching','parsing','storing_assets')`},
		{&result.FailedJobs, `SELECT COUNT(*) FROM collection_jobs WHERE status = 'failed'`},
		{&result.SourceWechat, `SELECT COUNT(*) FROM articles WHERE source_type = 'wechat'`},
		{&result.SourceWeb, `SELECT COUNT(*) FROM articles WHERE source_type = 'web'`},
	}
	for _, item := range queries {
		if err := s.db.QueryRowContext(ctx, item.query).Scan(item.target); err != nil {
			return result, err
		}
	}
	page, err := s.ListArticles(ctx, "", "", "ready", 1, 6)
	if err != nil {
		return result, err
	}
	result.RecentArticles = page.Items
	return result, nil
}

type scanner interface{ Scan(...any) error }

func scanUser(row scanner) (workspace.User, error) {
	var user workspace.User
	err := row.Scan(&user.ID, &user.Username, &user.Nickname, &user.Avatar, &user.Role, &user.Status, &user.CreatedAt, &user.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return user, workspace.ErrNotFound
	}
	return user, err
}

func scanArticle(row scanner) (workspace.Article, error) {
	var article workspace.Article
	var published sql.NullTime
	var duplicate sql.NullInt64
	var blocksJSON, metadataJSON []byte
	err := row.Scan(&article.ID, &article.OriginalURL, &article.CanonicalURL, &article.SourceType, &article.Title, &article.Author, &article.SourceName, &article.Language, &published, &article.PlainText, &article.CleanHTML, &blocksJSON, &metadataJSON, &article.RawObjectKey, &article.Status, &duplicate, &article.CreatedBy, &article.CreatedAt, &article.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return article, workspace.ErrNotFound
	}
	if err != nil {
		return article, err
	}
	if published.Valid {
		article.PublishedAt = &published.Time
	}
	if duplicate.Valid {
		value := uint64(duplicate.Int64)
		article.DuplicateOfID = &value
	}
	article.Blocks = make([]workspace.Block, 0)
	article.Metadata = map[string]any{}
	if err := json.Unmarshal(blocksJSON, &article.Blocks); err != nil {
		return article, err
	}
	if err := json.Unmarshal(metadataJSON, &article.Metadata); err != nil {
		return article, err
	}
	return article, nil
}

func scanJob(row scanner) (workspace.Job, error) {
	var job workspace.Job
	var warningsJSON []byte
	var started, finished sql.NullTime
	err := row.Scan(&job.ID, &job.ArticleID, &job.SubmittedBy, &job.Status, &job.Attempts, &job.ManualRetries, &job.ErrorCode, &job.ErrorMessage, &warningsJSON, &started, &finished, &job.CreatedAt, &job.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return job, workspace.ErrNotFound
	}
	if err != nil {
		return job, err
	}
	job.Warnings = make([]string, 0)
	_ = json.Unmarshal(warningsJSON, &job.Warnings)
	if started.Valid {
		job.StartedAt = &started.Time
	}
	if finished.Valid {
		job.FinishedAt = &finished.Time
	}
	return job, nil
}

func getArticleByHash(ctx context.Context, db interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, hash [32]byte) (workspace.Article, error) {
	return scanArticle(db.QueryRowContext(ctx, `SELECT `+articleColumns+` FROM articles WHERE canonical_url_hash = ?`, hash[:]))
}

func getLatestJob(ctx context.Context, db interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, articleID uint64) (workspace.Job, error) {
	return scanJob(db.QueryRowContext(ctx, `SELECT `+jobColumns+` FROM collection_jobs WHERE article_id = ? ORDER BY id DESC LIMIT 1`, articleID))
}

func duplicate(err error) bool {
	var target *mysql.MySQLError
	return errors.As(err, &target) && target.Number == 1062
}

func hashString(value string) [32]byte {
	return sha256.Sum256([]byte(value))
}

func ClampPage(page, pageSize int) (int, int) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}
	return page, pageSize
}

func CleanKeyword(value string) string { return strings.TrimSpace(value) }
