package editorial

import (
	"context"
	"errors"
	"time"

	"github.com/chenbb0128/weavepress/server/internal/modules/workspace"
)

const (
	StatusEditing       = "editing"
	StatusInReview      = "in_review"
	StatusApproved      = "approved"
	StatusPublishing    = "publishing"
	StatusPublished     = "published"
	StatusPublishFailed = "publish_failed"

	PublishQueued     = "queued"
	PublishPublishing = "publishing"
	PublishCompleted  = "completed"
	PublishFailed     = "failed"
)

var (
	ErrDraftNotEditable     = errors.New("draft is not editable")
	ErrDraftStateConflict   = errors.New("draft state transition conflict")
	ErrDraftVersionConflict = errors.New("draft version conflict")
	ErrPublisherDisabled    = errors.New("wechat publisher is disabled")
	ErrPublishNotRetryable  = errors.New("publish job is not retryable")
	ErrCoverRequired        = errors.New("wechat cover asset is required")
	ErrDraftPreflightFailed = errors.New("draft preflight failed")
)

const (
	WeChatMaxTitleRunes       = 64
	WeChatMaxAuthorRunes      = 8
	WeChatMaxDigestRunes      = 120
	WeChatMaxContentImageSize = 1 << 20
	WeChatMaxCoverImageSize   = 10 << 20
)

type Draft struct {
	ID              uint64             `json:"id"`
	SourceArticleID uint64             `json:"sourceArticleId"`
	Title           string             `json:"title"`
	Author          string             `json:"author"`
	Digest          string             `json:"digest"`
	ContentHTML     string             `json:"contentHtml"`
	PreviewHTML     string             `json:"previewHtml,omitempty"`
	CoverAssetID    *uint64            `json:"coverAssetId,omitempty"`
	Status          string             `json:"status"`
	CurrentVersion  uint               `json:"currentVersion"`
	CreatedBy       uint64             `json:"createdBy"`
	UpdatedBy       uint64             `json:"updatedBy"`
	CreatedAt       time.Time          `json:"createdAt"`
	UpdatedAt       time.Time          `json:"updatedAt"`
	SourceArticle   *workspace.Article `json:"sourceArticle,omitempty"`
	Events          []DraftEvent       `json:"events,omitempty"`
}

type DraftEvent struct {
	ID         uint64    `json:"id"`
	DraftID    uint64    `json:"draftId"`
	ActorID    uint64    `json:"actorId"`
	FromStatus string    `json:"fromStatus"`
	ToStatus   string    `json:"toStatus"`
	Note       string    `json:"note"`
	CreatedAt  time.Time `json:"createdAt"`
}

type DraftVersion struct {
	ID           uint64    `json:"id"`
	DraftID      uint64    `json:"draftId"`
	Version      uint      `json:"version"`
	Title        string    `json:"title"`
	Author       string    `json:"author"`
	Digest       string    `json:"digest"`
	ContentHTML  string    `json:"contentHtml"`
	CoverAssetID *uint64   `json:"coverAssetId,omitempty"`
	ChangeNote   string    `json:"changeNote"`
	CreatedBy    uint64    `json:"createdBy"`
	CreatedAt    time.Time `json:"createdAt"`
}

type PublishJob struct {
	ID            uint64            `json:"id"`
	DraftID       uint64            `json:"draftId"`
	RequestedBy   uint64            `json:"requestedBy"`
	Status        string            `json:"status"`
	Attempts      uint              `json:"attempts"`
	ManualRetries uint              `json:"manualRetries"`
	RemoteMediaID string            `json:"remoteMediaId,omitempty"`
	ErrorCode     string            `json:"errorCode,omitempty"`
	ErrorMessage  string            `json:"errorMessage,omitempty"`
	StartedAt     *time.Time        `json:"startedAt,omitempty"`
	FinishedAt    *time.Time        `json:"finishedAt,omitempty"`
	CreatedAt     time.Time         `json:"createdAt"`
	UpdatedAt     time.Time         `json:"updatedAt"`
	Draft         *Draft            `json:"draft,omitempty"`
	Events        []PublishJobEvent `json:"events,omitempty"`
}

type PublishJobEvent struct {
	ID        uint64    `json:"id"`
	Status    string    `json:"status"`
	Message   string    `json:"message"`
	CreatedAt time.Time `json:"createdAt"`
}

type Page[T any] struct {
	Items    []T   `json:"items"`
	Total    int64 `json:"total"`
	Page     int   `json:"page"`
	PageSize int   `json:"pageSize"`
}

type UpdateInput struct {
	Title           string
	Author          string
	Digest          string
	ContentHTML     string
	CoverAssetID    *uint64
	ExpectedVersion uint
	ChangeNote      string
}

type PreflightIssue struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Field   string `json:"field,omitempty"`
}

type PreflightResult struct {
	Valid  bool             `json:"valid"`
	Issues []PreflightIssue `json:"issues"`
}

type PreflightError struct {
	Result PreflightResult
}

func (e *PreflightError) Error() string {
	if len(e.Result.Issues) > 0 {
		return ErrDraftPreflightFailed.Error() + ": " + e.Result.Issues[0].Message
	}
	return ErrDraftPreflightFailed.Error()
}

func (e *PreflightError) Unwrap() error { return ErrDraftPreflightFailed }

type PublishResult struct {
	RemoteMediaID string
}

type PublishError struct {
	Code      string
	Message   string
	Retryable bool
	Cause     error
}

func (e *PublishError) Error() string {
	if e.Cause != nil {
		return e.Message + ": " + e.Cause.Error()
	}
	return e.Message
}

func (e *PublishError) Unwrap() error { return e.Cause }

type Publisher interface {
	Publish(context.Context, Draft) (PublishResult, error)
}
