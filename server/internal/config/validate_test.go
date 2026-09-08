package config

import (
	"math"
	"strings"
	"testing"
	"time"
)

func TestDatabaseValidation(t *testing.T) {
	cfg := validConfig()
	cfg.Database.Enabled = true
	cfg.Database.DSN = ""

	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "database.dsn") {
		t.Fatalf("Validate() error = %v, want database.dsn error", err)
	}
}

func TestDatabaseDisabledDoesNotRequireDSN(t *testing.T) {
	cfg := validConfig()
	cfg.Database.Enabled = false
	cfg.Database.DSN = ""

	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestRedisValidation(t *testing.T) {
	cfg := validConfig()
	cfg.Redis.Enabled = true
	cfg.Redis.Addr = "redis-without-port"

	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "redis.addr") {
		t.Fatalf("Validate() error = %v, want redis.addr error", err)
	}
}

func TestWorkerRequiresRedis(t *testing.T) {
	cfg := validConfig()
	cfg.Redis.Enabled = false
	cfg.Worker.Enabled = true

	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "requires redis.enabled") {
		t.Fatalf("Validate() error = %v, want worker requires redis error", err)
	}
}

func TestWorkerQueuesValidation(t *testing.T) {
	cfg := validConfig()
	cfg.Redis.Enabled = true
	cfg.Worker.Enabled = true
	cfg.Worker.Queues = map[string]int{"default": 0}

	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "worker.queues") {
		t.Fatalf("Validate() error = %v, want worker.queues error", err)
	}
}

func TestMetricsValidation(t *testing.T) {
	cfg := validConfig()
	cfg.Observability.Metrics.Enabled = true
	cfg.Observability.Metrics.Path = "metrics"

	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "observability.metrics.path") {
		t.Fatalf("Validate() error = %v, want metrics path error", err)
	}
}

func TestTracingValidation(t *testing.T) {
	cfg := validConfig()
	cfg.Observability.Tracing.Enabled = true
	cfg.Observability.Tracing.Exporter = "unknown"

	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "observability.tracing.exporter") {
		t.Fatalf("Validate() error = %v, want tracing exporter error", err)
	}
}

func TestTracingSampleRatioValidation(t *testing.T) {
	cfg := validConfig()
	cfg.Observability.Tracing.Enabled = true
	cfg.Observability.Tracing.SampleRatio = 1.1

	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "sample_ratio") {
		t.Fatalf("Validate() error = %v, want sample ratio error", err)
	}
}

func TestAIConfigValidate(t *testing.T) {
	valid := AIConfig{
		Enabled:         true,
		Provider:        "openai-compatible",
		BaseURL:         "https://llm.example.com",
		APIKey:          "secret",
		Model:           "test-model",
		RequestTimeout:  time.Second,
		MaxInputChars:   60000,
		MaxOutputTokens: 6000,
		Temperature:     0.4,
	}
	tests := []struct {
		name   string
		mutate func(*AIConfig)
		env    string
		want   string
	}{
		{
			name: "disabled",
			mutate: func(cfg *AIConfig) {
				cfg.Enabled = false
				cfg.BaseURL = ""
				cfg.APIKey = ""
				cfg.Model = ""
			},
			env: "local",
		},
		{name: "disabled with empty provider", mutate: func(cfg *AIConfig) { cfg.Enabled = false; cfg.Provider = "" }, env: "local", want: "provider"},
		{name: "missing base url", mutate: func(cfg *AIConfig) { cfg.BaseURL = "" }, env: "local", want: "base_url"},
		{name: "missing api key", mutate: func(cfg *AIConfig) { cfg.APIKey = "" }, env: "local", want: "api_key"},
		{name: "missing model", mutate: func(cfg *AIConfig) { cfg.Model = "" }, env: "local", want: "model"},
		{name: "production http", mutate: func(cfg *AIConfig) { cfg.BaseURL = "http://llm.example.com" }, env: "production", want: "HTTPS"},
		{name: "enabled", mutate: func(*AIConfig) {}, env: "local"},
		{name: "invalid provider", mutate: func(cfg *AIConfig) { cfg.Provider = "other" }, env: "local", want: "provider"},
		{name: "invalid base url", mutate: func(cfg *AIConfig) { cfg.BaseURL = "llm.example.com" }, env: "local", want: "base_url"},
		{name: "malformed base url", mutate: func(cfg *AIConfig) { cfg.BaseURL = "https://llm.example.com/%zz" }, env: "local", want: "base_url"},
		{name: "base url with userinfo", mutate: func(cfg *AIConfig) { cfg.Enabled = false; cfg.BaseURL = "https://user:pass@llm.example.com/proxy" }, env: "local", want: "base_url"},
		{name: "base url with query", mutate: func(cfg *AIConfig) { cfg.Enabled = false; cfg.BaseURL = "https://llm.example.com/proxy?tenant=one" }, env: "local", want: "base_url"},
		{name: "base url with fragment", mutate: func(cfg *AIConfig) { cfg.Enabled = false; cfg.BaseURL = "https://llm.example.com/proxy#fragment" }, env: "local", want: "base_url"},
		{name: "zero timeout", mutate: func(cfg *AIConfig) { cfg.RequestTimeout = 0 }, env: "local", want: "request_timeout"},
		{name: "zero input limit", mutate: func(cfg *AIConfig) { cfg.MaxInputChars = 0 }, env: "local", want: "max_input_chars"},
		{name: "zero output limit", mutate: func(cfg *AIConfig) { cfg.MaxOutputTokens = 0 }, env: "local", want: "max_output_tokens"},
		{name: "temperature below range", mutate: func(cfg *AIConfig) { cfg.Temperature = -0.1 }, env: "local", want: "temperature"},
		{name: "temperature above range", mutate: func(cfg *AIConfig) { cfg.Temperature = 2.1 }, env: "local", want: "temperature"},
		{name: "temperature NaN", mutate: func(cfg *AIConfig) { cfg.Temperature = math.NaN() }, env: "local", want: "temperature"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := valid
			tt.mutate(&cfg)
			err := cfg.Validate(tt.env)
			if tt.want == "" && err != nil {
				t.Fatalf("Validate() error = %v", err)
			}
			if tt.want != "" && (err == nil || !strings.Contains(err.Error(), tt.want)) {
				t.Fatalf("Validate() error = %v, want error containing %q", err, tt.want)
			}
		})
	}
}

