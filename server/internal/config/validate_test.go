package config

import (
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

func TestSanitizedSummaryRedactsSecrets(t *testing.T) {
	cfg := validConfig()
	cfg.Database.DSN = "user:secret@tcp(127.0.0.1:3306)/weavepress"
	cfg.Redis.Password = "redis-secret"

	summary := cfg.SanitizedSummary()
	if summary["database_dsn"] == cfg.Database.DSN {
		t.Fatal("SanitizedSummary leaked database dsn")
	}
	if summary["redis_password"] == cfg.Redis.Password {
		t.Fatal("SanitizedSummary leaked redis password")
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
