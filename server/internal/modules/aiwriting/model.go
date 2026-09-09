package aiwriting

import (
	"errors"
	"time"

	"github.com/chenbb0128/weavepress/server/internal/modules/workspace"
)

const (
	JobTypeAnalysis   = "analysis"
	JobTypeGeneration = "generation"

	JobQueued    = "queued"
	JobRunning   = "running"
	JobCompleted = "completed"
	JobFailed    = "failed"

	AnalysisPromptV1   = "analysis-v1"
	GenerationPromptV1 = "generation-v1"

	TaskAnalyze  = "ai:analyze"
	TaskGenerate = "ai:generate"
)

var (
	ErrNotConfigured          = errors.New("AI is not configured")
	ErrInputTooLarge          = errors.New("AI input is too large")
	ErrArticleNotReady        = errors.New("article is not ready for AI writing")
	ErrAnalysisNotReady       = errors.New("AI analysis is not ready")
	ErrInvalidParameters      = errors.New("AI generation parameters are invalid")
	ErrOutputInvalid          = errors.New("AI output is invalid")
	ErrSourceReferenceInvalid = errors.New("AI source reference is invalid")
	ErrQuoteMismatch          = errors.New("AI quote does not match its source")
	ErrAssetInvalid           = errors.New("AI asset is invalid")
	ErrExcessiveSourceOverlap = errors.New("AI output overlaps the source excessively")
	ErrJobNotRetryable        = errors.New("AI job is not retryable")
)

var AllowedTones = map[string]struct{}{
	"professional": {},
	"plain":        {},
	"analytical":   {},
	"storytelling": {},
	"warm":         {},
}

type TokenUsage struct {
	InputTokens  int `json:"inputTokens"`
	OutputTokens int `json:"outputTokens"`
	TotalTokens  int `json:"totalTokens"`
}

type SourceBlock struct {
	ID      string  `json:"id"`
	Type    string  `json:"type"`
	Text    string  `json:"text"`
	Level   int     `json:"level,omitempty"`
	AssetID *uint64 `json:"assetId,omitempty"`
	Alt     string  `json:"alt,omitempty"`
}

type SourceDocument struct {
	Article   workspace.Article      `json:"article"`
	PlainText string                 `json:"plainText"`
	Blocks    []SourceBlock          `json:"blocks"`
	BlockByID map[string]SourceBlock `json:"-"`
}

type Fact struct {
	ID             string   `json:"id"`
	Text           string   `json:"text"`
	SourceBlockIDs []string `json:"sourceBlockIds"`
	Confidence     string   `json:"confidence"`
}

type Viewpoint struct {
	ID             string   `json:"id"`
	Text           string   `json:"text"`
	Holder         string   `json:"holder"`
	SourceBlockIDs []string `json:"sourceBlockIds"`
}

type Quote struct {
	ID            string `json:"id"`
	Text          string `json:"text"`
	SourceBlockID string `json:"sourceBlockId"`
}

type Risk struct {
	ID             string   `json:"id"`
	Text           string   `json:"text"`
	SourceBlockIDs []string `json:"sourceBlockIds"`
}

type Angle struct {
	ID      string   `json:"id"`
	Title   string   `json:"title"`
	Thesis  string   `json:"thesis"`
	Outline []string `json:"outline"`
}

type AnalysisOutput struct {
	Summary    string      `json:"summary"`
	Facts      []Fact      `json:"facts"`
	Viewpoints []Viewpoint `json:"viewpoints"`
	Quotes     []Quote     `json:"quotes"`
	Risks      []Risk      `json:"risks"`
	Angles     []Angle     `json:"angles"`
}

type GeneratedBlock struct {
	Type    string   `json:"type"`
	Level   int      `json:"level,omitempty"`
	Text    string   `json:"text,omitempty"`
	Items   []string `json:"items,omitempty"`
	FactIDs []string `json:"factIds,omitempty"`
	QuoteID string   `json:"quoteId,omitempty"`
	AssetID *uint64  `json:"assetId,omitempty"`
	Alt     string   `json:"alt,omitempty"`
}

type GenerationOutput struct {
	Title  string           `json:"title"`
	Digest string           `json:"digest"`
	Blocks []GeneratedBlock `json:"blocks"`
}

type GenerationParams struct {
	AngleID                string `json:"angleId"`
	Audience               string `json:"audience"`
	Tone                   string `json:"tone"`
	TargetWords            int    `json:"targetWords"`
	AdditionalInstructions string `json:"additionalInstructions"`
	IdempotencyKey         string `json:"idempotencyKey"`
}

type Status struct {
	Enabled  bool   `json:"enabled"`
	Provider string `json:"provider"`
	Model    string `json:"model"`
}

type Job struct {
	ID               uint64   `json:"id"`
	Type             string   `json:"type"`
	ArticleID        uint64   `json:"articleId"`
	ParentJobID      *uint64  `json:"parentJobId,omitempty"`
	DraftID          *uint64  `json:"draftId,omitempty"`
	RequestedBy      uint64   `json:"requestedBy"`
	Status           string   `json:"status"`
	Attempts         uint     `json:"attempts"`
	ManualRetries    uint     `json:"manualRetries"`
	InputFingerprint [32]byte `json:"-"`
	Provider         string   `json:"provider"`
	Model            string   `json:"model"`
	PromptVersion    string   `json:"promptVersion"`
	TokenUsage
	ErrorCode    string             `json:"errorCode,omitempty"`
	ErrorMessage string             `json:"errorMessage,omitempty"`
	Retryable    bool               `json:"retryable"`
	StartedAt    *time.Time         `json:"startedAt,omitempty"`
	FinishedAt   *time.Time         `json:"finishedAt,omitempty"`
	CreatedAt    time.Time          `json:"createdAt"`
	UpdatedAt    time.Time          `json:"updatedAt"`
	Article      *workspace.Article `json:"article,omitempty"`
	Events       []JobEvent         `json:"events,omitempty"`
}

type JobEvent struct {
	ID        uint64    `json:"id"`
	JobID     uint64    `json:"jobId"`
	Status    string    `json:"status"`
	Message   string    `json:"message"`
	CreatedAt time.Time `json:"createdAt"`
}

type Analysis struct {
	ID        uint64 `json:"id"`
	JobID     uint64 `json:"jobId"`
	ArticleID uint64 `json:"articleId"`
	AnalysisOutput
	CreatedAt time.Time `json:"createdAt"`
	Job       *Job      `json:"job,omitempty"`
}

type Generation struct {
	ID                     uint64 `json:"id"`
	JobID                  uint64 `json:"jobId"`
	AnalysisID             uint64 `json:"analysisId"`
	AngleID                string `json:"angleId"`
	Audience               string `json:"audience"`
	Tone                   string `json:"tone"`
	TargetWords            int    `json:"targetWords"`
	AdditionalInstructions string `json:"additionalInstructions"`
	GenerationOutput
	FactMap     map[string][]string `json:"factMap,omitempty"`
	ContentHTML string              `json:"contentHtml,omitempty"`
	DraftID     *uint64             `json:"draftId,omitempty"`
	CreatedAt   time.Time           `json:"createdAt"`
	UpdatedAt   time.Time           `json:"updatedAt"`
	Job         *Job                `json:"job,omitempty"`
}

type JobFilter struct {
	Type      string `json:"type,omitempty"`
	Status    string `json:"status,omitempty"`
	ArticleID uint64 `json:"articleId,omitempty"`
}

type Page[T any] struct {
	Items    []T   `json:"items"`
	Total    int64 `json:"total"`
	Page     int   `json:"page"`
	PageSize int   `json:"pageSize"`
}
