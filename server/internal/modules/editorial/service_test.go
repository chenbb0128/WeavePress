package editorial

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/chenbb0128/weavepress/server/internal/modules/workspace"
)

func TestProcessPublishesQueuedDraft(t *testing.T) {
	store := &fakeEditorialStore{
		job:   PublishJob{ID: 8, DraftID: 3, Status: PublishQueued},
		draft: Draft{ID: 3, Status: StatusPublishing},
	}
	publisher := &fakePublisher{result: PublishResult{RemoteMediaID: "wechat-draft-media"}}
	service := New(store, &fakeArticleStore{}, nil, publisher, nil, true)

	if err := service.process(context.Background(), store.job.ID); err != nil {
		t.Fatal(err)
	}
	if publisher.calls != 1 || store.job.Status != PublishCompleted || store.job.RemoteMediaID != "wechat-draft-media" {
		t.Fatalf("publisher calls=%d, job=%#v", publisher.calls, store.job)
	}
}

func TestProcessCanResumePublishingJobAfterWorkerRestart(t *testing.T) {
	store := &fakeEditorialStore{
		job:   PublishJob{ID: 9, DraftID: 4, Status: PublishPublishing, Attempts: 1},
		draft: Draft{ID: 4, Status: StatusPublishing},
	}
	publisher := &fakePublisher{result: PublishResult{RemoteMediaID: "resumed-media"}}
	service := New(store, &fakeArticleStore{}, nil, publisher, nil, true)

	if err := service.process(context.Background(), store.job.ID); err != nil {
		t.Fatal(err)
	}
	if store.setPublishingCalls != 0 || store.job.Status != PublishCompleted {
		t.Fatalf("publishing calls=%d, job=%#v", store.setPublishingCalls, store.job)
	}
}

func TestClassifyPublishPreservesPublisherError(t *testing.T) {
	want := &PublishError{Code: "WECHAT_40125", Message: "invalid appsecret", Retryable: false}
	code, message, retryable := classifyPublish(want)
	if code != want.Code || message != want.Message || retryable {
		t.Fatalf("classifyPublish() = %q, %q, %t", code, message, retryable)
	}
}

func TestPublishRejectsDraftThatFailsPreflight(t *testing.T) {
	store := &fakeEditorialStore{draft: Draft{ID: 5, Status: StatusApproved}}
	service := New(store, &fakeArticleStore{}, nil, &fakePublisher{}, nil, true)

	_, err := service.Publish(context.Background(), store.draft.ID, 1)
	if !errors.Is(err, ErrDraftPreflightFailed) {
		t.Fatalf("Publish() error = %v", err)
	}
	if store.createPublishCalls != 0 {
		t.Fatalf("publish jobs created = %d", store.createPublishCalls)
	}
}

type fakePublisher struct {
	calls  int
	result PublishResult
	err    error
}

func (p *fakePublisher) Publish(context.Context, Draft) (PublishResult, error) {
	p.calls++
	return p.result, p.err
}

type fakeArticleStore struct {
	article workspace.Article
	asset   workspace.Asset
}

func (s *fakeArticleStore) GetArticle(context.Context, uint64) (workspace.Article, error) {
	return s.article, nil
}

func (s *fakeArticleStore) GetAsset(context.Context, uint64) (workspace.Asset, error) {
	return s.asset, nil
}

type fakeEditorialStore struct {
	job                PublishJob
	draft              Draft
	assets             []DraftAsset
	createAssetErr     error
	createAssetCalls   int
	createPublishCalls int
	setPublishingCalls int
}

func (s *fakeEditorialStore) CreateDraft(context.Context, uint64, uint64, string, string, string, string, *uint64) (Draft, error) {
	return Draft{}, errors.New("not implemented")
}

func (s *fakeEditorialStore) GetDraft(context.Context, uint64, bool) (Draft, error) {
	return s.draft, nil
}

func (s *fakeEditorialStore) ListDrafts(context.Context, string, string, int, int) (Page[Draft], error) {
	return Page[Draft]{}, errors.New("not implemented")
}

func (s *fakeEditorialStore) ListDraftVersions(context.Context, uint64) ([]DraftVersion, error) {
	return nil, errors.New("not implemented")
}

func (s *fakeEditorialStore) GetDraftVersion(context.Context, uint64, uint) (DraftVersion, error) {
	return DraftVersion{}, errors.New("not implemented")
}

