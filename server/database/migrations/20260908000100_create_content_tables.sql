-- +goose Up
CREATE TABLE auth_refresh_tokens (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    user_id BIGINT UNSIGNED NOT NULL,
    family_id CHAR(32) NOT NULL,
    token_hash BINARY(32) NOT NULL,
    replaced_by_hash BINARY(32) NULL,
    user_agent VARCHAR(512) NOT NULL DEFAULT '',
    ip_address VARCHAR(64) NOT NULL DEFAULT '',
    expires_at DATETIME(3) NOT NULL,
    revoked_at DATETIME(3) NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    UNIQUE KEY uk_refresh_tokens_hash (token_hash),
    KEY idx_refresh_tokens_user (user_id),
    KEY idx_refresh_tokens_family (family_id),
    CONSTRAINT fk_refresh_tokens_user FOREIGN KEY (user_id) REFERENCES users (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE articles (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    original_url TEXT NOT NULL,
    canonical_url TEXT NOT NULL,
    canonical_url_hash BINARY(32) NOT NULL,
    source_type VARCHAR(32) NOT NULL,
    title VARCHAR(512) NOT NULL DEFAULT '',
    author VARCHAR(255) NOT NULL DEFAULT '',
    source_name VARCHAR(255) NOT NULL DEFAULT '',
    language VARCHAR(32) NOT NULL DEFAULT '',
    published_at DATETIME(3) NULL,
    plain_text LONGTEXT NOT NULL,
    clean_html LONGTEXT NOT NULL,
    blocks_json JSON NOT NULL,
    metadata_json JSON NOT NULL,
    raw_object_key VARCHAR(1024) NOT NULL DEFAULT '',
    content_hash BINARY(32) NULL,
    status VARCHAR(32) NOT NULL DEFAULT 'pending',
    duplicate_of_id BIGINT UNSIGNED NULL,
    created_by BIGINT UNSIGNED NOT NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    UNIQUE KEY uk_articles_canonical_url_hash (canonical_url_hash),
    KEY idx_articles_status_created (status, created_at),
    KEY idx_articles_source_published (source_type, published_at),
    KEY idx_articles_content_hash (content_hash),
    CONSTRAINT fk_articles_duplicate FOREIGN KEY (duplicate_of_id) REFERENCES articles (id),
    CONSTRAINT fk_articles_created_by FOREIGN KEY (created_by) REFERENCES users (id),
    CONSTRAINT chk_articles_source_type CHECK (source_type IN ('wechat', 'web')),
    CONSTRAINT chk_articles_status CHECK (status IN ('pending', 'processing', 'ready', 'failed'))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE article_assets (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    article_id BIGINT UNSIGNED NOT NULL,
    source_url TEXT NOT NULL,
    source_url_hash BINARY(32) NOT NULL,
    object_key VARCHAR(1024) NOT NULL DEFAULT '',
    media_type VARCHAR(128) NOT NULL DEFAULT '',
    byte_size BIGINT UNSIGNED NOT NULL DEFAULT 0,
    width INT UNSIGNED NOT NULL DEFAULT 0,
    height INT UNSIGNED NOT NULL DEFAULT 0,
    sha256 BINARY(32) NOT NULL,
    position INT UNSIGNED NOT NULL DEFAULT 0,
    is_cover BOOLEAN NOT NULL DEFAULT FALSE,
    download_status VARCHAR(32) NOT NULL DEFAULT 'pending',
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    UNIQUE KEY uk_article_assets_source (article_id, source_url_hash),
    KEY idx_article_assets_sha256 (sha256),
    CONSTRAINT fk_article_assets_article FOREIGN KEY (article_id) REFERENCES articles (id) ON DELETE CASCADE,
    CONSTRAINT chk_article_assets_download_status CHECK (download_status IN ('pending', 'completed', 'failed'))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE collection_jobs (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    article_id BIGINT UNSIGNED NOT NULL,
    submitted_by BIGINT UNSIGNED NOT NULL,
    status VARCHAR(64) NOT NULL DEFAULT 'queued',
    attempts INT UNSIGNED NOT NULL DEFAULT 0,
    manual_retries INT UNSIGNED NOT NULL DEFAULT 0,
    error_code VARCHAR(64) NOT NULL DEFAULT '',
    error_message VARCHAR(1024) NOT NULL DEFAULT '',
    warnings_json JSON NOT NULL,
    started_at DATETIME(3) NULL,
    finished_at DATETIME(3) NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    KEY idx_collection_jobs_article (article_id, created_at),
    KEY idx_collection_jobs_status_created (status, created_at),
    CONSTRAINT fk_collection_jobs_article FOREIGN KEY (article_id) REFERENCES articles (id) ON DELETE CASCADE,
    CONSTRAINT fk_collection_jobs_user FOREIGN KEY (submitted_by) REFERENCES users (id),
    CONSTRAINT chk_collection_jobs_status CHECK (status IN ('queued', 'fetching', 'parsing', 'storing_assets', 'completed', 'completed_with_warnings', 'failed'))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE collection_job_events (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    job_id BIGINT UNSIGNED NOT NULL,
    status VARCHAR(64) NOT NULL,
    message VARCHAR(1024) NOT NULL DEFAULT '',
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    KEY idx_collection_job_events_job (job_id, created_at),
    CONSTRAINT fk_collection_job_events_job FOREIGN KEY (job_id) REFERENCES collection_jobs (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

-- +goose Down
DROP TABLE collection_job_events;
DROP TABLE collection_jobs;
DROP TABLE article_assets;
DROP TABLE articles;
DROP TABLE auth_refresh_tokens;
