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

func TestPublishProcessPublishesQueuedDraft(t *testing.T) {
	coverID := uint64(8)
	store := &fakeEditorialStore{
		job:    PublishJob{ID: 8, DraftID: 3, Status: PublishQueued},
		draft:  Draft{ID: 3, Status: StatusPublishing, CurrentVersion: 2, Title: "标题", ContentHTML: `<p style="color:#222">已保存正文</p>`, CoverAssetID: &coverID, EditorDocument: savedDocument(t, `{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"已保存正文"}]}]}`), ThemeID: DefaultThemeID, ThemeVersion: 1},
		assets: []DraftAsset{{ID: 8, DraftID: 3, ObjectKey: "cover.webp", MediaType: "image/webp", ByteSize: 8, BodyEligible: true, CoverEligible: true}},
	}
	publisher := &fakePublisher{result: PublishResult{RemoteMediaID: "wechat-draft-media"}}
	service := New(store, &fakeArticleStore{}, nil, publisher, nil, true)

	if err := service.process(context.Background(), store.job.ID); err != nil {
		t.Fatal(err)
	}
	if publisher.calls != 1 || store.job.Status != PublishCompleted || store.job.RemoteMediaID != "wechat-draft-media" {
		t.Fatalf("publisher calls=%d, job=%#v", publisher.calls, store.job)
	}
	if publisher.draft.EditorDocument == nil || len(publisher.draft.Assets) != 1 || !strings.Contains(publisher.draft.ContentHTML, `style=`) || store.updateCalls != 0 {
		t.Fatalf("published draft = %#v", publisher.draft)
	}
}

