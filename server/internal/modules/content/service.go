package content

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"net/url"
	"path"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/hibiken/asynq"

	"github.com/chenbb0128/weavepress/server/internal/collectors"
	"github.com/chenbb0128/weavepress/server/internal/config"
	"github.com/chenbb0128/weavepress/server/internal/modules/workspace"
	"github.com/chenbb0128/weavepress/server/internal/platform/objectstore"
	"github.com/chenbb0128/weavepress/server/internal/platform/queue"
)

const TaskCollectArticle = "content:collect"

type Service struct {
	store    workspace.Store
	queue    *queue.Client
	objects  objectstore.Store
	fetcher  collectors.Fetcher
	registry *collectors.Registry
	cfg      config.Config
}

type SubmitResult struct {
	Article workspace.Article `json:"article"`
	Job     workspace.Job     `json:"job"`
	Reused  bool              `json:"reused"`
}

func New(store workspace.Store, queueClient *queue.Client, objects objectstore.Store, cfg config.Config) *Service {
	return &Service{store: store, queue: queueClient, objects: objects, fetcher: collectors.NewFetcher(cfg.Collector), registry: collectors.NewRegistry(), cfg: cfg}
}

func (s *Service) Submit(ctx context.Context, rawURL string, userID uint64) (SubmitResult, error) {
	canonical, sourceType, hash, err := collectors.Canonicalize(rawURL)
	if err != nil {
		return SubmitResult{}, err
	}
	parsed, _ := url.Parse(canonical)
	if err := s.fetcher.Validate(ctx, parsed); err != nil {
		return SubmitResult{}, err
	}
	article, job, reused, err := s.store.CreateArticleJob(ctx, strings.TrimSpace(rawURL), canonical, hash, sourceType, userID)
	if err != nil {
		return SubmitResult{}, err
	}
	if !reused {
		if err := s.enqueue(job); err != nil {
			_ = s.store.SetJobFailure(ctx, job.ID, "QUEUE_UNAVAILABLE", "任务队列暂不可用", false)
			return SubmitResult{}, err
		}
	}
	return SubmitResult{Article: article, Job: job, Reused: reused}, nil
}

func (s *Service) Retry(ctx context.Context, jobID uint64) (workspace.Job, error) {
	job, err := s.store.RetryJob(ctx, jobID)
	if err != nil {
		return workspace.Job{}, err
	}
	if err := s.enqueue(job); err != nil {
		_ = s.store.SetJobFailure(ctx, job.ID, "QUEUE_UNAVAILABLE", "任务队列暂不可用", false)
		return workspace.Job{}, err
	}
	return job, nil
}

func (s *Service) enqueue(job workspace.Job) error {
	payload, err := json.Marshal(map[string]uint64{"jobId": job.ID})
	if err != nil {
		return err
	}
	taskID := fmt.Sprintf("collect:%d:%d:%d", job.ID, job.Attempts, job.ManualRetries)
	_, err = s.queue.Asynq.Enqueue(asynq.NewTask(TaskCollectArticle, payload), asynq.Queue("collection"), asynq.TaskID(taskID), asynq.MaxRetry(3), asynq.Timeout(15*time.Minute))
	return err
}

func (s *Service) HandleTask(ctx context.Context, task *asynq.Task) error {
	var payload struct {
		JobID uint64 `json:"jobId"`
	}
	if err := json.Unmarshal(task.Payload(), &payload); err != nil {
		return fmt.Errorf("%w: invalid payload", asynq.SkipRetry)
	}
	err := s.process(ctx, payload.JobID)
	if err == nil {
		return nil
	}
	if errors.Is(err, workspace.ErrJobStateConflict) {
		return nil
	}
	code, message, retryable := classify(err)
	retryCount, _ := asynq.GetRetryCount(ctx)
	maxRetry, _ := asynq.GetMaxRetry(ctx)
	willRetry := retryable && retryCount < maxRetry
	_ = s.store.SetJobFailure(ctx, payload.JobID, code, message, willRetry)
	if !willRetry {
		return fmt.Errorf("%w: %s", asynq.SkipRetry, message)
	}
	return err
}

func (s *Service) process(ctx context.Context, jobID uint64) error {
	job, err := s.store.GetJob(ctx, jobID, false)
	if err != nil {
		return err
	}
	if job.Status != "queued" {
		return nil
	}
	article, err := s.store.GetArticle(ctx, job.ArticleID)
	if err != nil {
		return err
	}
	if err := s.store.SetJobStage(ctx, jobID, "fetching", "正在获取原始页面"); err != nil {
		return err
	}
	collected, err := s.registry.Collect(ctx, s.fetcher, article.CanonicalURL)
	if err != nil {
		return err
	}
	if err := s.store.SetJobStage(ctx, jobID, "parsing", "正在解析正文结构"); err != nil {
		return err
	}
	collected.Title = truncateRunes(collected.Title, 512)
	collected.Author = truncateRunes(collected.Author, 255)
	collected.SourceName = truncateRunes(collected.SourceName, 255)
	if err := s.store.SetJobStage(ctx, jobID, "storing_assets", "正在归档页面和图片"); err != nil {
		return err
	}
	rawKey, err := s.storeRaw(ctx, article.ID, collected.RawHTML)
	if err != nil {
		return fmt.Errorf("store raw page: %w", err)
	}
	assets, warnings, err := s.storeImages(ctx, article.ID, article.CanonicalURL, collected.Images)
	if err != nil {
		return err
	}
	contentHash := sha256.Sum256([]byte(collected.PlainText))
	return s.store.CompleteArticle(ctx, article.ID, jobID, collected, rawKey, contentHash, assets, warnings)
}

