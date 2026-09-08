package mysqlstore_test

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"

	"github.com/chenbb0128/weavepress/server/internal/modules/workspace"
	"github.com/chenbb0128/weavepress/server/internal/modules/workspace/mysqlstore"
	"github.com/chenbb0128/weavepress/server/internal/platform/database"
)

func TestMySQLIntegrationCollectionStateAndIdempotency(t *testing.T) {
	rawDSN := os.Getenv("WEAVEPRESS_TEST_MYSQL_DSN")
	if rawDSN == "" {
		t.Skip("WEAVEPRESS_TEST_MYSQL_DSN is not set")
	}
	dsn, err := database.NormalizeMySQLDSN(rawDSN)
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		t.Fatal(err)
	}

	store := mysqlstore.New(db)
	suffix := time.Now().UnixNano()
	user, err := store.CreateUser(ctx, fmt.Sprintf("integration_%d", suffix), "unused", "集成测试", workspace.RoleEditor, "active")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = db.Exec(`UPDATE articles SET duplicate_of_id = NULL WHERE created_by = ?`, user.ID)
		_, _ = db.Exec(`DELETE FROM articles WHERE created_by = ?`, user.ID)
		_, _ = db.Exec(`DELETE FROM auth_refresh_tokens WHERE user_id = ?`, user.ID)
		_, _ = db.Exec(`DELETE FROM users WHERE id = ?`, user.ID)
	})

	family := fmt.Sprintf("%032x", suffix)
	oldRefresh := sha256.Sum256([]byte(fmt.Sprintf("old-refresh-%d", suffix)))
	newRefresh := sha256.Sum256([]byte(fmt.Sprintf("new-refresh-%d", suffix)))
	if err := store.CreateRefreshToken(ctx, user.ID, family, oldRefresh, time.Now().Add(time.Hour), "integration", "127.0.0.1"); err != nil {
		t.Fatal(err)
	}
	rotatedUserID, err := store.RotateRefreshToken(ctx, oldRefresh, "", newRefresh, time.Now().Add(time.Hour), "integration", "127.0.0.1")
	if err != nil || rotatedUserID != user.ID {
		t.Fatalf("refresh rotation user=%d err=%v", rotatedUserID, err)
	}
	if _, err := store.RotateRefreshToken(ctx, oldRefresh, "", sha256.Sum256([]byte("reuse")), time.Now().Add(time.Hour), "integration", "127.0.0.1"); !errors.Is(err, workspace.ErrRefreshReuse) {
		t.Fatalf("refresh reuse error = %v", err)
	}
	var activeFamilyTokens int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM auth_refresh_tokens WHERE family_id = ? AND revoked_at IS NULL`, family).Scan(&activeFamilyTokens); err != nil {
		t.Fatal(err)
	}
	if activeFamilyTokens != 0 {
		t.Fatalf("active tokens after family reuse = %d, want 0", activeFamilyTokens)
	}

	canonical := fmt.Sprintf("https://example.com/integration/%d", suffix)
	canonicalHash := sha256.Sum256([]byte(canonical))
	type result struct {
		articleID uint64
		jobID     uint64
		reused    bool
		err       error
	}
	results := make(chan result, 8)
	var wait sync.WaitGroup
	for range 8 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			article, job, reused, err := store.CreateArticleJob(ctx, canonical, canonical, canonicalHash, "web", user.ID)
			results <- result{articleID: article.ID, jobID: job.ID, reused: reused, err: err}
		}()
	}
	wait.Wait()
	close(results)
	var articleID, jobID uint64
	var created atomic.Int32
	for item := range results {
		if item.err != nil {
			t.Fatal(item.err)
		}
		if articleID == 0 {
			articleID, jobID = item.articleID, item.jobID
		}
		if item.articleID != articleID || item.jobID != jobID {
			t.Fatalf("concurrent submit returned different records: article=%d job=%d", item.articleID, item.jobID)
		}
		if !item.reused {
			created.Add(1)
		}
	}
	if created.Load() != 1 {
		t.Fatalf("created count = %d, want 1", created.Load())
	}

	if err := store.SetJobStage(ctx, jobID, "parsing", "invalid transition"); !errors.Is(err, workspace.ErrJobStateConflict) {
		t.Fatalf("invalid transition error = %v", err)
	}
	for _, stage := range []string{"fetching", "parsing", "storing_assets"} {
		if err := store.SetJobStage(ctx, jobID, stage, stage); err != nil {
			t.Fatal(err)
		}
	}
	body := fmt.Sprintf("用于验证正文哈希去重的集成测试正文 %d。", suffix)
	collected := workspace.CollectedArticle{Title: "集成测试文章", PlainText: body, CleanHTML: "<p>" + body + "</p>", Blocks: []workspace.Block{{Type: "paragraph", Text: body}}, Metadata: map[string]any{"test": true}}
	contentHash := sha256.Sum256([]byte(collected.PlainText))
	if err := store.CompleteArticle(ctx, articleID, jobID, collected, "raw/test.html.gz", contentHash, nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := store.CompleteArticle(ctx, articleID, jobID, collected, "raw/test.html.gz", contentHash, nil, nil); err != nil {
		t.Fatalf("idempotent completion failed: %v", err)
	}
	if err := store.SetJobFailure(ctx, jobID, "LATE_ERROR", "late duplicate delivery", false); err != nil {
		t.Fatal(err)
	}
	completed, err := store.GetJob(ctx, jobID, false)
	if err != nil || completed.Status != "completed" {
		t.Fatalf("completed job was overwritten: status=%q err=%v", completed.Status, err)
	}
	if _, err := store.RetryJob(ctx, jobID); !errors.Is(err, workspace.ErrJobNotRetryable) {
		t.Fatalf("completed retry error = %v", err)
	}

	secondCanonical := canonical + "/same-content"
	secondHash := sha256.Sum256([]byte(secondCanonical))
	secondArticle, secondJob, _, err := store.CreateArticleJob(ctx, secondCanonical, secondCanonical, secondHash, "web", user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetJobStage(ctx, secondJob.ID, "fetching", "fetching"); err != nil {
		t.Fatal(err)
	}
	if err := store.SetJobFailure(ctx, secondJob.ID, "HTTP_503", "temporary", false); err != nil {
		t.Fatal(err)
	}
	if _, err := store.RetryJob(ctx, secondJob.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.RetryJob(ctx, secondJob.ID); !errors.Is(err, workspace.ErrJobNotRetryable) {
		t.Fatalf("parallel manual retry guard error = %v", err)
	}
	for _, stage := range []string{"fetching", "parsing", "storing_assets"} {
		if err := store.SetJobStage(ctx, secondJob.ID, stage, stage); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.CompleteArticle(ctx, secondArticle.ID, secondJob.ID, collected, "raw/test-duplicate.html.gz", contentHash, nil, []string{"asset warning"}); err != nil {
		t.Fatal(err)
	}
	duplicate, err := store.GetArticle(ctx, secondArticle.ID)
	if err != nil || duplicate.DuplicateOfID == nil || *duplicate.DuplicateOfID != articleID {
		var duplicateOf uint64
		if duplicate.DuplicateOfID != nil {
			duplicateOf = *duplicate.DuplicateOfID
		}
		t.Fatalf("content duplicate link = %d, want %d, err=%v", duplicateOf, articleID, err)
	}
}
