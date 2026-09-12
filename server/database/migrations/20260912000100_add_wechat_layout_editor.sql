-- +goose Up
ALTER TABLE drafts
    ADD COLUMN editor_document JSON NULL AFTER content_html,
    ADD COLUMN theme_id VARCHAR(64) NOT NULL DEFAULT 'minimal-business' AFTER editor_document,
    ADD COLUMN theme_version INT UNSIGNED NOT NULL DEFAULT 1 AFTER theme_id;

ALTER TABLE draft_versions
    ADD COLUMN editor_document JSON NULL AFTER content_html,
    ADD COLUMN theme_id VARCHAR(64) NOT NULL DEFAULT 'minimal-business' AFTER editor_document,
    ADD COLUMN theme_version INT UNSIGNED NOT NULL DEFAULT 1 AFTER theme_id;

CREATE TABLE draft_assets (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    draft_id BIGINT UNSIGNED NOT NULL,
    origin VARCHAR(16) NOT NULL,
    article_asset_id BIGINT UNSIGNED NULL,
    object_key VARCHAR(1024) NOT NULL,
    media_type VARCHAR(128) NOT NULL,
    byte_size BIGINT UNSIGNED NOT NULL,
    width INT UNSIGNED NOT NULL DEFAULT 0,
    height INT UNSIGNED NOT NULL DEFAULT 0,
    sha256 BINARY(32) NOT NULL,
    uploaded_by BIGINT UNSIGNED NOT NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    UNIQUE KEY uk_draft_assets_article (draft_id, article_asset_id),
    KEY idx_draft_assets_draft_created (draft_id, created_at),
    KEY idx_draft_assets_sha256 (sha256),
    CONSTRAINT fk_draft_assets_draft FOREIGN KEY (draft_id) REFERENCES drafts (id) ON DELETE CASCADE,
    CONSTRAINT fk_draft_assets_article FOREIGN KEY (article_asset_id) REFERENCES article_assets (id),
    CONSTRAINT fk_draft_assets_user FOREIGN KEY (uploaded_by) REFERENCES users (id),
    CONSTRAINT chk_draft_assets_origin CHECK (
        (origin = 'article' AND article_asset_id IS NOT NULL) OR
        (origin = 'upload' AND article_asset_id IS NULL)
    )
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

INSERT INTO draft_assets
    (draft_id, origin, article_asset_id, object_key, media_type, byte_size, width, height, sha256, uploaded_by)
SELECT d.id, 'article', aa.id, aa.object_key, aa.media_type, aa.byte_size,
       aa.width, aa.height, aa.sha256, d.created_by
FROM drafts d
JOIN article_assets aa ON aa.article_id = d.source_article_id
WHERE aa.download_status = 'completed' AND aa.object_key <> '';

ALTER TABLE draft_versions DROP FOREIGN KEY fk_draft_versions_cover;
ALTER TABLE drafts DROP FOREIGN KEY fk_drafts_cover;

UPDATE drafts d
LEFT JOIN draft_assets da
  ON da.draft_id = d.id AND da.article_asset_id = d.cover_asset_id
SET d.cover_asset_id = da.id;

UPDATE draft_versions dv
LEFT JOIN draft_assets da
  ON da.draft_id = dv.draft_id AND da.article_asset_id = dv.cover_asset_id
SET dv.cover_asset_id = da.id;

ALTER TABLE drafts
    ADD CONSTRAINT fk_drafts_cover_draft_asset
    FOREIGN KEY (cover_asset_id) REFERENCES draft_assets (id) ON DELETE SET NULL;
ALTER TABLE draft_versions
    ADD CONSTRAINT fk_draft_versions_cover_draft_asset
    FOREIGN KEY (cover_asset_id) REFERENCES draft_assets (id) ON DELETE SET NULL;

-- +goose Down
ALTER TABLE draft_versions DROP FOREIGN KEY fk_draft_versions_cover_draft_asset;
ALTER TABLE drafts DROP FOREIGN KEY fk_drafts_cover_draft_asset;
UPDATE draft_versions dv LEFT JOIN draft_assets da ON da.id = dv.cover_asset_id
SET dv.cover_asset_id = da.article_asset_id;
UPDATE drafts d LEFT JOIN draft_assets da ON da.id = d.cover_asset_id
SET d.cover_asset_id = da.article_asset_id;
ALTER TABLE drafts ADD CONSTRAINT fk_drafts_cover FOREIGN KEY (cover_asset_id) REFERENCES article_assets (id);
ALTER TABLE draft_versions ADD CONSTRAINT fk_draft_versions_cover FOREIGN KEY (cover_asset_id) REFERENCES article_assets (id);
DROP TABLE draft_assets;
ALTER TABLE draft_versions DROP COLUMN theme_version, DROP COLUMN theme_id, DROP COLUMN editor_document;
ALTER TABLE drafts DROP COLUMN theme_version, DROP COLUMN theme_id, DROP COLUMN editor_document;
