package mysqlstore_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/pressly/goose/v3"

	"github.com/chenbb0128/weavepress/server/internal/modules/editorial"
	"github.com/chenbb0128/weavepress/server/internal/modules/workspace/mysqlstore"
)

const layoutMigrations = "../../../../database/migrations"

// Each test owns a fresh database. Existing development data is never migrated or deleted.
func layoutTestDatabase(t *testing.T, version int64) (*sql.DB, string) {
	t.Helper()
	raw := os.Getenv("WEAVEPRESS_TEST_MYSQL_ADMIN_DSN")
	if raw == "" {
		t.Skip("WEAVEPRESS_TEST_MYSQL_ADMIN_DSN is not set")
	}
	cfg, err := mysql.ParseDSN(raw)
	if err != nil {
		t.Fatal(err)
	}
	cfg.DBName = ""
	cfg.ParseTime = true
	admin, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	name := fmt.Sprintf("weavepress_layout_test_%d", time.Now().UnixNano())
	if _, err = admin.Exec("CREATE DATABASE " + name); err != nil {
		admin.Close()
		t.Fatal(err)
	}
	cfg.DBName = name
	db, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		db.Close()
		if _, err := admin.Exec("DROP DATABASE " + name); err != nil {
			t.Errorf("remove owned test database %s: %v", name, err)
		}
		admin.Close()
	})
	if err := goose.SetDialect("mysql"); err != nil {
		t.Fatal(err)
	}
	if err := goose.UpTo(db, layoutMigrations, version); err != nil {
		t.Fatal(err)
	}
	return db, cfg.FormatDSN()
}

func seedLayoutLegacy(t *testing.T, db *sql.DB) {
	t.Helper()
	for _, query := range []string{
		`INSERT INTO users (id, username, password_hash) VALUES (1, 'layout-test', 'unused')`,
		`INSERT INTO articles (id, original_url, canonical_url, canonical_url_hash, source_type, plain_text, clean_html, blocks_json, metadata_json, created_by, status)
		 VALUES (1, 'https://example.com', 'https://example.com', UNHEX(SHA2('article', 256)), 'web', '正文', '<p>正文</p>', '[]', '{}', 1, 'ready')`,
		`INSERT INTO article_assets (id, article_id, source_url, source_url_hash, object_key, media_type, byte_size, width, height, sha256, download_status)
		 VALUES (101, 1, 'https://example.com/1', UNHEX(SHA2('first', 256)), 'first.png', 'image/png', 100, 10, 10, UNHEX(SHA2('first', 256)), 'completed'),
		 (102, 1, 'https://example.com/2', UNHEX(SHA2('second', 256)), 'second.png', 'image/png', 100, 10, 10, UNHEX(SHA2('second', 256)), 'completed')`,
		`INSERT INTO drafts (id, source_article_id, title, content_html, cover_asset_id, created_by, updated_by, status)
		 VALUES (1, 1, '旧审核稿', '<p>正文</p>', 101, 1, 1, 'approved')`,
		`INSERT INTO draft_versions (draft_id, version, title, content_html, cover_asset_id, created_by)
		 VALUES (1, 1, '旧审核稿', '<p>正文</p>', 101, 1)`,
		`INSERT INTO draft_events (draft_id, actor_id, from_status, to_status, note)
		 VALUES (1, 1, 'in_review', 'approved', '旧审核通过')`,
	} {
		if _, err := db.Exec(query); err != nil {
			t.Fatal(err)
		}
	}
}

func TestDraftLayoutMigrationCompatibility(t *testing.T) {
	db, _ := layoutTestDatabase(t, 20260909000100)
	seedLayoutLegacy(t, db)
	if err := goose.Up(db, layoutMigrations); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"drafts", "draft_versions"} {
		var cover uint64
		if err := db.QueryRow("SELECT cover_asset_id FROM " + table + " WHERE id = 1").Scan(&cover); err != nil || cover != 101 {
			t.Fatalf("%s legacy cover = %d, want article asset 101; error = %v", table, cover, err)
		}
		// The old image can still write its original article-asset foreign key after Up.
		if _, err := db.Exec("UPDATE " + table + " SET cover_asset_id = 102 WHERE id = 1"); err != nil {
			t.Fatalf("old application write after Up: %v", err)
		}
	}
	store := mysqlstore.New(db)
	service := editorial.New(store, store, nil, nil, nil, false)
	draft, err := service.Get(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	var mapped uint64
	if err := db.QueryRow(`SELECT id FROM draft_assets WHERE draft_id = 1 AND article_asset_id = 102`).Scan(&mapped); err != nil {
		t.Fatal(err)
	}
	if draft.CoverAssetID == nil || *draft.CoverAssetID != mapped || !draft.MigrationNeeded {
		t.Fatalf("new reader after old cover write = %#v; mapped cover = %d", draft, mapped)
	}
	// Drafts created by an old image after Up have no backfilled cover or assets.
	if _, err := db.Exec(`INSERT INTO drafts (id, source_article_id, title, content_html, cover_asset_id, created_by, updated_by)
		VALUES (2, 1, '切换前新建稿', '<p>新正文</p>', 102, 1, 1)`); err != nil {
		t.Fatal(err)
	}
	created, err := service.Get(context.Background(), 2)
	if err != nil || created.CoverAssetID == nil {
		t.Fatalf("read draft created by old image = %#v, %v", created, err)
	}
	cover, err := store.GetDraftAsset(context.Background(), 2, *created.CoverAssetID)
	if err != nil || cover.ArticleAssetID == nil || *cover.ArticleAssetID != 102 {
		t.Fatalf("lazy cover mapping = %#v, %v", cover, err)
	}
	if err := goose.Down(db, layoutMigrations); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"drafts", "draft_versions"} {
		var cover uint64
		if err := db.QueryRow("SELECT cover_asset_id FROM " + table + " WHERE id = 1").Scan(&cover); err != nil || cover != 102 {
			t.Fatalf("%s cover after Down = %d, want article asset 102; error = %v", table, cover, err)
		}
	}
	if err := goose.Up(db, layoutMigrations); err != nil {
		t.Fatal(err)
	}
}

