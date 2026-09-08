package workspace

import "time"

type Role string

const (
	RoleAdmin  Role = "admin"
	RoleEditor Role = "editor"
)

type User struct {
	ID        uint64    `json:"id"`
	Username  string    `json:"username"`
	Nickname  string    `json:"realName"`
	Avatar    string    `json:"avatar"`
	Role      Role      `json:"role"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type UserWithPassword struct {
	User
	PasswordHash string
}

type RefreshToken struct {
	UserID    uint64
	FamilyID  string
	TokenHash [32]byte
	ExpiresAt time.Time
	RevokedAt *time.Time
}

type Block struct {
	Type      string  `json:"type"`
	Text      string  `json:"text,omitempty"`
	Level     int     `json:"level,omitempty"`
	AssetID   *uint64 `json:"assetId,omitempty"`
	SourceURL string  `json:"sourceUrl,omitempty"`
	Alt       string  `json:"alt,omitempty"`
}

type Article struct {
	ID             uint64         `json:"id"`
	OriginalURL    string         `json:"originalUrl"`
	CanonicalURL   string         `json:"canonicalUrl"`
	SourceType     string         `json:"sourceType"`
	Title          string         `json:"title"`
	Author         string         `json:"author"`
	SourceName     string         `json:"sourceName"`
	Language       string         `json:"language"`
	PublishedAt    *time.Time     `json:"publishedAt"`
	PlainText      string         `json:"plainText,omitempty"`
	CleanHTML      string         `json:"cleanHtml,omitempty"`
	Blocks         []Block        `json:"blocks,omitempty"`
	Metadata       map[string]any `json:"metadata,omitempty"`
	RawObjectKey   string         `json:"-"`
	Status         string         `json:"status"`
	DuplicateOfID  *uint64        `json:"duplicateOfId,omitempty"`
	CreatedBy      uint64         `json:"createdBy"`
	CreatedAt      time.Time      `json:"createdAt"`
	UpdatedAt      time.Time      `json:"updatedAt"`
	Assets         []Asset        `json:"assets,omitempty"`
	RawSnapshotURL string         `json:"rawSnapshotUrl,omitempty"`
}

type Asset struct {
	ID             uint64    `json:"id"`
	ArticleID      uint64    `json:"articleId"`
	SourceURL      string    `json:"sourceUrl,omitempty"`
	ObjectKey      string    `json:"-"`
	MediaType      string    `json:"mediaType"`
	ByteSize       uint64    `json:"byteSize"`
	Width          uint      `json:"width"`
	Height         uint      `json:"height"`
	Position       uint      `json:"position"`
	IsCover        bool      `json:"isCover"`
	DownloadStatus string    `json:"downloadStatus"`
	MediaURL       string    `json:"mediaUrl"`
	CreatedAt      time.Time `json:"createdAt"`
	SourceHash     [32]byte  `json:"-"`
	SHA256         [32]byte  `json:"-"`
}

type Job struct {
	ID            uint64     `json:"id"`
	ArticleID     uint64     `json:"articleId"`
	SubmittedBy   uint64     `json:"submittedBy"`
	Status        string     `json:"status"`
	Attempts      uint       `json:"attempts"`
	ManualRetries uint       `json:"manualRetries"`
	ErrorCode     string     `json:"errorCode,omitempty"`
	ErrorMessage  string     `json:"errorMessage,omitempty"`
	Warnings      []string   `json:"warnings"`
	StartedAt     *time.Time `json:"startedAt,omitempty"`
	FinishedAt    *time.Time `json:"finishedAt,omitempty"`
	CreatedAt     time.Time  `json:"createdAt"`
	UpdatedAt     time.Time  `json:"updatedAt"`
	Article       *Article   `json:"article,omitempty"`
	Events        []JobEvent `json:"events,omitempty"`
}

type JobEvent struct {
	ID        uint64    `json:"id"`
	Status    string    `json:"status"`
	Message   string    `json:"message"`
	CreatedAt time.Time `json:"createdAt"`
}

type Dashboard struct {
	ArticlesTotal  int64     `json:"articlesTotal"`
	CollectedToday int64     `json:"collectedToday"`
	ProcessingJobs int64     `json:"processingJobs"`
	FailedJobs     int64     `json:"failedJobs"`
	SourceWechat   int64     `json:"sourceWechat"`
	SourceWeb      int64     `json:"sourceWeb"`
	RecentArticles []Article `json:"recentArticles"`
}

type Page[T any] struct {
	Items    []T   `json:"items"`
	Total    int64 `json:"total"`
	Page     int   `json:"page"`
	PageSize int   `json:"pageSize"`
}

type CollectedArticle struct {
	Title       string
	Author      string
	SourceName  string
	Language    string
	PublishedAt *time.Time
	RawHTML     []byte
	CleanHTML   string
	PlainText   string
	Blocks      []Block
	Images      []CollectedImage
	CoverURL    string
	Metadata    map[string]any
}

type CollectedImage struct {
	SourceURL string
	Alt       string
	Position  uint
	IsCover   bool
}

type StoredAsset struct {
	SourceURL      string
	ObjectKey      string
	MediaType      string
	ByteSize       uint64
	Width          uint
	Height         uint
	Position       uint
	IsCover        bool
	DownloadStatus string
	SHA256         [32]byte
}