func (s *Service) storeRaw(ctx context.Context, articleID uint64, raw []byte) (string, error) {
	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	if _, err := writer.Write(raw); err != nil {
		return "", err
	}
	if err := writer.Close(); err != nil {
		return "", err
	}
	now := time.Now().UTC()
	key := fmt.Sprintf("raw/%04d/%02d/%d.html.gz", now.Year(), now.Month(), articleID)
	return key, s.objects.Put(ctx, key, compressed.Bytes(), "application/gzip")
}

func (s *Service) storeImages(ctx context.Context, articleID uint64, referer string, images []workspace.CollectedImage) ([]workspace.StoredAsset, []string, error) {
	truncated := len(images) > s.cfg.Collector.MaxImages
	if len(images) > s.cfg.Collector.MaxImages {
		images = images[:s.cfg.Collector.MaxImages]
	}
	assets := make([]workspace.StoredAsset, 0, len(images))
	warnings := make([]string, 0)
	semaphore := make(chan struct{}, s.cfg.Collector.ImageConcurrency)
	var wait sync.WaitGroup
	var lock sync.Mutex
	var total int64
	for _, imageItem := range images {
		imageItem := imageItem
		wait.Add(1)
		go func() {
			defer wait.Done()
			semaphore <- struct{}{}
			defer func() { <-semaphore }()
			imageCtx, cancel := context.WithTimeout(ctx, s.cfg.Collector.ImageTimeout)
			defer cancel()
			fetched, err := s.fetcher.FetchImage(imageCtx, imageItem.SourceURL, referer)
			if err != nil {
				lock.Lock()
				warnings = append(warnings, fmt.Sprintf("图片下载失败：%s", imageItem.SourceURL))
				assets = append(assets, failedAsset(imageItem))
				lock.Unlock()
				return
			}
			lock.Lock()
			if total+int64(len(fetched.Body)) > s.cfg.Collector.ArticleMaxBytes {
				warnings = append(warnings, "文章图片总大小超过限制，部分图片未归档")
				assets = append(assets, failedAsset(imageItem))
				lock.Unlock()
				return
			}
			total += int64(len(fetched.Body))
			lock.Unlock()
			hash := sha256.Sum256(fetched.Body)
			extension := extensionFor(fetched.ContentType)
			now := time.Now().UTC()
			key := fmt.Sprintf("articles/%04d/%02d/%s%s", now.Year(), now.Month(), hex.EncodeToString(hash[:]), extension)
			width, height := imageDimensions(fetched.Body)
			if err := s.objects.Put(imageCtx, key, fetched.Body, fetched.ContentType); err != nil {
				lock.Lock()
				warnings = append(warnings, fmt.Sprintf("图片存储失败：%s", imageItem.SourceURL))
				assets = append(assets, failedAsset(imageItem))
				lock.Unlock()
				return
			}
			asset := workspace.StoredAsset{SourceURL: imageItem.SourceURL, ObjectKey: key, MediaType: fetched.ContentType, ByteSize: uint64(len(fetched.Body)), Width: width, Height: height, Position: imageItem.Position, IsCover: imageItem.IsCover, DownloadStatus: "completed", SHA256: hash}
			lock.Lock()
			assets = append(assets, asset)
			lock.Unlock()
		}()
	}
	wait.Wait()
	if err := ctx.Err(); err != nil {
		return nil, warnings, err
	}
	sort.SliceStable(assets, func(i, j int) bool { return assets[i].Position < assets[j].Position })
	if truncated {
		warnings = append(warnings, "图片数量达到上限，仅归档前 100 张")
	}
	return assets, warnings, nil
}

func failedAsset(image workspace.CollectedImage) workspace.StoredAsset {
	return workspace.StoredAsset{SourceURL: image.SourceURL, Position: image.Position, IsCover: image.IsCover, DownloadStatus: "failed"}
}

func imageDimensions(body []byte) (uint, uint) {
	config, _, err := image.DecodeConfig(bytes.NewReader(body))
	if err == nil && config.Width > 0 && config.Height > 0 {
		return uint(config.Width), uint(config.Height)
	}
	return webPDimensions(body)
}