func TestSanitizedSummaryRedactsSecrets(t *testing.T) {
	cfg := validConfig()
	cfg.Database.DSN = "user:secret@tcp(127.0.0.1:3306)/weavepress"
	cfg.Redis.Password = "redis-secret"
	cfg.AI.APIKey = "ai-secret"

	summary := cfg.SanitizedSummary()
	if summary["database_dsn"] == cfg.Database.DSN {
		t.Fatal("SanitizedSummary leaked database dsn")
	}
	if summary["redis_password"] == cfg.Redis.Password {
		t.Fatal("SanitizedSummary leaked redis password")
	}
	if summary["ai_api_key"] != "<redacted>" {
		t.Fatalf("SanitizedSummary ai_api_key = %v, want <redacted>", summary["ai_api_key"])
	}
	for _, key := range []string{"ai_enabled", "ai_provider", "ai_model", "ai_api_key"} {
		if _, ok := summary[key]; !ok {
			t.Fatalf("SanitizedSummary missing %q", key)
		}
	}
	if _, ok := summary["ai_base_url"]; ok {
		t.Fatal("SanitizedSummary exposed ai_base_url")
	}
}

func TestLoadAIDefaults(t *testing.T) {
	cfg, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AI.Enabled || cfg.AI.Provider != "openai-compatible" || cfg.AI.RequestTimeout != 120*time.Second || cfg.AI.MaxInputChars != 60000 || cfg.AI.MaxOutputTokens != 6000 || cfg.AI.Temperature != 0.4 {
		t.Fatalf("AI defaults = %#v", cfg.AI)
	}
	if cfg.SanitizedSummary()["ai_api_key"] != "" {
		t.Fatalf("empty ai_api_key should remain empty, got %v", cfg.SanitizedSummary()["ai_api_key"])
	}
}

func TestLoadAIEnvironment(t *testing.T) {
	t.Setenv("WEAVEPRESS_AI_ENABLED", "true")
	t.Setenv("WEAVEPRESS_AI_PROVIDER", "openai-compatible")
	t.Setenv("WEAVEPRESS_AI_BASE_URL", "https://llm.example.com")
	t.Setenv("WEAVEPRESS_AI_API_KEY", "secret")
	t.Setenv("WEAVEPRESS_AI_MODEL", "test-model")
	t.Setenv("WEAVEPRESS_AI_REQUEST_TIMEOUT", "45s")
	t.Setenv("WEAVEPRESS_AI_MAX_INPUT_CHARS", "12345")
	t.Setenv("WEAVEPRESS_AI_MAX_OUTPUT_TOKENS", "2345")
	t.Setenv("WEAVEPRESS_AI_TEMPERATURE", "0.7")

	cfg, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.AI.Enabled || cfg.AI.BaseURL != "https://llm.example.com" || cfg.AI.APIKey != "secret" || cfg.AI.Model != "test-model" || cfg.AI.RequestTimeout != 45*time.Second || cfg.AI.MaxInputChars != 12345 || cfg.AI.MaxOutputTokens != 2345 || cfg.AI.Temperature != 0.7 {
		t.Fatalf("AI environment config = %#v", cfg.AI)
	}
}