func (s *fakeEditorialStore) UpdateDraft(context.Context, uint64, uint64, UpdateInput) (Draft, error) {
	return Draft{}, errors.New("not implemented")
}

func (s *fakeEditorialStore) RestoreDraftVersion(context.Context, uint64, uint64, uint, uint) (Draft, error) {
	return Draft{}, errors.New("not implemented")
}

func (s *fakeEditorialStore) SetDraftStatus(context.Context, uint64, uint64, string, string, string) (Draft, error) {
	return Draft{}, errors.New("not implemented")
}

func (s *fakeEditorialStore) EnsureArticleAssets(context.Context, uint64, uint64, uint64) error {
	return nil
}

func (s *fakeEditorialStore) ListDraftAssets(_ context.Context, draftID uint64) ([]DraftAsset, error) {
	result := make([]DraftAsset, 0, len(s.assets))
	for _, asset := range s.assets {
		if asset.DraftID == draftID {
			result = append(result, asset)
		}
	}
	return result, nil
}

func (s *fakeEditorialStore) GetDraftAsset(_ context.Context, draftID, assetID uint64) (DraftAsset, error) {
	for _, asset := range s.assets {
		if asset.DraftID == draftID && asset.ID == assetID {
			return asset, nil
		}
	}
	return DraftAsset{}, workspace.ErrNotFound
}

func (s *fakeEditorialStore) GetDraftAssetByID(_ context.Context, assetID uint64) (DraftAsset, error) {
	for _, asset := range s.assets {
		if asset.ID == assetID {
			return asset, nil
		}
	}
	return DraftAsset{}, workspace.ErrNotFound
}

func (s *fakeEditorialStore) CreateUploadedDraftAsset(_ context.Context, draftID, userID uint64, input NewDraftAsset) (DraftAsset, error) {
	s.createAssetCalls++
	if s.createAssetErr != nil {
		return DraftAsset{}, s.createAssetErr
	}
	if s.draft.Status != StatusEditing {
		return DraftAsset{}, ErrDraftNotEditable
	}
	asset := DraftAsset{
		ID: uint64(len(s.assets) + 1), DraftID: draftID, Origin: "upload", ObjectKey: input.ObjectKey,
		MediaType: input.MediaType, ByteSize: input.ByteSize, Width: input.Width, Height: input.Height,
		SHA256: input.SHA256, UploadedBy: userID,
		BodyEligible: input.ByteSize <= WeChatMaxContentImageSize, CoverEligible: input.ByteSize <= WeChatMaxCoverImageSize,
	}
	s.assets = append(s.assets, asset)
	return asset, nil
}

func (s *fakeEditorialStore) CreatePublishJob(context.Context, uint64, uint64) (PublishJob, error) {
	s.createPublishCalls++
	return PublishJob{}, errors.New("not implemented")
}

func (s *fakeEditorialStore) GetPublishJob(context.Context, uint64, bool) (PublishJob, error) {
	return s.job, nil
}

func (s *fakeEditorialStore) ListPublishJobs(context.Context, string, int, int) (Page[PublishJob], error) {
	return Page[PublishJob]{}, errors.New("not implemented")
}

func (s *fakeEditorialStore) SetPublishJobPublishing(context.Context, uint64) error {
	s.setPublishingCalls++
	s.job.Status = PublishPublishing
	s.job.Attempts++
	return nil
}

func (s *fakeEditorialStore) SetPublishJobFailure(context.Context, uint64, string, string, bool) error {
	s.job.Status = PublishFailed
	return nil
}

func (s *fakeEditorialStore) CompletePublishJob(_ context.Context, _ uint64, remoteMediaID string) error {
	s.job.Status = PublishCompleted
	s.job.RemoteMediaID = remoteMediaID
	return nil
}

func (s *fakeEditorialStore) RetryPublishJob(context.Context, uint64, uint64) (PublishJob, error) {
	return PublishJob{}, errors.New("not implemented")
}

type fakeAssetObjects struct {
	key       string
	body      []byte
	mediaType string
	calls     int
	err       error
}

func (s *fakeAssetObjects) Put(_ context.Context, key string, body []byte, mediaType string) error {
	s.calls++
	s.key, s.body, s.mediaType = key, append([]byte(nil), body...), mediaType
	return s.err
}

