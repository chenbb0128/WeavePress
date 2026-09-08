package editorial

import (
	"context"
	"errors"
	"testing"

	"github.com/chenbb0128/weavepress/server/internal/modules/workspace"
)

func TestProcessPublishesQueuedDraft(t *testing.T) {
	store := &fakeEditorialStore{
		job:   PublishJob{ID: 8, DraftID: 3, Status: PublishQueued},
		draft: Draft{ID: 3, Status: StatusPublishing},
	}
	publisher := &fakePublisher{result: PublishResult{RemoteMediaID: "wechat-draft-media"}}
	service := New(store, &fakeArticleStore{}, nil, publisher, true)

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
	service := New(store, &fakeArticleStore{}, nil, publisher, true)

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
	service := New(store, &fakeArticleStore{}, nil, &fakePublisher{}, true)

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
