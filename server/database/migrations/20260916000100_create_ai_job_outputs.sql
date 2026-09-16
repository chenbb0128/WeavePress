-- +goose Up
CREATE TABLE ai_job_outputs (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    job_id BIGINT UNSIGNED NOT NULL,
    stage VARCHAR(16) NOT NULL,
    content MEDIUMTEXT NOT NULL,
    validation_error TEXT NOT NULL,
    truncated BOOLEAN NOT NULL DEFAULT FALSE,
    input_tokens INT UNSIGNED NOT NULL DEFAULT 0,
    output_tokens INT UNSIGNED NOT NULL DEFAULT 0,
    total_tokens INT UNSIGNED NOT NULL DEFAULT 0,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    UNIQUE KEY uk_ai_job_outputs_job_stage (job_id, stage),
    CONSTRAINT fk_ai_job_outputs_job FOREIGN KEY (job_id) REFERENCES ai_jobs (id) ON DELETE CASCADE,
    CONSTRAINT chk_ai_job_outputs_stage CHECK (stage IN ('initial', 'repair'))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

-- +goose Down
DROP TABLE ai_job_outputs;
