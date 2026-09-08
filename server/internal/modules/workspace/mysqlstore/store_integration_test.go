package mysqlstore_test

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"

	"github.com/chenbb0128/weavepress/server/internal/modules/aiwriting"
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

func TestMySQLIntegrationAIStore(t *testing.T) {
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
	t.Cleanup(func() { _ = db.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		t.Fatal(err)
	}
	for _, column := range []struct {
		table string
		name  string
		want  int64
	}{
		{table: "ai_generations", name: "angle_id", want: 64},
		{table: "ai_generations", name: "tone", want: 32},
		{table: "ai_job_events", name: "status", want: 64},
	} {
		var got int64
		err := db.QueryRowContext(ctx, `SELECT CHARACTER_MAXIMUM_LENGTH
			FROM information_schema.columns
			WHERE table_schema = DATABASE() AND table_name = ? AND column_name = ?`, column.table, column.name).Scan(&got)
		if err != nil {
			t.Fatalf("query %s.%s length: %v", column.table, column.name, err)
		}
		if got != column.want {
			t.Fatalf("%s.%s length = %d, want %d", column.table, column.name, got, column.want)
		}
	}

	workspaceStore := mysqlstore.New(db)
	aiStore := mysqlstore.NewAIStore(db)
	suffix := time.Now().UnixNano()
	firstUser, err := workspaceStore.CreateUser(ctx, fmt.Sprintf("ai_first_%d", suffix), "unused", "AI 集成测试一", workspace.RoleEditor, "active")
	if err != nil {
		t.Fatal(err)
	}
	var secondUser workspace.User
	var articleID uint64
	t.Cleanup(func() {
		cleanup := func(query string, args ...any) {
			t.Helper()
			if _, cleanupErr := db.Exec(query, args...); cleanupErr != nil {
				t.Errorf("AI integration cleanup failed: %v", cleanupErr)
			}
		}
		cleanup(`DELETE FROM ai_job_events WHERE job_id IN (SELECT id FROM ai_jobs WHERE requested_by IN (?, ?))`, firstUser.ID, secondUser.ID)
		cleanup(`DELETE FROM ai_generations WHERE job_id IN (SELECT id FROM ai_jobs WHERE requested_by IN (?, ?))`, firstUser.ID, secondUser.ID)
		cleanup(`DELETE FROM ai_analyses WHERE job_id IN (SELECT id FROM ai_jobs WHERE requested_by IN (?, ?))`, firstUser.ID, secondUser.ID)
		cleanup(`DELETE FROM ai_jobs WHERE requested_by IN (?, ?) AND job_type = 'generation'`, firstUser.ID, secondUser.ID)
		cleanup(`DELETE FROM ai_jobs WHERE requested_by IN (?, ?) AND job_type = 'analysis'`, firstUser.ID, secondUser.ID)
		cleanup(`DELETE FROM draft_events WHERE draft_id IN (SELECT id FROM drafts WHERE created_by IN (?, ?))`, firstUser.ID, secondUser.ID)
		cleanup(`DELETE FROM draft_versions WHERE draft_id IN (SELECT id FROM drafts WHERE created_by IN (?, ?))`, firstUser.ID, secondUser.ID)
		cleanup(`DELETE FROM drafts WHERE created_by IN (?, ?)`, firstUser.ID, secondUser.ID)
		cleanup(`DELETE FROM article_assets WHERE article_id = ?`, articleID)
		cleanup(`DELETE FROM collection_job_events WHERE job_id IN (SELECT id FROM collection_jobs WHERE article_id = ?)`, articleID)
		cleanup(`DELETE FROM collection_jobs WHERE article_id = ?`, articleID)
		cleanup(`DELETE FROM articles WHERE id = ?`, articleID)
		cleanup(`DELETE FROM users WHERE id IN (?, ?)`, firstUser.ID, secondUser.ID)
	})
	secondUser, err = workspaceStore.CreateUser(ctx, fmt.Sprintf("ai_second_%d", suffix), "unused", "AI 集成测试二", workspace.RoleEditor, "active")
	if err != nil {
		t.Fatal(err)
	}
	canonical := fmt.Sprintf("https://example.com/ai/%d", suffix)
	canonicalHash := sha256.Sum256([]byte(canonical))
	articleResult, err := db.ExecContext(ctx, `INSERT INTO articles
		(original_url, canonical_url, canonical_url_hash, source_type, title, author, source_name, language,
		 plain_text, clean_html, blocks_json, metadata_json, status, created_by)
		VALUES (?, ?, ?, 'web', 'AI 测试文章', '原作者', '测试来源', 'zh-CN', '事实一。事实二。', '<p>事实一。事实二。</p>', JSON_ARRAY(), JSON_OBJECT(), 'ready', ?)`,
		canonical, canonical, canonicalHash[:], firstUser.ID)
	if err != nil {
		t.Fatal(err)
	}
	articleInsertID, err := articleResult.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	articleID = uint64(articleInsertID)
	assetURL := canonical + "/cover.jpg"
	assetHash := sha256.Sum256([]byte(assetURL))
	assetResult, err := db.ExecContext(ctx, `INSERT INTO article_assets
		(article_id, source_url, source_url_hash, object_key, media_type, byte_size, sha256, position, is_cover, download_status)
		VALUES (?, ?, ?, 'articles/ai-test-cover.jpg', 'image/jpeg', 8, ?, 1, TRUE, 'completed')`,
		articleID, assetURL, assetHash[:], assetHash[:])
	if err != nil {
		t.Fatal(err)
	}
	assetInsertID, err := assetResult.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	coverID := uint64(assetInsertID)

	fingerprint := sha256.Sum256([]byte("AI integration fingerprint"))
	analysisInput := aiwriting.CreateAnalysisJobInput{
		ArticleID: articleID, RequestedBy: firstUser.ID, Provider: "openai", Model: "gpt-test",
		PromptVersion: aiwriting.AnalysisPromptV1, InputFingerprint: fingerprint,
	}
	type analysisCreateResult struct {
		job    aiwriting.Job
		reused bool
		err    error
	}
	analysisResults := make(chan analysisCreateResult, 8)
	var analysisWait sync.WaitGroup
	for range 8 {
		analysisWait.Add(1)
		go func() {
			defer analysisWait.Done()
			job, reused, createErr := aiStore.CreateAnalysisJob(ctx, analysisInput)
			analysisResults <- analysisCreateResult{job: job, reused: reused, err: createErr}
		}()
	}
	analysisWait.Wait()
	close(analysisResults)
	var analysisJob aiwriting.Job
	created := 0
	for result := range analysisResults {
		if result.err != nil {
			t.Fatal(result.err)
		}
		if analysisJob.ID == 0 {
			analysisJob = result.job
		}
		if result.job.ID != analysisJob.ID {
			t.Fatalf("analysis job IDs differ: got %d, want %d", result.job.ID, analysisJob.ID)
		}
		if !result.reused {
			created++
		}
	}
	if created != 1 {
		t.Fatalf("analysis jobs created = %d, want 1", created)
	}
	var reuseKey []byte
	if err := db.QueryRowContext(ctx, `SELECT reuse_key FROM ai_jobs WHERE id = ?`, analysisJob.ID).Scan(&reuseKey); err != nil {
		t.Fatal(err)
	}
	expectedReuseMaterial := fmt.Sprintf("analysis\x00%d\x00%s\x00", articleID, aiwriting.AnalysisPromptV1)
	expectedReuseKey := sha256.Sum256(append([]byte(expectedReuseMaterial), fingerprint[:]...))
	if !reflect.DeepEqual(reuseKey, expectedReuseKey[:]) {
		t.Fatalf("reuse key = %x, want %x", reuseKey, expectedReuseKey)
	}
	forceInput := analysisInput
	forceInput.Force = true
	forcedJob, reused, err := aiStore.CreateAnalysisJob(ctx, forceInput)
	if err != nil || reused || forcedJob.ID == analysisJob.ID {
		t.Fatalf("force analysis job=%#v reused=%v err=%v", forcedJob, reused, err)
	}
	var forcedReuseKey []byte
	if err := db.QueryRowContext(ctx, `SELECT reuse_key FROM ai_jobs WHERE id = ?`, forcedJob.ID).Scan(&forcedReuseKey); err != nil {
		t.Fatal(err)
	}
	if forcedReuseKey != nil {
		t.Fatalf("forced reuse key = %x, want NULL", forcedReuseKey)
	}

	if err := aiStore.SetJobRunning(ctx, analysisJob.ID); err != nil {
		t.Fatal(err)
	}
	if err := aiStore.SetJobRunning(ctx, analysisJob.ID); err != nil {
		t.Fatalf("running recovery: %v", err)
	}
	analysisOutput := aiwriting.AnalysisOutput{
		Summary:    "两项事实的摘要",
		Facts:      []aiwriting.Fact{{ID: "fact-1", Text: "事实一", SourceBlockIDs: []string{"block-1"}, Confidence: "high"}},
		Viewpoints: []aiwriting.Viewpoint{{ID: "view-1", Text: "观点", Holder: "作者", SourceBlockIDs: []string{"block-1"}}},
		Quotes:     []aiwriting.Quote{{ID: "quote-1", Text: "事实一", SourceBlockID: "block-1"}},
		Risks:      []aiwriting.Risk{{ID: "risk-1", Text: "需核实", SourceBlockIDs: []string{"block-2"}}},
		Angles:     []aiwriting.Angle{{ID: "angle-1", Title: "角度一", Thesis: "论点", Outline: []string{"起", "承"}}},
	}
	analysis, err := aiStore.CompleteAnalysis(ctx, analysisJob.ID, analysisOutput, aiwriting.TokenUsage{InputTokens: 101, OutputTokens: 29, TotalTokens: 130})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(analysis.AnalysisOutput, analysisOutput) {
		t.Fatalf("analysis JSON roundtrip = %#v, want %#v", analysis.AnalysisOutput, analysisOutput)
	}
	analysisAgain, err := aiStore.CompleteAnalysis(ctx, analysisJob.ID, analysisOutput, aiwriting.TokenUsage{InputTokens: 999, OutputTokens: 999, TotalTokens: 999})
	if err != nil || analysisAgain.ID != analysis.ID {
		t.Fatalf("idempotent analysis = %#v, err=%v", analysisAgain, err)
	}
	completedAnalysisJob, err := aiStore.GetJob(ctx, analysisJob.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	if completedAnalysisJob.Status != aiwriting.JobCompleted || completedAnalysisJob.Attempts != 2 || completedAnalysisJob.TotalTokens != 130 || completedAnalysisJob.Article == nil || len(completedAnalysisJob.Events) != 4 {
		t.Fatalf("completed analysis job = %#v", completedAnalysisJob)
	}
	if err := aiStore.SetJobRunning(ctx, analysisJob.ID); err != nil {
		t.Fatalf("completed running no-op: %v", err)
	}
	var analysisCount int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM ai_analyses WHERE job_id = ?`, analysisJob.ID).Scan(&analysisCount); err != nil || analysisCount != 1 {
		t.Fatalf("analysis count=%d err=%v", analysisCount, err)
	}
	loadedAnalysis, err := aiStore.GetAnalysis(ctx, analysis.ID)
	if err != nil || loadedAnalysis.Job == nil || len(loadedAnalysis.Job.Events) != 4 || !reflect.DeepEqual(loadedAnalysis.AnalysisOutput, analysisOutput) {
		t.Fatalf("loaded analysis = %#v, err=%v", loadedAnalysis, err)
	}
	analysisPage, err := aiStore.ListAnalyses(ctx, articleID, 1, 10)
	if err != nil || analysisPage.Total != 1 || len(analysisPage.Items) != 1 {
		t.Fatalf("analysis page = %#v, err=%v", analysisPage, err)
	}

	params := aiwriting.GenerationParams{AngleID: "angle-1", Audience: "大众", Tone: "plain", TargetWords: 800, AdditionalInstructions: "保持简洁", IdempotencyKey: "same-generation-key"}
	generationInput := aiwriting.CreateGenerationJobInput{
		AnalysisID: analysis.ID, RequestedBy: firstUser.ID, Params: params, Provider: "openai", Model: "gpt-test",
		PromptVersion: aiwriting.GenerationPromptV1, InputFingerprint: fingerprint,
	}
	generation, generationJob, reused, err := aiStore.CreateGenerationJob(ctx, generationInput)
	if err != nil || reused {
		t.Fatalf("create generation=%#v job=%#v reused=%v err=%v", generation, generationJob, reused, err)
	}
	sameGeneration, sameGenerationJob, reused, err := aiStore.CreateGenerationJob(ctx, generationInput)
	if err != nil || !reused || sameGeneration.ID != generation.ID || sameGenerationJob.ID != generationJob.ID {
		t.Fatalf("reuse generation=%#v job=%#v reused=%v err=%v", sameGeneration, sameGenerationJob, reused, err)
	}

	concurrentInput := generationInput
	concurrentInput.Params.IdempotencyKey = "concurrent-generation-key"
	type generationCreateResult struct {
		generation aiwriting.Generation
		job        aiwriting.Job
		reused     bool
		err        error
	}
	generationResults := make(chan generationCreateResult, 8)
	var generationWait sync.WaitGroup
	for range 8 {
		generationWait.Add(1)
		go func() {
			defer generationWait.Done()
			item, job, wasReused, createErr := aiStore.CreateGenerationJob(ctx, concurrentInput)
			generationResults <- generationCreateResult{generation: item, job: job, reused: wasReused, err: createErr}
		}()
	}
	generationWait.Wait()
	close(generationResults)
	var concurrentGenerationID, concurrentJobID uint64
	created = 0
	for result := range generationResults {
		if result.err != nil {
			t.Fatal(result.err)
		}
		if concurrentGenerationID == 0 {
			concurrentGenerationID, concurrentJobID = result.generation.ID, result.job.ID
		}
		if result.generation.ID != concurrentGenerationID || result.job.ID != concurrentJobID {
			t.Fatalf("concurrent generation IDs differ: generation=%d job=%d", result.generation.ID, result.job.ID)
		}
		if !result.reused {
			created++
		}
	}
	if created != 1 {
		t.Fatalf("concurrent generation created = %d, want 1", created)
	}
	otherUserInput := generationInput
	otherUserInput.RequestedBy = secondUser.ID
	otherGeneration, otherJob, reused, err := aiStore.CreateGenerationJob(ctx, otherUserInput)
	if err != nil || reused || otherGeneration.ID == generation.ID || otherJob.ID == generationJob.ID {
		t.Fatalf("other user generation=%#v job=%#v reused=%v err=%v", otherGeneration, otherJob, reused, err)
	}

	if err := aiStore.SetJobRunning(ctx, generationJob.ID); err != nil {
		t.Fatal(err)
	}
	generationOutput := aiwriting.GenerationOutput{
		Title: "AI 生成标题", Digest: "AI 生成摘要",
		Blocks: []aiwriting.GeneratedBlock{
			{Type: "paragraph", Text: "第一段", FactIDs: []string{"fact-1", "fact-1"}},
			{Type: "heading", Level: 2, Text: "小标题"},
			{Type: "paragraph", Text: "第二段", FactIDs: []string{"fact-1", "fact-2", "fact-1"}},
		},
	}
	contentHTML := `<p>第一段</p><h2>小标题</h2><p>第二段</p>`
	draftInput := editorial.GeneratedDraftInput{SourceArticleID: articleID, CreatedBy: firstUser.ID, Title: generationOutput.Title, Digest: generationOutput.Digest, ContentHTML: contentHTML, CoverAssetID: &coverID}
	completedGeneration, err := aiStore.CompleteGeneration(ctx, generationJob.ID, generationOutput, draftInput, aiwriting.TokenUsage{InputTokens: 80, OutputTokens: 120, TotalTokens: 200})
	if err != nil || completedGeneration.DraftID == nil {
		t.Fatalf("complete generation=%#v err=%v", completedGeneration, err)
	}
	wantFactMap := map[string][]string{"1": {"fact-1"}, "3": {"fact-1", "fact-2"}}
	if !reflect.DeepEqual(completedGeneration.GenerationOutput, generationOutput) || !reflect.DeepEqual(completedGeneration.FactMap, wantFactMap) || completedGeneration.ContentHTML != contentHTML {
		t.Fatalf("completed generation roundtrip = %#v", completedGeneration)
	}
	draftID := *completedGeneration.DraftID
	var draftStatus, draftAuthor string
	var draftCover sql.NullInt64
	if err := db.QueryRowContext(ctx, `SELECT status, author, cover_asset_id FROM drafts WHERE id = ?`, draftID).Scan(&draftStatus, &draftAuthor, &draftCover); err != nil {
		t.Fatal(err)
	}
	if draftStatus != editorial.StatusEditing || draftAuthor != "" || !draftCover.Valid || uint64(draftCover.Int64) != coverID {
		t.Fatalf("draft status=%q author=%q cover=%#v", draftStatus, draftAuthor, draftCover)
	}
	assertCount := func(query string, want int, args ...any) {
		t.Helper()
		var count int
		if countErr := db.QueryRowContext(ctx, query, args...).Scan(&count); countErr != nil || count != want {
			t.Fatalf("count query %q = %d, want %d, err=%v", query, count, want, countErr)
		}
	}
	assertCount(`SELECT COUNT(*) FROM draft_versions WHERE draft_id = ? AND version = 1 AND change_note = 'AI 合规采编生成'`, 1, draftID)
	assertCount(`SELECT COUNT(*) FROM draft_events WHERE draft_id = ? AND from_status = '' AND to_status = 'editing' AND note = 'AI 合规采编生成'`, 1, draftID)
	completedGenerationAgain, err := aiStore.CompleteGeneration(ctx, generationJob.ID, generationOutput, draftInput, aiwriting.TokenUsage{InputTokens: 999, OutputTokens: 999, TotalTokens: 999})
	if err != nil || completedGenerationAgain.DraftID == nil || *completedGenerationAgain.DraftID != draftID {
		t.Fatalf("idempotent generation=%#v err=%v", completedGenerationAgain, err)
	}
	assertCount(`SELECT COUNT(*) FROM drafts WHERE id = ?`, 1, draftID)
	assertCount(`SELECT COUNT(*) FROM draft_versions WHERE draft_id = ?`, 1, draftID)
	assertCount(`SELECT COUNT(*) FROM draft_events WHERE draft_id = ?`, 1, draftID)
	loadedGeneration, err := aiStore.GetGeneration(ctx, generation.ID)
	if err != nil || loadedGeneration.Job == nil || loadedGeneration.Job.Status != aiwriting.JobCompleted || loadedGeneration.Job.TotalTokens != 200 {
		t.Fatalf("loaded generation=%#v err=%v", loadedGeneration, err)
	}
	byJobGeneration, err := aiStore.GetGenerationByJobID(ctx, generationJob.ID)
	if err != nil || byJobGeneration.ID != generation.ID {
		t.Fatalf("generation by job=%#v err=%v", byJobGeneration, err)
	}

	rollbackInput := generationInput
	rollbackInput.Params.IdempotencyKey = "rollback-generation-key"
	rollbackGeneration, rollbackJob, _, err := aiStore.CreateGenerationJob(ctx, rollbackInput)
	if err != nil {
		t.Fatal(err)
	}
	if err := aiStore.SetJobRunning(ctx, rollbackJob.ID); err != nil {
		t.Fatal(err)
	}
	invalidCoverID := ^uint64(0)
	invalidDraftInput := draftInput
	invalidDraftInput.CoverAssetID = &invalidCoverID
	if _, err := aiStore.CompleteGeneration(ctx, rollbackJob.ID, generationOutput, invalidDraftInput, aiwriting.TokenUsage{TotalTokens: 1}); err == nil {
		t.Fatal("complete generation with invalid cover unexpectedly succeeded")
	}
	assertCount(`SELECT COUNT(*) FROM drafts WHERE title = ? AND created_by = ?`, 1, generationOutput.Title, firstUser.ID)
	var rollbackStatus, rollbackTitle string
	var rollbackDraftID sql.NullInt64
	if err := db.QueryRowContext(ctx, `SELECT j.status, g.title, g.draft_id FROM ai_jobs j JOIN ai_generations g ON g.job_id = j.id WHERE g.id = ?`, rollbackGeneration.ID).Scan(&rollbackStatus, &rollbackTitle, &rollbackDraftID); err != nil {
		t.Fatal(err)
	}
	if rollbackStatus != aiwriting.JobRunning || rollbackTitle != "" || rollbackDraftID.Valid {
		t.Fatalf("rollback state status=%q title=%q draft=%#v", rollbackStatus, rollbackTitle, rollbackDraftID)
	}
	if err := aiStore.SetJobFailure(ctx, rollbackJob.ID, "FINAL", "等待人工重试", false, aiwriting.TokenUsage{}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE ai_jobs SET retryable = TRUE WHERE id = ?`, rollbackJob.ID); err != nil {
		t.Fatal(err)
	}
	retriedGenerationJob, err := aiStore.RetryJob(ctx, rollbackJob.ID, secondUser.ID)
	if err != nil {
		t.Fatal(err)
	}
	if retriedGenerationJob.RequestedBy != firstUser.ID {
		t.Errorf("retried generation requested_by = %d, want original requester %d", retriedGenerationJob.RequestedBy, firstUser.ID)
	}
	retriedGenerationWithEvents, err := aiStore.GetJob(ctx, rollbackJob.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	wantRetryMessage := fmt.Sprintf("用户 %d 手动重试 AI 任务", secondUser.ID)
	if got := retriedGenerationWithEvents.Events[len(retriedGenerationWithEvents.Events)-1].Message; got != wantRetryMessage {
		t.Errorf("generation retry event = %q, want %q", got, wantRetryMessage)
	}
	reusedRollbackGeneration, reusedRollbackJob, reused, err := aiStore.CreateGenerationJob(ctx, rollbackInput)
	if err != nil {
		t.Fatal(err)
	}
	if !reused || reusedRollbackGeneration.ID != rollbackGeneration.ID || reusedRollbackJob.ID != rollbackJob.ID {
		t.Errorf("generation retry idempotency: generation=%d job=%d reused=%v, want generation=%d job=%d reused=true",
			reusedRollbackGeneration.ID, reusedRollbackJob.ID, reused, rollbackGeneration.ID, rollbackJob.ID)
	}

	jobsPage, err := aiStore.ListJobs(ctx, aiwriting.JobFilter{Type: aiwriting.JobTypeGeneration, Status: aiwriting.JobCompleted, ArticleID: articleID}, 1, 10)
	if err != nil || jobsPage.Total != 1 || len(jobsPage.Items) != 1 || jobsPage.Items[0].Article == nil || jobsPage.Items[0].Article.Title != "AI 测试文章" || jobsPage.Items[0].Article.SourceName != "测试来源" {
		t.Fatalf("jobs page=%#v err=%v", jobsPage, err)
	}
	if _, err := aiStore.GetJob(ctx, ^uint64(0), false); !errors.Is(err, workspace.ErrNotFound) {
		t.Fatalf("missing job error=%v", err)
	}

	failureInput := analysisInput
	failureInput.Force = true
	failureJob, _, err := aiStore.CreateAnalysisJob(ctx, failureInput)
	if err != nil {
		t.Fatal(err)
	}
	if err := aiStore.SetJobRunning(ctx, failureJob.ID); err != nil {
		t.Fatal(err)
	}
	if err := aiStore.SetJobFailure(ctx, failureJob.ID, "TEMP", "稍后重试", true, aiwriting.TokenUsage{InputTokens: 5, TotalTokens: 5}); err != nil {
		t.Fatal(err)
	}
	requeued, err := aiStore.GetJob(ctx, failureJob.ID, true)
	if err != nil || requeued.Status != aiwriting.JobQueued || !requeued.Retryable || requeued.FinishedAt != nil || requeued.TotalTokens != 5 || requeued.Events[len(requeued.Events)-1].Message != "临时错误，等待自动重试：稍后重试" {
		t.Fatalf("requeued job=%#v err=%v", requeued, err)
	}
	if err := aiStore.SetJobRunning(ctx, failureJob.ID); err != nil {
		t.Fatal(err)
	}
	if err := aiStore.SetJobFailure(ctx, failureJob.ID, "FINAL", "最终失败", false, aiwriting.TokenUsage{OutputTokens: 3, TotalTokens: 3}); err != nil {
		t.Fatal(err)
	}
	failed, err := aiStore.GetJob(ctx, failureJob.ID, false)
	if err != nil || failed.Status != aiwriting.JobFailed || failed.Retryable || failed.FinishedAt == nil || failed.TotalTokens != 8 {
		t.Fatalf("failed job=%#v err=%v", failed, err)
	}
	if _, err := aiStore.RetryJob(ctx, failureJob.ID, secondUser.ID); !errors.Is(err, aiwriting.ErrJobNotRetryable) {
		t.Fatalf("non-retryable job retry error=%v", err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE ai_jobs SET retryable = TRUE WHERE id = ?`, failureJob.ID); err != nil {
		t.Fatal(err)
	}
	retried, err := aiStore.RetryJob(ctx, failureJob.ID, secondUser.ID)
	if err != nil || retried.Status != aiwriting.JobQueued || retried.RequestedBy != firstUser.ID || retried.ManualRetries != 1 || retried.ErrorCode != "" || retried.FinishedAt != nil {
		t.Fatalf("retried job=%#v err=%v", retried, err)
	}
	if _, err := aiStore.RetryJob(ctx, failureJob.ID, firstUser.ID); !errors.Is(err, aiwriting.ErrJobNotRetryable) {
		t.Fatalf("queued job retry error=%v", err)
	}
	longMessage := strings.Repeat("测", 1030)
	if err := aiStore.AddJobEvent(ctx, failureJob.ID, aiwriting.JobQueued, longMessage); err != nil {
		t.Fatal(err)
	}
	withLongEvent, err := aiStore.GetJob(ctx, failureJob.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	if got := len([]rune(withLongEvent.Events[len(withLongEvent.Events)-1].Message)); got != 1024 {
		t.Fatalf("event message runes=%d, want 1024", got)
	}

	notReadyInput := generationInput
	notReadyInput.AnalysisID = analysis.ID + 999999
	notReadyInput.Params.IdempotencyKey = "missing-analysis"
	if _, _, _, err := aiStore.CreateGenerationJob(ctx, notReadyInput); !errors.Is(err, workspace.ErrNotFound) {
		t.Fatalf("missing analysis error=%v", err)
	}
	forcedAnalysisPlaceholder, _, err := aiStore.CreateAnalysisJob(ctx, forceInput)
	if err != nil {
		t.Fatal(err)
	}
	pendingResult, err := db.ExecContext(ctx, `INSERT INTO ai_analyses (job_id, article_id, summary, facts_json, viewpoints_json, quotes_json, risks_json, angles_json)
		VALUES (?, ?, '', JSON_ARRAY(), JSON_ARRAY(), JSON_ARRAY(), JSON_ARRAY(), JSON_ARRAY())`, forcedAnalysisPlaceholder.ID, articleID)
	if err != nil {
		t.Fatal(err)
	}
	pendingAnalysisID, err := pendingResult.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	pendingInput := generationInput
	pendingInput.AnalysisID = uint64(pendingAnalysisID)
	pendingInput.Params.IdempotencyKey = "not-ready"
	if _, _, _, err := aiStore.CreateGenerationJob(ctx, pendingInput); !errors.Is(err, aiwriting.ErrAnalysisNotReady) {
		t.Fatalf("pending analysis error=%v", err)
	}
}
