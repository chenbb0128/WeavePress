package editorial

import (
	"context"

	"github.com/chenbb0128/weavepress/server/internal/modules/workspace"
)

type ArticleStore interface {
	GetArticle(context.Context, uint64) (workspace.Article, error)
	GetAsset(context.Context, uint64) (workspace.Asset, error)
}

type Store interface {
	CreateDraft(context.Context, uint64, uint64, string, string, string, string, *uint64) (Draft, error)
	GetDraft(context.Context, uint64, bool) (Draft, error)
	ListDrafts(context.Context, string, string, int, int) (Page[Draft], error)
	ListDraftVersions(context.Context, uint64) ([]DraftVersion, error)
	GetDraftVersion(context.Context, uint64, uint) (DraftVersion, error)
	UpdateDraft(context.Context, uint64, uint64, UpdateInput) (Draft, error)
	RestoreDraftVersion(context.Context, uint64, uint64, uint, uint) (Draft, error)
	SetDraftStatus(context.Context, uint64, uint64, string, string, string) (Draft, error)

	CreatePublishJob(context.Context, uint64, uint64) (PublishJob, error)
	GetPublishJob(context.Context, uint64, bool) (PublishJob, error)
	ListPublishJobs(context.Context, string, int, int) (Page[PublishJob], error)
	SetPublishJobPublishing(context.Context, uint64) error
	SetPublishJobFailure(context.Context, uint64, string, string, bool) error
	CompletePublishJob(context.Context, uint64, string) error
	RetryPublishJob(context.Context, uint64, uint64) (PublishJob, error)
}