func webPDimensions(body []byte) (uint, uint) {
	if len(body) < 30 || string(body[:4]) != "RIFF" || string(body[8:12]) != "WEBP" {
		return 0, 0
	}
	chunk, payload := string(body[12:16]), body[20:]
	read24 := func(value []byte) uint {
		return uint(value[0]) | uint(value[1])<<8 | uint(value[2])<<16
	}
	switch chunk {
	case "VP8X":
		if len(payload) >= 10 {
			return read24(payload[4:7]) + 1, read24(payload[7:10]) + 1
		}
	case "VP8L":
		if len(payload) >= 5 && payload[0] == 0x2f {
			bits := uint(payload[1]) | uint(payload[2])<<8 | uint(payload[3])<<16 | uint(payload[4])<<24
			return (bits & 0x3fff) + 1, ((bits >> 14) & 0x3fff) + 1
		}
	case "VP8 ":
		if len(payload) >= 10 && payload[3] == 0x9d && payload[4] == 0x01 && payload[5] == 0x2a {
			width := (uint(payload[6]) | uint(payload[7])<<8) & 0x3fff
			height := (uint(payload[8]) | uint(payload[9])<<8) & 0x3fff
			return width, height
		}
	}
	return 0, 0
}

func (s *Service) ListArticles(ctx context.Context, keyword, sourceType, status string, page, pageSize int) (workspace.Page[workspace.Article], error) {
	page, pageSize = clamp(page, pageSize)
	return s.store.ListArticles(ctx, strings.TrimSpace(keyword), sourceType, status, page, pageSize)
}

func (s *Service) Article(ctx context.Context, id uint64) (workspace.Article, error) {
	article, err := s.store.GetArticle(ctx, id)
	if err != nil {
		return article, err
	}
	for index := range article.Assets {
		if article.Assets[index].DownloadStatus == "completed" && article.Assets[index].ObjectKey != "" {
			article.Assets[index].MediaURL = s.SignMedia("assets", article.Assets[index].ID)
		}
	}
	if article.RawObjectKey != "" {
		article.RawSnapshotURL = s.SignMedia("raw", article.ID)
	}
	return article, nil
}

func (s *Service) ListJobs(ctx context.Context, status string, page, pageSize int) (workspace.Page[workspace.Job], error) {
	p, ps := clamp(page, pageSize)
	return s.store.ListJobs(ctx, status, p, ps)
}
func (s *Service) Job(ctx context.Context, id uint64) (workspace.Job, error) {
	return s.store.GetJob(ctx, id, true)
}
func (s *Service) Dashboard(ctx context.Context) (workspace.Dashboard, error) {
	return s.store.Dashboard(ctx)
}

func (s *Service) SignMedia(kind string, id uint64) string {
	expires := time.Now().Add(s.cfg.Auth.MediaURLTTL).Unix()
	payload := fmt.Sprintf("%s:%d:%d", kind, id, expires)
	mac := hmac.New(sha256.New, []byte(s.cfg.Auth.MediaSigningKey))
	_, _ = mac.Write([]byte(payload))
	return fmt.Sprintf("/media/%s/%d?expires=%d&signature=%s", kind, id, expires, hex.EncodeToString(mac.Sum(nil)))
}

func (s *Service) VerifyMedia(kind string, id uint64, expires int64, signature string) bool {
	if time.Now().Unix() > expires {
		return false
	}
	payload := fmt.Sprintf("%s:%d:%d", kind, id, expires)
	mac := hmac.New(sha256.New, []byte(s.cfg.Auth.MediaSigningKey))
	_, _ = mac.Write([]byte(payload))
	received, err := hex.DecodeString(signature)
	return err == nil && hmac.Equal(received, mac.Sum(nil))
}

func (s *Service) MediaObject(ctx context.Context, kind string, id uint64) (string, string, error) {
	switch kind {
	case "assets":
		asset, err := s.store.GetAsset(ctx, id)
		return asset.ObjectKey, asset.MediaType, err
	case "raw":
		article, err := s.store.GetArticle(ctx, id)
		return article.RawObjectKey, "application/gzip", err
	default:
		return "", "", workspace.ErrNotFound
	}
}

func (s *Service) ObjectStore() objectstore.Store { return s.objects }

func classify(err error) (string, string, bool) {
	var target *collectors.Error
	if errors.As(err, &target) {
		return target.Code, target.Message, target.Retryable
	}
	return "INTERNAL_ERROR", "采集处理失败", true
}
func extensionFor(mediaType string) string {
	switch mediaType {
	case "image/jpeg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	default:
		return path.Ext(mediaType)
	}
}
func truncateRunes(value string, limit int) string {
	if utf8.RuneCountInString(value) <= limit {
		return value
	}
	return string([]rune(value)[:limit])
}
func clamp(page, size int) (int, int) {
	if page < 1 {
		page = 1
	}
	if size < 1 {
		size = 20
	}
	if size > 100 {
		size = 100
	}
	return page, size
}
func ParseUint(value string) (uint64, error) { return strconv.ParseUint(value, 10, 64) }