func TestDraftLayoutFreshInstall(t *testing.T) {
	_, dsn := layoutTestDatabase(t, 20260912000100)
	t.Setenv("WEAVEPRESS_TEST_MYSQL_DSN", dsn)
	t.Run("TestDraftLayoutPersistence", TestDraftLayoutPersistence)
	t.Run("TestMySQLIntegrationAIStore", TestMySQLIntegrationAIStore)
}

func TestLegacyReviewedDraftMigrationTransaction(t *testing.T) {
	for _, status := range []string{editorial.StatusApproved, editorial.StatusPublishFailed} {
		t.Run(status, func(t *testing.T) {
			db, _ := layoutTestDatabase(t, 20260909000100)
			seedLayoutLegacy(t, db)
			if err := goose.Up(db, layoutMigrations); err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec(`UPDATE drafts SET status = ? WHERE id = 1`, status); err != nil {
				t.Fatal(err)
			}
			store := mysqlstore.New(db)
			service := editorial.New(store, store, nil, nil, nil, true)
			ctx := context.Background()
			draft, err := service.Get(ctx, 1)
			if err != nil || !draft.MigrationNeeded || draft.EditorDocument == nil {
				t.Fatalf("legacy read = %#v, %v", draft, err)
			}
			if _, err := service.Publish(ctx, 1, 1); !errors.Is(err, editorial.ErrDraftPreflightFailed) {
				t.Fatalf("temporary conversion publish error = %v", err)
			}
			input := editorial.UpdateInput{Title: draft.Title, EditorDocument: draft.EditorDocument, ThemeID: draft.ThemeID, ThemeVersion: 1, CoverAssetID: draft.CoverAssetID, ExpectedVersion: 1}
			stale := input
			stale.ExpectedVersion = 2
			if _, err := service.Update(ctx, 1, 1, stale); !errors.Is(err, editorial.ErrDraftVersionConflict) {
				t.Fatalf("stale migration error = %v", err)
			}
			withoutDocument := input
			withoutDocument.EditorDocument = nil
			if _, err := store.UpdateDraft(ctx, 1, 1, withoutDocument); !errors.Is(err, editorial.ErrDraftNotEditable) {
				t.Fatalf("non-structured store update bypassed state: %v", err)
			}
			// Audit failure must roll back the document, version and old approval together.
			if _, err := db.Exec(`CREATE TRIGGER reject_migration_audit BEFORE INSERT ON draft_events FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'audit unavailable'`); err != nil {
				t.Fatal(err)
			}
			if _, err := service.Update(ctx, 1, 1, input); err == nil || !strings.Contains(err.Error(), "audit unavailable") {
				t.Fatalf("audit failure was not propagated: %v", err)
			}
			unchanged, err := store.GetDraft(ctx, 1, true)
			versions, versionErr := store.ListDraftVersions(ctx, 1)
			if err != nil || versionErr != nil || unchanged.Status != status || unchanged.CurrentVersion != 1 || unchanged.EditorDocument != nil || len(unchanged.Events) != 1 || len(versions) != 1 {
				t.Fatalf("failed migration was not atomic: draft=%#v versions=%#v errors=%v/%v", unchanged, versions, err, versionErr)
			}
			if _, err := db.Exec(`DROP TRIGGER reject_migration_audit`); err != nil {
				t.Fatal(err)
			}
			saved, err := service.Update(ctx, 1, 1, input)
			if err != nil {
				t.Fatalf("explicit structured migration save: %v", err)
			}
			if saved.Status != editorial.StatusEditing || saved.CurrentVersion != 2 || saved.EditorDocument == nil || saved.MigrationNeeded {
				t.Fatalf("saved migration = %#v", saved)
			}
			if len(saved.Events) != 2 || saved.Events[1].FromStatus != status || saved.Events[1].ToStatus != editorial.StatusEditing || saved.Events[1].ActorID != 1 || !strings.Contains(saved.Events[1].Note, "重新审核") {
				t.Fatalf("migration audit = %#v", saved.Events)
			}
			if _, err := store.CreatePublishJob(ctx, 1, 1); !errors.Is(err, editorial.ErrDraftStateConflict) {
				t.Fatalf("migrated draft must require another review: %v", err)
			}
			// A saved structured draft cannot reuse the legacy exception.
			if _, err := db.Exec(`UPDATE drafts SET status = ? WHERE id = 1`, status); err != nil {
				t.Fatal(err)
			}
			input.ExpectedVersion = 2
			if _, err := service.Update(ctx, 1, 1, input); !errors.Is(err, editorial.ErrDraftNotEditable) {
				t.Fatalf("ordinary reviewed draft save error = %v", err)
			}
			for _, locked := range []string{editorial.StatusInReview, editorial.StatusPublishing, editorial.StatusPublished} {
				if _, err := db.Exec(`UPDATE drafts SET status = ?, editor_document = NULL WHERE id = 1`, locked); err != nil {
					t.Fatal(err)
				}
				if _, err := service.Update(ctx, 1, 1, input); !errors.Is(err, editorial.ErrDraftNotEditable) {
					t.Fatalf("legacy %s must remain locked: %v", locked, err)
				}
			}
		})
	}
}