func TestDraftAssetUploadStoresValidatedImage(t *testing.T) {
	body := validDraftPNG(t, 20, 10)
	store := &fakeEditorialStore{draft: Draft{ID: 7, Status: StatusEditing, CurrentVersion: 3, ContentHTML: "<p>unchanged</p>"}}
	objects := &fakeAssetObjects{}
	service := New(store, &fakeArticleStore{}, nil, nil, objects, true)

	asset, err := service.UploadAsset(context.Background(), 7, 42, "header.png", "image/png; charset=binary", body)
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(body)
	wantSuffix := fmt.Sprintf("/%x.png", hash)
	if !strings.HasPrefix(objects.key, "drafts/") || !strings.HasSuffix(objects.key, wantSuffix) {
		t.Fatalf("object key = %q, want drafts/{yyyy}/{mm}/{sha256}.png", objects.key)
	}
	if objects.calls != 1 || objects.mediaType != "image/png" || string(objects.body) != string(body) {
		t.Fatalf("object write = calls %d, type %q, bytes %d", objects.calls, objects.mediaType, len(objects.body))
	}
	if store.createAssetCalls != 1 || asset.Origin != "upload" || asset.DraftID != 7 || asset.UploadedBy != 42 || !asset.BodyEligible || !asset.CoverEligible {
		t.Fatalf("asset = %#v, create calls = %d", asset, store.createAssetCalls)
	}
	if store.draft.CurrentVersion != 3 || store.draft.ContentHTML != "<p>unchanged</p>" {
		t.Fatalf("draft was changed: %#v", store.draft)
	}
}

func TestDraftAssetUploadAllowsCoverOnlyImage(t *testing.T) {
	body := append(validDraftPNG(t, 20, 10), make([]byte, WeChatMaxContentImageSize)...)
	store := &fakeEditorialStore{draft: Draft{ID: 8, Status: StatusEditing}}
	service := New(store, &fakeArticleStore{}, nil, nil, &fakeAssetObjects{}, true)

	asset, err := service.UploadAsset(context.Background(), 8, 42, "cover.png", "image/png", body)
	if err != nil {
		t.Fatal(err)
	}
	if asset.BodyEligible || !asset.CoverEligible {
		t.Fatalf("eligibility = body %t, cover %t", asset.BodyEligible, asset.CoverEligible)
	}
}

func TestDraftAssetUploadChecksDraftBeforeImageAndStorage(t *testing.T) {
	store := &fakeEditorialStore{draft: Draft{ID: 9, Status: StatusApproved}}
	objects := &fakeAssetObjects{}
	service := New(store, &fakeArticleStore{}, nil, nil, objects, true)

	_, err := service.UploadAsset(context.Background(), 9, 42, "fake.png", "image/png", []byte("not an image"))
	if !errors.Is(err, ErrDraftNotEditable) {
		t.Fatalf("UploadAsset() error = %v", err)
	}
	if objects.calls != 0 || store.createAssetCalls != 0 {
		t.Fatalf("side effects = object calls %d, create calls %d", objects.calls, store.createAssetCalls)
	}
}

func TestDraftAssetUploadDoesNotCreateRecordWhenStorageFails(t *testing.T) {
	store := &fakeEditorialStore{draft: Draft{ID: 10, Status: StatusEditing}}
	objects := &fakeAssetObjects{err: errors.New("storage unavailable")}
	service := New(store, &fakeArticleStore{}, nil, nil, objects, true)

	_, err := service.UploadAsset(context.Background(), 10, 42, "header.png", "image/png", validDraftPNG(t, 20, 10))
	if err == nil {
		t.Fatal("UploadAsset() error = nil")
	}
	if store.createAssetCalls != 0 {
		t.Fatalf("create calls = %d", store.createAssetCalls)
	}
}

func TestDraftAssetServiceUsesDraftAssetStore(t *testing.T) {
	store := &fakeEditorialStore{assets: []DraftAsset{{ID: 11, DraftID: 3, ObjectKey: "drafts/2026/09/image.webp", MediaType: "image/webp"}}}
	service := New(store, &fakeArticleStore{}, nil, nil, &fakeAssetObjects{}, true)

	assets, err := service.Assets(context.Background(), 3)
	if err != nil || len(assets) != 1 || assets[0].ID != 11 {
		t.Fatalf("Assets() = %#v, %v", assets, err)
	}
	key, mediaType, err := service.AssetObject(context.Background(), 11)
	if err != nil || key != "drafts/2026/09/image.webp" || mediaType != "image/webp" {
		t.Fatalf("AssetObject() = %q, %q, %v", key, mediaType, err)
	}
	if themes := service.Themes(); len(themes) != 6 {
		t.Fatalf("Themes() count = %d", len(themes))
	}
}
