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

	"github.com/chenbb0128/weavepress/server/internal/modules/editorial"
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

func TestMySQLIntegrationEditorialWorkflow(t *testing.T) {
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
	user, err := store.CreateUser(ctx, fmt.Sprintf("editorial_%d", suffix), "unused", "稿件集成测试", workspace.RoleAdmin, "active")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM drafts WHERE created_by = ?`, user.ID)
		_, _ = db.Exec(`UPDATE articles SET duplicate_of_id = NULL WHERE created_by = ?`, user.ID)
		_, _ = db.Exec(`DELETE FROM articles WHERE created_by = ?`, user.ID)
		_, _ = db.Exec(`DELETE FROM users WHERE id = ?`, user.ID)
	})

	canonical := fmt.Sprintf("https://example.com/editorial/%d", suffix)
	article, job, _, err := store.CreateArticleJob(ctx, canonical, canonical, sha256.Sum256([]byte(canonical)), "web", user.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, stage := range []string{"fetching", "parsing", "storing_assets"} {
		if err := store.SetJobStage(ctx, job.ID, stage, stage); err != nil {
			t.Fatal(err)
		}
	}
	imageURL := canonical + "/cover.jpg"
	imageHash := sha256.Sum256([]byte("cover"))
	collected := workspace.CollectedArticle{
		Title: "稿件集成测试", Author: "作者", PlainText: "正文", CleanHTML: "<p>正文</p>",
		Blocks:   []workspace.Block{{Type: "paragraph", Text: "正文"}, {Type: "image", SourceURL: imageURL, Alt: "封面"}},
		Metadata: map[string]any{"test": true},
	}
	assets := []workspace.StoredAsset{{SourceURL: imageURL, ObjectKey: "articles/test-cover.jpg", MediaType: "image/jpeg", ByteSize: 5, Position: 1, IsCover: true, DownloadStatus: "completed", SHA256: imageHash}}
	if err := store.CompleteArticle(ctx, article.ID, job.ID, collected, "raw/editorial.html.gz", sha256.Sum256([]byte(collected.PlainText+fmt.Sprint(suffix))), assets, nil); err != nil {
		t.Fatal(err)
	}
	article, err = store.GetArticle(ctx, article.ID)
	if err != nil || len(article.Assets) != 1 {
		t.Fatalf("article assets = %d, err=%v", len(article.Assets), err)
	}
	coverID := article.Assets[0].ID
	contentHTML := fmt.Sprintf(`<p>正文</p><img data-weavepress-asset-id="%d" alt="封面">`, coverID)
	draft, err := store.CreateDraft(ctx, article.ID, user.ID, article.Title, article.Author, "摘要", contentHTML, &coverID)
	if err != nil {
		t.Fatal(err)
	}
	updated, err := store.UpdateDraft(ctx, draft.ID, user.ID, editorial.UpdateInput{Title: draft.Title + "（改）", Author: draft.Author, Digest: draft.Digest, ContentHTML: contentHTML, CoverAssetID: &coverID, ExpectedVersion: 1, ChangeNote: "调整标题"})
	if err != nil || updated.CurrentVersion != 2 {
		t.Fatalf("updated version = %d, err=%v", updated.CurrentVersion, err)
	}
	restored, err := store.RestoreDraftVersion(ctx, draft.ID, user.ID, 1, updated.CurrentVersion)
	if err != nil || restored.CurrentVersion != 3 || restored.Title != draft.Title {
		t.Fatalf("restored draft = %#v, err=%v", restored, err)
	}
	restoredVersion, err := store.GetDraftVersion(ctx, draft.ID, restored.CurrentVersion)
	if err != nil || restoredVersion.ChangeNote != "恢复自 v1" {
		t.Fatalf("restored version = %#v, err=%v", restoredVersion, err)
	}
	if _, err := store.RestoreDraftVersion(ctx, draft.ID, user.ID, 2, updated.CurrentVersion); !errors.Is(err, editorial.ErrDraftVersionConflict) {
		t.Fatalf("stale restore error = %v", err)
	}
	if _, err := store.UpdateDraft(ctx, draft.ID, user.ID, editorial.UpdateInput{Title: "过期保存", ContentHTML: contentHTML, ExpectedVersion: 1}); !errors.Is(err, editorial.ErrDraftVersionConflict) {
		t.Fatalf("stale update error = %v", err)
	}
	if _, err = store.SetDraftStatus(ctx, draft.ID, user.ID, editorial.StatusEditing, editorial.StatusInReview, "提交审核"); err != nil {
		t.Fatal(err)
	}
	if _, err = store.SetDraftStatus(ctx, draft.ID, user.ID, editorial.StatusInReview, editorial.StatusApproved, "审核通过"); err != nil {
		t.Fatal(err)
	}
	publishJob, err := store.CreatePublishJob(ctx, draft.ID, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreatePublishJob(ctx, draft.ID, user.ID); !errors.Is(err, editorial.ErrDraftStateConflict) {
		t.Fatalf("parallel publish error = %v", err)
	}
	if err := store.SetPublishJobPublishing(ctx, publishJob.ID); err != nil {
		t.Fatal(err)
	}
	if err := store.SetPublishJobFailure(ctx, publishJob.ID, "WECHAT_-1", "系统繁忙", false); err != nil {
		t.Fatal(err)
	}
	if _, err := store.RetryPublishJob(ctx, publishJob.ID, user.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.RetryPublishJob(ctx, publishJob.ID, user.ID); !errors.Is(err, editorial.ErrPublishNotRetryable) {
		t.Fatalf("parallel retry error = %v", err)
	}
	if err := store.SetPublishJobPublishing(ctx, publishJob.ID); err != nil {
		t.Fatal(err)
	}
	if err := store.CompletePublishJob(ctx, publishJob.ID, "remote-media-id"); err != nil {
		t.Fatal(err)
	}
	completed, err := store.GetPublishJob(ctx, publishJob.ID, true)
	if err != nil || completed.Status != editorial.PublishCompleted || completed.RemoteMediaID != "remote-media-id" || len(completed.Events) < 5 {
		t.Fatalf("completed publish job = %#v, err=%v", completed, err)
	}
	finalDraft, err := store.GetDraft(ctx, draft.ID, true)
	if err != nil || finalDraft.Status != editorial.StatusPublished || len(finalDraft.Events) < 6 {
		t.Fatalf("final draft = %#v, err=%v", finalDraft, err)
	}
}