func TestPublishProcessCanResumePublishingJobAfterWorkerRestart(t *testing.T) {
	coverID := uint64(8)
	store := &fakeEditorialStore{
		job:    PublishJob{ID: 9, DraftID: 4, Status: PublishPublishing, Attempts: 1},
		draft:  Draft{ID: 4, Status: StatusPublishing, CurrentVersion: 2, Title: "标题", ContentHTML: `<p>正文</p>`, CoverAssetID: &coverID, EditorDocument: savedDocument(t, `{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"正文"}]}]}`), ThemeID: DefaultThemeID, ThemeVersion: 1},
		assets: []DraftAsset{{ID: 8, DraftID: 4, ObjectKey: "cover.png", MediaType: "image/png", ByteSize: 8, BodyEligible: true, CoverEligible: true}},
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
	draft  Draft
	result PublishResult
	err    error
}

func (p *fakePublisher) Publish(_ context.Context, draft Draft) (PublishResult, error) {
	p.calls++
	p.draft = draft
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
	updated            UpdateInput
	historical         DraftVersion
	updateCalls        int
	statusCalls        int
	beforeStatus       func()
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
	return s.historical, nil
}

func (s *fakeEditorialStore) UpdateDraft(_ context.Context, _ uint64, _ uint64, input UpdateInput) (Draft, error) {
	if input.ExpectedVersion != s.draft.CurrentVersion {
		return Draft{}, ErrDraftVersionConflict
	}
	s.updateCalls++
	s.updated = input
	s.draft.Title, s.draft.Author, s.draft.Digest = input.Title, input.Author, input.Digest
	s.draft.EditorDocument, s.draft.ContentHTML = input.EditorDocument, input.ContentHTML
	s.draft.ThemeID, s.draft.ThemeVersion, s.draft.CoverAssetID = input.ThemeID, input.ThemeVersion, input.CoverAssetID
	s.draft.CurrentVersion++
	return s.draft, nil
}

func (s *fakeEditorialStore) RestoreDraftVersion(ctx context.Context, id, userID uint64, input RestoreInput) (Draft, error) {
	if input.TargetVersion >= s.draft.CurrentVersion {
		return Draft{}, ErrDraftVersionConflict
	}
	return s.UpdateDraft(ctx, id, userID, UpdateInput{Title: input.Title, Author: input.Author, Digest: input.Digest, EditorDocument: input.EditorDocument, ThemeID: input.ThemeID, ThemeVersion: input.ThemeVersion, ContentHTML: input.ContentHTML, CoverAssetID: input.CoverAssetID, ExpectedVersion: input.ExpectedVersion})
}

func (s *fakeEditorialStore) SetDraftStatus(_ context.Context, _ uint64, _ uint64, from, to, _ string, expectedVersion uint) (Draft, error) {
	if s.beforeStatus != nil {
		s.beforeStatus()
	}
	if s.draft.Status != from {
		return Draft{}, ErrDraftStateConflict
	}
	if expectedVersion != 0 && s.draft.CurrentVersion != expectedVersion {
		return Draft{}, ErrDraftVersionConflict
	}
	s.statusCalls++
	s.draft.Status = to
	return s.draft, nil
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

func savedDocument(t *testing.T, body string) *Document {
	t.Helper()
	doc, err := ParseDocument([]byte(body))
	if err != nil {
		t.Fatal(err)
	}
	return &doc
}

func TestUpdateRendersTrustedDocument(t *testing.T) {
	store := &fakeEditorialStore{draft: Draft{ID: 1, Status: StatusEditing, CurrentVersion: 1}}
	doc := savedDocument(t, `{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"正文"}]}]}`)
	service := New(store, &fakeArticleStore{}, nil, nil, nil, false)
	saved, err := service.Update(context.Background(), 1, 7, UpdateInput{Title: "标题", EditorDocument: doc, ThemeID: "clear-blue", ThemeVersion: 99, ContentHTML: "<p>客户端 HTML</p>", ExpectedVersion: 1})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(saved.ContentHTML, `style=`) || !strings.Contains(saved.ContentHTML, "正文") || strings.Contains(saved.ContentHTML, "客户端") || saved.ThemeVersion != 1 || saved.CurrentVersion != 2 {
		t.Fatalf("saved = %#v", saved)
	}
	if saved.ContentHTML != store.updated.ContentHTML {
		t.Fatal("response must use server render")
	}
	_, err = service.Update(context.Background(), 1, 7, UpdateInput{Title: "标题", EditorDocument: doc, ThemeID: "clear-blue", ExpectedVersion: 1})
	if !errors.Is(err, ErrDraftVersionConflict) || store.updateCalls != 1 {
		t.Fatalf("conflict = %v, writes = %d", err, store.updateCalls)
	}
}

func TestGetLegacyHydratesWithoutSaving(t *testing.T) {
	articleAssetID := uint64(9)
	store := &fakeEditorialStore{draft: Draft{ID: 1, CurrentVersion: 2, ContentHTML: `<p>旧稿</p><img data-weavepress-asset-id="9">`}, assets: []DraftAsset{{ID: 81, DraftID: 1, ArticleAssetID: &articleAssetID, ObjectKey: "body.png", MediaType: "image/png", ByteSize: 1, BodyEligible: true, CoverEligible: true}}}
	got, err := New(store, &fakeArticleStore{}, nil, nil, nil, false).Get(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if got.EditorDocument == nil || !got.MigrationNeeded || got.ThemeID != DefaultThemeID || len(got.Assets) != 1 {
		t.Fatalf("draft = %#v", got)
	}
	if ids := ReferencedDraftAssetIDs(*got.EditorDocument); len(ids) != 1 || ids[0] != 81 {
		t.Fatalf("image IDs = %v", ids)
	}
	if !strings.Contains(got.ContentHTML, `data-weavepress-draft-asset-id="81"`) || store.draft.EditorDocument != nil || store.updateCalls != 0 || store.draft.CurrentVersion != 2 {
		t.Fatalf("hydrate mutated saved draft: %#v", store.draft)
	}
}

func TestGetLegacyConversionFailureIsReadOnly(t *testing.T) {
	store := &fakeEditorialStore{draft: Draft{ID: 1, ContentHTML: `<p> </p>`}}
	got, err := New(store, &fakeArticleStore{}, nil, nil, nil, false).Get(context.Background(), 1)
	if err != nil || got.EditorDocument != nil || !got.MigrationNeeded || len(got.MigrationWarnings) == 0 || got.ContentHTML != `<p> </p>` || store.updateCalls != 0 {
		t.Fatalf("draft = %#v, err = %v", got, err)
	}
}

func TestUpdateValidatesDraftAssets(t *testing.T) {
	for _, test := range []struct {
		name  string
		asset DraftAsset
		image bool
		want  error
	}{
		{"foreign", DraftAsset{ID: 8, DraftID: 2, ObjectKey: "a.png", MediaType: "image/png", BodyEligible: true, CoverEligible: true}, true, ErrDraftAssetInvalid},
		{"body too large", DraftAsset{ID: 8, DraftID: 1, ObjectKey: "a.webp", MediaType: "image/webp", ByteSize: WeChatMaxContentImageSize + 1, CoverEligible: true}, true, ErrDraftAssetTooLarge},
		{"large cover", DraftAsset{ID: 8, DraftID: 1, ObjectKey: "a.webp", MediaType: "image/webp", ByteSize: WeChatMaxContentImageSize + 1, CoverEligible: true}, false, nil},
		{"unavailable", DraftAsset{ID: 8, DraftID: 1, MediaType: "image/png", BodyEligible: true, CoverEligible: true}, true, ErrDraftAssetInvalid},
		{"invalid cover", DraftAsset{ID: 8, DraftID: 1, ObjectKey: "a.png", MediaType: "image/png"}, false, ErrDraftAssetInvalid},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := &fakeEditorialStore{draft: Draft{ID: 1, CurrentVersion: 1}, assets: []DraftAsset{test.asset}}
			doc := savedDocument(t, `{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"正文"}]}]}`)
			if test.image {
				doc.Content = append(doc.Content, imageNode(8))
			}
			cover := uint64(8)
			_, err := New(store, &fakeArticleStore{}, nil, nil, nil, false).Update(context.Background(), 1, 7, UpdateInput{Title: "标题", EditorDocument: doc, ThemeID: DefaultThemeID, CoverAssetID: &cover, ExpectedVersion: 1})
			if !errors.Is(err, test.want) {
				t.Fatalf("error = %v, want %v", err, test.want)
			}
			if test.want != nil && store.updateCalls != 0 {
				t.Fatal("invalid input saved")
			}
		})
	}
}

func TestRestoreLegacyCreatesStructuredVersion(t *testing.T) {
	coverID := uint64(8)
	articleCoverID := uint64(101)
	store := &fakeEditorialStore{draft: Draft{ID: 1, CurrentVersion: 3}, historical: DraftVersion{DraftID: 1, Version: 1, Title: "历史标题", Author: "历史作者", Digest: "历史摘要", ContentHTML: `<p>历史正文</p>`, CoverAssetID: &coverID, LegacyCoverAssetID: &articleCoverID}, assets: []DraftAsset{{ID: 8, DraftID: 1, ArticleAssetID: &articleCoverID, ObjectKey: "cover.webp", MediaType: "image/webp", ByteSize: 8, BodyEligible: true, CoverEligible: true}}}
	got, err := New(store, &fakeArticleStore{}, nil, nil, nil, false).RestoreVersion(context.Background(), 1, 7, 1, 3)
	if err != nil {
		t.Fatal(err)
	}
	if got.EditorDocument == nil || got.ThemeID != DefaultThemeID || got.ThemeVersion != 1 || got.CurrentVersion != 4 || got.Title != "历史标题" || got.Author != "历史作者" || got.Digest != "历史摘要" || got.CoverAssetID == nil || *got.CoverAssetID != 8 || !strings.Contains(got.ContentHTML, `style=`) {
		t.Fatalf("restored = %#v", got)
	}
}

func TestRestoreLegacyRejectsUnconvertibleAndStaleVersions(t *testing.T) {
	store := &fakeEditorialStore{draft: Draft{ID: 1, CurrentVersion: 3}, historical: DraftVersion{DraftID: 1, Version: 1, ContentHTML: `<p> </p>`}}
	service := New(store, &fakeArticleStore{}, nil, nil, nil, false)
	if _, err := service.RestoreVersion(context.Background(), 1, 7, 1, 2); !errors.Is(err, ErrDraftVersionConflict) {
		t.Fatalf("stale restore = %v", err)
	}
	if _, err := service.RestoreVersion(context.Background(), 1, 7, 1, 3); !errors.Is(err, ErrLegacyConvertFailed) {
		t.Fatalf("invalid legacy restore = %v", err)
	}
	if store.updateCalls != 0 || store.draft.CurrentVersion != 3 {
		t.Fatal("failed restore changed draft")
	}
}

func TestRestoreUsesExactHistoricalTheme(t *testing.T) {
	doc := savedDocument(t, `{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"历史正文"}]}]}`)
	store := &fakeEditorialStore{draft: Draft{ID: 1, CurrentVersion: 3}, historical: DraftVersion{DraftID: 1, Version: 1, Title: "标题", EditorDocument: doc, ThemeID: "clear-blue", ThemeVersion: 2, ContentHTML: "untrusted"}}
	service := New(store, &fakeArticleStore{}, nil, nil, nil, false)
	if _, err := service.RestoreVersion(context.Background(), 1, 7, 1, 3); !errors.Is(err, ErrThemeNotFound) || store.updateCalls != 0 {
		t.Fatalf("unknown historical theme = %v", err)
	}
	store.historical.ThemeVersion = 1
	got, err := service.RestoreVersion(context.Background(), 1, 7, 1, 3)
	if err != nil || got.ThemeID != "clear-blue" || !strings.Contains(got.ContentHTML, "历史正文") || strings.Contains(got.ContentHTML, "untrusted") {
		t.Fatalf("restored = %#v, %v", got, err)
	}
}

func TestPreflightSubmitReviewUsesSavedDraftAssets(t *testing.T) {
	cover := uint64(8)
	store := &fakeEditorialStore{draft: Draft{ID: 1, Title: "标题", Status: StatusEditing, CurrentVersion: 2, ContentHTML: `<p>正文</p>`, CoverAssetID: &cover, EditorDocument: savedDocument(t, `{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"正文"}]}]}`), ThemeID: DefaultThemeID, ThemeVersion: 1}, assets: []DraftAsset{{ID: 8, DraftID: 1, ObjectKey: "a.gif", MediaType: "image/gif", ByteSize: 3, BodyEligible: true, CoverEligible: true}}}
	service := New(store, &fakeArticleStore{}, nil, nil, nil, false)
	result, err := service.Preflight(context.Background(), 1)
	if err != nil || !result.Valid {
		t.Fatalf("preflight = %#v, %v", result, err)
	}
	assets := store.assets
	store.assets = nil
	if _, err = service.SubmitReview(context.Background(), 1, 7); !errors.Is(err, ErrDraftPreflightFailed) || store.statusCalls != 0 {
		t.Fatalf("review error = %v, transitions = %d", err, store.statusCalls)
	}
	store.assets = assets
	got, err := service.SubmitReview(context.Background(), 1, 7)
	if err != nil || got.Status != StatusInReview || store.statusCalls != 1 {
		t.Fatalf("review = %#v, %v", got, err)
	}
}

func TestPublishProcessRejectsInvalidSavedDraft(t *testing.T) {
	store := &fakeEditorialStore{draft: Draft{ID: 1, Title: "标题", ContentHTML: "<p> </p>"}, job: PublishJob{ID: 8, DraftID: 1, Status: PublishQueued}}
	publisher := &fakePublisher{}
	err := New(store, &fakeArticleStore{}, nil, publisher, nil, true).process(context.Background(), 8)
	if !errors.Is(err, ErrDraftPreflightFailed) || publisher.calls != 0 {
		t.Fatalf("process error = %v, publishes = %d", err, publisher.calls)
	}
	if _, _, retryable := classifyPublish(err); retryable {
		t.Fatal("invalid saved draft must not be retried")
	}
}

func TestUpdateRequiresDocumentAndKnownTheme(t *testing.T) {
	for _, test := range []struct {
		name  string
		doc   *Document
		theme string
		want  error
	}{
		{"HTML only", nil, DefaultThemeID, ErrDocumentInvalid},
		{"unknown theme", &Document{Type: "doc"}, "unknown", ErrThemeNotFound},
		{"invalid document", &Document{Type: "script"}, DefaultThemeID, ErrDocumentInvalid},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := &fakeEditorialStore{draft: Draft{ID: 1, CurrentVersion: 1}}
			_, err := New(store, &fakeArticleStore{}, nil, nil, nil, false).Update(context.Background(), 1, 7, UpdateInput{Title: "标题", ContentHTML: "<p>不能直存</p>", EditorDocument: test.doc, ThemeID: test.theme, ExpectedVersion: 1})
			if !errors.Is(err, test.want) || store.updateCalls != 0 {
				t.Fatalf("error = %v, writes = %d", err, store.updateCalls)
			}
		})
	}
}

