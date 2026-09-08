package config

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/mitchellh/mapstructure"
	"github.com/spf13/viper"
)

func Load(path string) (Config, error) {
	v := viper.New()
	v.SetConfigType("yaml")
	setDefaults(v)

	v.SetEnvPrefix("WEAVEPRESS")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	if err := bindEnv(v,
		"app.name",
		"app.env",
		"http.addr",
		"http.read_header_timeout",
		"http.read_timeout",
		"http.write_timeout",
		"http.idle_timeout",
		"http.shutdown_timeout",
		"http.max_header_bytes",
		"http.max_body_bytes",
		"http.trusted_proxies",
		"http.cors.allowed_origins",
		"http.cors.allowed_methods",
		"http.cors.allowed_headers",
		"http.cors.allow_credentials",
		"database.enabled",
		"database.driver",
		"database.dsn",
		"database.max_open_conns",
		"database.max_idle_conns",
		"database.conn_max_lifetime",
		"database.conn_max_idle_time",
		"database.ping_timeout",
		"redis.enabled",
		"redis.addr",
		"redis.username",
		"redis.password",
		"redis.db",
		"redis.dial_timeout",
		"redis.read_timeout",
		"redis.write_timeout",
		"redis.ping_timeout",
		"redis.pool_size",
		"redis.min_idle_conns",
		"redis.key_prefix",
		"worker.enabled",
		"worker.concurrency",
		"worker.shutdown_timeout",
		"worker.queues",
		"auth.jwt_secret",
		"auth.access_ttl",
		"auth.refresh_ttl",
		"auth.refresh_cookie",
		"auth.cookie_secure",
		"auth.media_signing_key",
		"auth.media_url_ttl",
		"storage.driver",
		"storage.local_dir",
		"storage.qiniu.access_key",
		"storage.qiniu.secret_key",
		"storage.qiniu.bucket",
		"storage.qiniu.domain",
		"collector.page_max_bytes",
		"collector.image_max_bytes",
		"collector.article_max_bytes",
		"collector.max_images",
		"collector.request_timeout",
		"collector.image_timeout",
		"collector.image_concurrency",
		"collector.max_redirects",
		"collector.user_agent",
		"observability.metrics.enabled",
		"observability.metrics.path",
		"observability.metrics.namespace",
		"observability.tracing.enabled",
		"observability.tracing.exporter",
		"observability.tracing.endpoint",
		"observability.tracing.insecure",
		"observability.tracing.sample_ratio",
		"log.level",
	); err != nil {
		return Config{}, err
	}

	if path != "" {
		v.SetConfigFile(path)
		if err := v.ReadInConfig(); err != nil {
			return Config{}, fmt.Errorf("read config file: %w", err)
		}
	} else {
		v.SetConfigName("config")
		v.AddConfigPath("./configs")
		if err := v.ReadInConfig(); err != nil {
			var notFound viper.ConfigFileNotFoundError
			if !errors.As(err, &notFound) {
				return Config{}, fmt.Errorf("read config file: %w", err)
			}
		}
	}

	var cfg Config
	if err := v.Unmarshal(&cfg, viper.DecodeHook(mapstructure.StringToTimeDurationHookFunc())); err != nil {
		return Config{}, fmt.Errorf("decode config: %w", err)
	}

	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}

	return cfg, nil
}

