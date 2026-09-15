-- +goose Up
CREATE TABLE ai_provider_settings (
    provider VARCHAR(32) NOT NULL,
    base_url VARCHAR(500) NOT NULL,
    model VARCHAR(100) NOT NULL,
    api_key_ciphertext VARBINARY(4096) NULL,
    updated_by BIGINT UNSIGNED NOT NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (provider),
    CONSTRAINT fk_ai_provider_settings_user FOREIGN KEY (updated_by) REFERENCES users (id),
    CONSTRAINT chk_ai_provider_settings_provider CHECK (provider IN ('zhipu', 'openai', 'openai-compatible'))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE ai_runtime_settings (
    id TINYINT UNSIGNED NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT FALSE,
    active_provider VARCHAR(32) NOT NULL,
    updated_by BIGINT UNSIGNED NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    CONSTRAINT fk_ai_runtime_settings_user FOREIGN KEY (updated_by) REFERENCES users (id),
    CONSTRAINT chk_ai_runtime_settings_singleton CHECK (id = 1),
    CONSTRAINT chk_ai_runtime_settings_provider CHECK (active_provider IN ('zhipu', 'openai', 'openai-compatible'))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

INSERT INTO ai_runtime_settings (id, enabled, active_provider, updated_by)
VALUES (1, FALSE, 'zhipu', NULL);

-- +goose Down
DROP TABLE ai_runtime_settings;
DROP TABLE ai_provider_settings;