func TestGetLegacyPreviewDoesNotSignArticleIDsAsDraftAssets(t *testing.T) {
	preview := DraftPreviewHTML(`<p>正文</p><img data-weavepress-asset-id="11"><img data-weavepress-draft-asset-id="12">`, func(id uint64) string {
		return fmt.Sprintf("/draft-assets/%d", id)
	})
	if strings.Contains(preview, `/draft-assets/11`) || !strings.Contains(preview, `src="/draft-assets/12"`) {
		t.Fatalf("preview = %s", preview)
	}
}

func TestPreflightLegacyRequiresSaveBeforeReviewAndPublish(t *testing.T) {
	for _, action := range []string{"preflight", "review", "process", "publish"} {
		t.Run(action, func(t *testing.T) {
			coverID := uint64(8)
			store := &fakeEditorialStore{draft: Draft{ID: 1, Title: "标题", Status: StatusEditing, CurrentVersion: 1, ContentHTML: `<p>可转换旧正文</p>`, CoverAssetID: &coverID},
				assets: []DraftAsset{{ID: 8, DraftID: 1, ObjectKey: "cover.png", MediaType: "image/png", ByteSize: 8, BodyEligible: true, CoverEligible: true}},
				job:    PublishJob{ID: 9, DraftID: 1, Status: PublishQueued},
			}
			publisher := &fakePublisher{}
			service := New(store, &fakeArticleStore{}, nil, publisher, nil, true)
			var result PreflightResult
			var err error
			switch action {
			case "preflight":
				result, err = service.Preflight(context.Background(), 1)
			case "review":
				_, err = service.SubmitReview(context.Background(), 1, 7)
			case "process":
				err = service.process(context.Background(), 9)
			case "publish":
				_, err = service.Publish(context.Background(), 1, 7)
			}
			if action != "preflight" {
				var preflightErr *PreflightError
				if !errors.As(err, &preflightErr) {
					t.Fatalf("expected preflight rejection, got %v", err)
				}
				result = preflightErr.Result
			} else if err != nil {
				t.Fatal(err)
			}
			if result.Valid || !hasIssue(result, "EDITOR_DOCUMENT_REQUIRED") {
				t.Fatalf("result = %#v", result)
			}
			if store.draft.Status != StatusEditing || store.draft.CurrentVersion != 1 || store.updateCalls != 0 || store.statusCalls != 0 || store.job.Status != PublishQueued || store.setPublishingCalls != 0 || store.createPublishCalls != 0 || publisher.calls != 0 {
				t.Fatalf("rejected legacy draft changed state: draft=%#v, job=%#v, publisher=%d", store.draft, store.job, publisher.calls)
			}
		})
	}
}