func setDefaults(v *viper.Viper) {
	v.SetDefault("app.name", "weavepress")
	v.SetDefault("app.env", "local")
	v.SetDefault("http.addr", ":8080")
	v.SetDefault("http.read_header_timeout", 5*time.Second)
	v.SetDefault("http.read_timeout", 15*time.Second)
	v.SetDefault("http.write_timeout", 30*time.Second)
	v.SetDefault("http.idle_timeout", 60*time.Second)
	v.SetDefault("http.shutdown_timeout", 10*time.Second)
	v.SetDefault("http.max_header_bytes", 1<<20)
	v.SetDefault("http.max_body_bytes", int64(1<<20))
	v.SetDefault("http.trusted_proxies", []string{})
	v.SetDefault("http.cors.allowed_origins", []string{"http://localhost:3000", "http://localhost:5173"})
	v.SetDefault("http.cors.allowed_methods", []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"})
	v.SetDefault("http.cors.allowed_headers", []string{"Authorization", "Content-Type", "X-Request-ID", "traceparent", "tracestate"})
	v.SetDefault("http.cors.allow_credentials", true)
	v.SetDefault("database.enabled", false)
	v.SetDefault("database.driver", "mysql")
	v.SetDefault("database.dsn", "")
	v.SetDefault("database.max_open_conns", 25)
	v.SetDefault("database.max_idle_conns", 5)
	v.SetDefault("database.conn_max_lifetime", 30*time.Minute)
	v.SetDefault("database.conn_max_idle_time", 5*time.Minute)
	v.SetDefault("database.ping_timeout", 3*time.Second)
	v.SetDefault("redis.enabled", false)
	v.SetDefault("redis.addr", "127.0.0.1:6379")
	v.SetDefault("redis.username", "")
	v.SetDefault("redis.password", "")
	v.SetDefault("redis.db", 0)
	v.SetDefault("redis.dial_timeout", 3*time.Second)
	v.SetDefault("redis.read_timeout", 2*time.Second)
	v.SetDefault("redis.write_timeout", 2*time.Second)
	v.SetDefault("redis.ping_timeout", 2*time.Second)
	v.SetDefault("redis.pool_size", 10)
	v.SetDefault("redis.min_idle_conns", 2)
	v.SetDefault("redis.key_prefix", "weavepress:local")
	v.SetDefault("worker.enabled", false)
	v.SetDefault("worker.concurrency", 4)
	v.SetDefault("worker.shutdown_timeout", 10*time.Second)
	v.SetDefault("worker.queues", map[string]int{"collection": 2, "default": 1})
	v.SetDefault("auth.jwt_secret", "local-development-jwt-secret-change-me")
	v.SetDefault("auth.access_ttl", 15*time.Minute)
	v.SetDefault("auth.refresh_ttl", 30*24*time.Hour)
	v.SetDefault("auth.refresh_cookie", "wp_refresh_token")
	v.SetDefault("auth.cookie_secure", false)
	v.SetDefault("auth.media_signing_key", "local-development-media-signing-key")
	v.SetDefault("auth.media_url_ttl", 10*time.Minute)
	v.SetDefault("storage.driver", "local")
	v.SetDefault("storage.local_dir", "./data")
	v.SetDefault("storage.qiniu.access_key", "")
	v.SetDefault("storage.qiniu.secret_key", "")
	v.SetDefault("storage.qiniu.bucket", "")
	v.SetDefault("storage.qiniu.domain", "")
	v.SetDefault("collector.page_max_bytes", int64(10<<20))
	v.SetDefault("collector.image_max_bytes", int64(15<<20))
	v.SetDefault("collector.article_max_bytes", int64(100<<20))
	v.SetDefault("collector.max_images", 100)
	v.SetDefault("collector.request_timeout", 30*time.Second)
	v.SetDefault("collector.image_timeout", 30*time.Second)
	v.SetDefault("collector.image_concurrency", 4)
	v.SetDefault("collector.max_redirects", 5)
	v.SetDefault("collector.user_agent", "Mozilla/5.0 (compatible; WeavePress/1.0; +https://github.com/chenbb0128/weavepress)")
	v.SetDefault("observability.metrics.enabled", true)
	v.SetDefault("observability.metrics.path", "/metrics")
	v.SetDefault("observability.metrics.namespace", "weavepress")
	v.SetDefault("observability.tracing.enabled", false)
	v.SetDefault("observability.tracing.exporter", "stdout")
	v.SetDefault("observability.tracing.endpoint", "localhost:4317")
	v.SetDefault("observability.tracing.insecure", true)
	v.SetDefault("observability.tracing.sample_ratio", 1.0)
	v.SetDefault("log.level", "info")
}

func bindEnv(v *viper.Viper, keys ...string) error {
	for _, key := range keys {
		if err := v.BindEnv(key); err != nil {
			return fmt.Errorf("bind env %s: %w", key, err)
		}
	}
	return nil
}
