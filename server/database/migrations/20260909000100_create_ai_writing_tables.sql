-- +goose Up
CREATE TABLE ai_jobs (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    job_type VARCHAR(32) NOT NULL,
    article_id BIGINT UNSIGNED NOT NULL,
    parent_job_id BIGINT UNSIGNED NULL,
    requested_by BIGINT UNSIGNED NOT NULL,
    status VARCHAR(32) NOT NULL DEFAULT 'queued',
    attempts INT UNSIGNED NOT NULL DEFAULT 0,
    manual_retries INT UNSIGNED NOT NULL DEFAULT 0,
    reuse_key BINARY(32) NULL,
    idempotency_hash BINARY(32) NULL,
    input_fingerprint BINARY(32) NOT NULL,
    provider VARCHAR(64) NOT NULL,
    model VARCHAR(255) NOT NULL,
    prompt_version VARCHAR(64) NOT NULL,
    input_tokens INT UNSIGNED NOT NULL DEFAULT 0,
    output_tokens INT UNSIGNED NOT NULL DEFAULT 0,
    total_tokens INT UNSIGNED NOT NULL DEFAULT 0,
    error_code VARCHAR(64) NOT NULL DEFAULT '',
    error_message VARCHAR(1024) NOT NULL DEFAULT '',
    retryable BOOLEAN NOT NULL DEFAULT FALSE,
    started_at DATETIME(3) NULL,
    finished_at DATETIME(3) NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    UNIQUE KEY uk_ai_jobs_reuse_key (reuse_key),
    UNIQUE KEY uk_ai_jobs_user_idempotency (requested_by, idempotency_hash),
    KEY idx_ai_jobs_status_created (status, created_at),
    KEY idx_ai_jobs_article_created (article_id, created_at),
    CONSTRAINT fk_ai_jobs_article FOREIGN KEY (article_id) REFERENCES articles (id),
    CONSTRAINT fk_ai_jobs_parent FOREIGN KEY (parent_job_id) REFERENCES ai_jobs (id),
    CONSTRAINT fk_ai_jobs_user FOREIGN KEY (requested_by) REFERENCES users (id),
    CONSTRAINT chk_ai_jobs_type CHECK (job_type IN ('analysis', 'generation')),
    CONSTRAINT chk_ai_jobs_status CHECK (status IN ('queued', 'running', 'completed', 'failed'))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE ai_analyses (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    job_id BIGINT UNSIGNED NOT NULL,
    article_id BIGINT UNSIGNED NOT NULL,
    summary TEXT NOT NULL,
    facts_json JSON NOT NULL,
    viewpoints_json JSON NOT NULL,
    quotes_json JSON NOT NULL,
    risks_json JSON NOT NULL,
    angles_json JSON NOT NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    UNIQUE KEY uk_ai_analyses_job (job_id),
    KEY idx_ai_analyses_article_created (article_id, created_at),
    CONSTRAINT fk_ai_analyses_job FOREIGN KEY (job_id) REFERENCES ai_jobs (id),
    CONSTRAINT fk_ai_analyses_article FOREIGN KEY (article_id) REFERENCES articles (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE ai_generations (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    job_id BIGINT UNSIGNED NOT NULL,
    analysis_id BIGINT UNSIGNED NOT NULL,
    angle_id VARCHAR(64) NOT NULL,
    audience VARCHAR(255) NOT NULL,
    tone VARCHAR(32) NOT NULL,
    target_words INT UNSIGNED NOT NULL,
    additional_instructions VARCHAR(2000) NOT NULL DEFAULT '',
    title VARCHAR(255) NOT NULL DEFAULT '',
    digest VARCHAR(255) NOT NULL DEFAULT '',
    blocks_json JSON NOT NULL,
    fact_map_json JSON NOT NULL,
    content_html LONGTEXT NOT NULL,
    draft_id BIGINT UNSIGNED NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    UNIQUE KEY uk_ai_generations_job (job_id),
    UNIQUE KEY uk_ai_generations_draft (draft_id),
    KEY idx_ai_generations_analysis_created (analysis_id, created_at),
    CONSTRAINT fk_ai_generations_job FOREIGN KEY (job_id) REFERENCES ai_jobs (id),
    CONSTRAINT fk_ai_generations_analysis FOREIGN KEY (analysis_id) REFERENCES ai_analyses (id),
    CONSTRAINT fk_ai_generations_draft FOREIGN KEY (draft_id) REFERENCES drafts (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE ai_job_events (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    job_id BIGINT UNSIGNED NOT NULL,
    status VARCHAR(64) NOT NULL,
    message VARCHAR(1024) NOT NULL DEFAULT '',
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    KEY idx_ai_job_events_job_created (job_id, created_at),
    CONSTRAINT fk_ai_job_events_job FOREIGN KEY (job_id) REFERENCES ai_jobs (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

-- +goose Down
DROP TABLE ai_job_events;
DROP TABLE ai_generations;
DROP TABLE ai_analyses;
DROP TABLE ai_jobs;