func TestSubmitReviewRejectsConcurrentSaveAfterPreflight(t *testing.T) {
	coverID := uint64(8)
	store := &fakeEditorialStore{draft: Draft{ID: 1, Title: "标题", Status: StatusEditing, CurrentVersion: 2, ContentHTML: `<p>已保存正文</p>`, CoverAssetID: &coverID,
		EditorDocument: savedDocument(t, `{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"已保存正文"}]}]}`)},
		assets: []DraftAsset{{ID: 8, DraftID: 1, ObjectKey: "cover.png", MediaType: "image/png", ByteSize: 8, BodyEligible: true, CoverEligible: true}},
	}
	store.beforeStatus = func() {
		store.draft.CurrentVersion = 3
		store.draft.Title = "并发保存的新标题"
	}
	_, err := New(store, &fakeArticleStore{}, nil, nil, nil, false).SubmitReview(context.Background(), 1, 7)
	if !errors.Is(err, ErrDraftVersionConflict) {
		t.Fatalf("expected version conflict, got %v", err)
	}
	if store.draft.Status != StatusEditing || store.statusCalls != 0 || store.draft.CurrentVersion != 3 || store.draft.Title != "并发保存的新标题" {
		t.Fatalf("unchecked version was reviewed or overwritten: %#v", store.draft)
	}
}