func TestProductionRequiresSecureRefreshCookie(t *testing.T) {
	cfg := validConfig()
	cfg.App.Env = "production"
	cfg.Auth.CookieSecure = false

	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "auth.cookie_secure") {
		t.Fatalf("Validate() error = %v, want secure cookie error", err)
	}
}

func TestWeChatEnabledRequiresCredentials(t *testing.T) {
	cfg := validConfig()
	cfg.WeChat.Enabled = true
	cfg.WeChat.AppID = ""
	cfg.WeChat.AppSecret = ""

	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "wechat.app_id") {
		t.Fatalf("Validate() error = %v, want WeChat credentials error", err)
	}
}

func TestProductionWeChatAPIRequiresHTTPS(t *testing.T) {
	cfg := validConfig()
	cfg.App.Env = "production"
	cfg.Auth.CookieSecure = true
	cfg.Storage = StorageConfig{Driver: "qiniu", Qiniu: QiniuStorageConfig{AccessKey: "ak", SecretKey: "sk", Bucket: "bucket", Domain: "https://media.example.com"}}
	cfg.WeChat.APIBase = "http://api.weixin.qq.com"

	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "wechat.api_base") {
		t.Fatalf("Validate() error = %v, want HTTPS error", err)
	}
}

func validConfig() Config {
	return Config{
		App: AppConfig{Name: "weavepress", Env: "local"},
		HTTP: HTTPConfig{
			Addr:              ":8080",
			ReadHeaderTimeout: time.Second,
			ReadTimeout:       time.Second,
			WriteTimeout:      time.Second,
			IdleTimeout:       time.Second,
			ShutdownTimeout:   time.Second,
			MaxHeaderBytes:    1,
			MaxBodyBytes:      1,
			CORS: CORSConfig{
				AllowedOrigins: []string{"http://localhost:3000"},
				AllowedMethods: []string{"GET"},
				AllowedHeaders: []string{"Content-Type"},
			},
		},
		Database: DatabaseConfig{
			Enabled:         true,
			Driver:          "mysql",
			DSN:             "user:pass@tcp(127.0.0.1:3306)/weavepress",
			MaxOpenConns:    10,
			MaxIdleConns:    5,
			ConnMaxLifetime: time.Minute,
			ConnMaxIdleTime: time.Minute,
			PingTimeout:     time.Second,
		},
		Redis: RedisConfig{
			Enabled:      false,
			Addr:         "127.0.0.1:6379",
			DB:           0,
			DialTimeout:  time.Second,
			ReadTimeout:  time.Second,
			WriteTimeout: time.Second,
			PingTimeout:  time.Second,
			PoolSize:     10,
			MinIdleConns: 2,
			KeyPrefix:    "weavepress:test",
		},
		Worker: WorkerConfig{
			Enabled:         false,
			Concurrency:     10,
			ShutdownTimeout: time.Second,
			Queues:          map[string]int{"default": 1},
		},
		Auth: AuthConfig{
			JWTSecret:       "test-jwt-secret-that-is-at-least-32-characters",
			AccessTTL:       15 * time.Minute,
			RefreshTTL:      30 * 24 * time.Hour,
			RefreshCookie:   "wp_refresh_token",
			MediaSigningKey: "test-media-secret-that-is-at-least-32-chars",
			MediaURLTTL:     10 * time.Minute,
		},
		Storage: StorageConfig{Driver: "local", LocalDir: "./testdata"},
		Collector: CollectorConfig{
			PageMaxBytes: 10 << 20, ImageMaxBytes: 15 << 20, ArticleMaxBytes: 100 << 20,
			MaxImages: 100, RequestTimeout: 30 * time.Second, ImageTimeout: 30 * time.Second,
			ImageConcurrency: 4, MaxRedirects: 5, UserAgent: "WeavePress-Test",
		},
		WeChat: WeChatConfig{APIBase: "https://api.weixin.qq.com", RequestTimeout: 30 * time.Second},
		AI: AIConfig{
			Enabled:         false,
			Provider:        "openai-compatible",
			RequestTimeout:  120 * time.Second,
			MaxInputChars:   60000,
			MaxOutputTokens: 6000,
			Temperature:     0.4,
		},
		Observability: ObservabilityConfig{
			Metrics: MetricsConfig{
				Enabled:   true,
				Path:      "/metrics",
				Namespace: "weavepress",
			},
			Tracing: TracingConfig{
				Enabled:     false,
				Exporter:    "stdout",
				Endpoint:    "localhost:4317",
				Insecure:    true,
				SampleRatio: 1,
			},
		},
		Log: LogConfig{Level: "info"},
	}
}
