-- +goose Up
CREATE TABLE drafts (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    source_article_id BIGINT UNSIGNED NOT NULL,
    title VARCHAR(255) NOT NULL,
    author VARCHAR(64) NOT NULL DEFAULT '',
    digest VARCHAR(255) NOT NULL DEFAULT '',
    content_html LONGTEXT NOT NULL,
    cover_asset_id BIGINT UNSIGNED NULL,
    status VARCHAR(32) NOT NULL DEFAULT 'editing',
    current_version INT UNSIGNED NOT NULL DEFAULT 1,
    created_by BIGINT UNSIGNED NOT NULL,
    updated_by BIGINT UNSIGNED NOT NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    KEY idx_drafts_status_updated (status, updated_at),
    KEY idx_drafts_source_article (source_article_id, created_at),
    CONSTRAINT fk_drafts_article FOREIGN KEY (source_article_id) REFERENCES articles (id),
    CONSTRAINT fk_drafts_cover FOREIGN KEY (cover_asset_id) REFERENCES article_assets (id),
    CONSTRAINT fk_drafts_created_by FOREIGN KEY (created_by) REFERENCES users (id),
    CONSTRAINT fk_drafts_updated_by FOREIGN KEY (updated_by) REFERENCES users (id),
    CONSTRAINT chk_drafts_status CHECK (status IN ('editing', 'in_review', 'approved', 'publishing', 'published', 'publish_failed'))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE draft_versions (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    draft_id BIGINT UNSIGNED NOT NULL,
    version INT UNSIGNED NOT NULL,
    title VARCHAR(255) NOT NULL,
    author VARCHAR(64) NOT NULL DEFAULT '',
    digest VARCHAR(255) NOT NULL DEFAULT '',
    content_html LONGTEXT NOT NULL,
    cover_asset_id BIGINT UNSIGNED NULL,
    change_note VARCHAR(255) NOT NULL DEFAULT '',
    created_by BIGINT UNSIGNED NOT NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    UNIQUE KEY uk_draft_versions_version (draft_id, version),
    CONSTRAINT fk_draft_versions_draft FOREIGN KEY (draft_id) REFERENCES drafts (id) ON DELETE CASCADE,
    CONSTRAINT fk_draft_versions_cover FOREIGN KEY (cover_asset_id) REFERENCES article_assets (id),
    CONSTRAINT fk_draft_versions_user FOREIGN KEY (created_by) REFERENCES users (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE draft_events (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    draft_id BIGINT UNSIGNED NOT NULL,
    actor_id BIGINT UNSIGNED NOT NULL,
    from_status VARCHAR(32) NOT NULL DEFAULT '',
    to_status VARCHAR(32) NOT NULL,
    note VARCHAR(255) NOT NULL DEFAULT '',
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    KEY idx_draft_events_draft (draft_id, created_at),
    CONSTRAINT fk_draft_events_draft FOREIGN KEY (draft_id) REFERENCES drafts (id) ON DELETE CASCADE,
    CONSTRAINT fk_draft_events_actor FOREIGN KEY (actor_id) REFERENCES users (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE wechat_publish_jobs (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    draft_id BIGINT UNSIGNED NOT NULL,
    requested_by BIGINT UNSIGNED NOT NULL,
    status VARCHAR(32) NOT NULL DEFAULT 'queued',
    attempts INT UNSIGNED NOT NULL DEFAULT 0,
    manual_retries INT UNSIGNED NOT NULL DEFAULT 0,
    remote_media_id VARCHAR(255) NOT NULL DEFAULT '',
    error_code VARCHAR(64) NOT NULL DEFAULT '',
    error_message VARCHAR(1024) NOT NULL DEFAULT '',
    started_at DATETIME(3) NULL,
    finished_at DATETIME(3) NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    KEY idx_wechat_publish_jobs_draft (draft_id, created_at),
    KEY idx_wechat_publish_jobs_status (status, created_at),
    CONSTRAINT fk_wechat_publish_jobs_draft FOREIGN KEY (draft_id) REFERENCES drafts (id) ON DELETE CASCADE,
    CONSTRAINT fk_wechat_publish_jobs_user FOREIGN KEY (requested_by) REFERENCES users (id),
    CONSTRAINT chk_wechat_publish_jobs_status CHECK (status IN ('queued', 'publishing', 'completed', 'failed'))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE wechat_publish_job_events (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    job_id BIGINT UNSIGNED NOT NULL,
    status VARCHAR(32) NOT NULL,
    message VARCHAR(1024) NOT NULL DEFAULT '',
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    KEY idx_wechat_publish_job_events_job (job_id, created_at),
    CONSTRAINT fk_wechat_publish_job_events_job FOREIGN KEY (job_id) REFERENCES wechat_publish_jobs (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

-- +goose Down
DROP TABLE wechat_publish_job_events;
DROP TABLE wechat_publish_jobs;
DROP TABLE draft_events;
DROP TABLE draft_versions;
DROP TABLE drafts;
