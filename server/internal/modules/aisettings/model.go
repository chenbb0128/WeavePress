package aisettings

import (
	"errors"
	"time"
)

const (
	ProviderZhipu            = "zhipu"
	ProviderOpenAI           = "openai"
	ProviderOpenAICompatible = "openai-compatible"

	zhipuBaseURL  = "https://open.bigmodel.cn/api/paas/v4"
	openAIBaseURL = "https://api.openai.com/v1"
)

var (
	ErrInvalidSettings       = errors.New("AI settings are invalid")
	ErrNotConfigured         = errors.New("AI is not configured")
	ErrProviderNotConfigured = errors.New("AI provider is not configured")
	ErrSettingsConflict      = errors.New("AI settings changed concurrently")
	ErrCipherUnavailable     = errors.New("AI settings cipher is unavailable")
	ErrDecryptFailed         = errors.New("AI provider credential could not be decrypted")
	ErrTesterUnavailable     = errors.New("AI connection tester is unavailable")
)

type ProviderDefinition struct {
	ID              string
	Name            string
	BaseURL         string
	BaseURLEditable bool
	DefaultModel    string
	ModelOptions    []string
}

var providerCatalog = []ProviderDefinition{
	{ID: ProviderZhipu, Name: "智谱 GLM", BaseURL: zhipuBaseURL, DefaultModel: "glm-5.3-flash", ModelOptions: []string{"glm-5.3-flash"}},
	{ID: ProviderOpenAI, Name: "OpenAI", BaseURL: openAIBaseURL, DefaultModel: "gpt-5-mini", ModelOptions: []string{"gpt-5", "gpt-5-mini"}},
	{ID: ProviderOpenAICompatible, Name: "自定义 OpenAI-compatible", BaseURLEditable: true},
}

type ProviderView struct {
	ID              string   `json:"id"`
	Name            string   `json:"name"`
	BaseURL         string   `json:"baseUrl"`
	BaseURLEditable bool     `json:"baseUrlEditable"`
	Model           string   `json:"model"`
	ModelOptions    []string `json:"modelOptions"`
	KeyConfigured   bool     `json:"keyConfigured"`
}

type SettingsView struct {
	Enabled        bool           `json:"enabled"`
	ActiveProvider string         `json:"activeProvider"`
	Providers      []ProviderView `json:"providers"`
}

type UpdateInput struct {
	Enabled        bool   `json:"enabled"`
	ActiveProvider string `json:"activeProvider"`
	BaseURL        string `json:"baseUrl"`
	Model          string `json:"model"`
	APIKey         string `json:"apiKey"`
}

type TestInput struct {
	ActiveProvider string `json:"activeProvider"`
	BaseURL        string `json:"baseUrl"`
	Model          string `json:"model"`
	APIKey         string `json:"apiKey"`
}

type TestResult struct {
	Success   bool   `json:"success"`
	Provider  string `json:"provider"`
	Model     string `json:"model"`
	LatencyMS int64  `json:"latencyMs"`
}

type RuntimeConfig struct {
	Enabled  bool
	Provider string
	BaseURL  string
	Model    string
	APIKey   string
}

type RuntimeStatus struct {
	Enabled  bool
	Provider string
	Model    string
}

type StoredRuntime struct {
	Enabled        bool
	ActiveProvider string
	UpdatedBy      *uint64
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type StoredProvider struct {
	Provider      string
	BaseURL       string
	Model         string
	APICiphertext []byte
	UpdatedBy     uint64
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

type StoreUpdate struct {
	Enabled               bool
	ActiveProvider        string
	Provider              string
	BaseURL               string
	Model                 string
	APICiphertext         []byte
	PreserveKey           bool
	CompareCredential     bool
	ExpectedBaseURL       string
	ExpectedAPICiphertext []byte
	UpdatedBy             uint64
}
