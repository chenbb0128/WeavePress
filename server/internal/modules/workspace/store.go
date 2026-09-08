package workspace

import (
	"context"
	"errors"
	"time"
)

var (
	ErrNotFound         = errors.New("resource not found")
	ErrUsernameTaken    = errors.New("username already exists")
	ErrInvalidRefresh   = errors.New("invalid refresh token")
	ErrRefreshReuse     = errors.New("refresh token reuse detected")
	ErrArticleExists    = errors.New("article already exists")
	ErrJobNotRetryable  = errors.New("job is not retryable")
	ErrJobStateConflict = errors.New("job state transition conflict")
)

type Store interface {
	CreateUser(context.Context, string, string, string, Role, string) (User, error)
	GetUserByID(context.Context, uint64) (User, error)
	GetUserByUsername(context.Context, string) (UserWithPassword, error)
	ListUsers(context.Context, string, string, int, int) (Page[User], error)
	UpdateUser(context.Context, uint64, string, Role, string) (User, error)
	UpdatePassword(context.Context, uint64, string) error
	CreateRefreshToken(context.Context, uint64, string, [32]byte, time.Time, string, string) error
	RotateRefreshToken(context.Context, [32]byte, string, [32]byte, time.Time, string, string) (uint64, error)
	RevokeRefreshToken(context.Context, [32]byte) error
	RevokeUserTokens(context.Context, uint64) error

	CreateArticleJob(context.Context, string, string, [32]byte, string, uint64) (Article, Job, bool, error)
	GetArticle(context.Context, uint64) (Article, error)
	ListArticles(context.Context, string, string, string, int, int) (Page[Article], error)
	GetAsset(context.Context, uint64) (Asset, error)
	GetJob(context.Context, uint64, bool) (Job, error)
	ListJobs(context.Context, string, int, int) (Page[Job], error)
	SetJobStage(context.Context, uint64, string, string) error
	SetJobFailure(context.Context, uint64, string, string, bool) error
	RetryJob(context.Context, uint64) (Job, error)
	CompleteArticle(context.Context, uint64, uint64, CollectedArticle, string, [32]byte, []StoredAsset, []string) error
	Dashboard(context.Context) (Dashboard, error)
}
