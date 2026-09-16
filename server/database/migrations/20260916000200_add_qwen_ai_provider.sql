-- +goose Up
ALTER TABLE ai_provider_settings
    DROP CHECK chk_ai_provider_settings_provider,
    ADD CONSTRAINT chk_ai_provider_settings_provider
        CHECK (provider IN ('zhipu', 'openai', 'qwen', 'openai-compatible'));

ALTER TABLE ai_runtime_settings
    DROP CHECK chk_ai_runtime_settings_provider,
    ADD CONSTRAINT chk_ai_runtime_settings_provider
        CHECK (active_provider IN ('zhipu', 'openai', 'qwen', 'openai-compatible'));

-- +goose Down
ALTER TABLE ai_runtime_settings
    DROP CHECK chk_ai_runtime_settings_provider,
    ADD CONSTRAINT chk_ai_runtime_settings_provider
        CHECK (active_provider IN ('zhipu', 'openai', 'openai-compatible'));

ALTER TABLE ai_provider_settings
    DROP CHECK chk_ai_provider_settings_provider,
    ADD CONSTRAINT chk_ai_provider_settings_provider
        CHECK (provider IN ('zhipu', 'openai', 'openai-compatible'));
