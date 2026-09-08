package config

import (
	"fmt"
	"math"
	"net"
	"net/netip"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"
)

var metricNamespacePattern = regexp.MustCompile(`^[a-zA-Z_:][a-zA-Z0-9_:]*$`)

func (c Config) Validate() error {
	if strings.TrimSpace(c.App.Name) == "" {
		return fmt.Errorf("config app.name is required")
	}

	env := strings.ToLower(strings.TrimSpace(c.App.Env))
	if !slices.Contains([]string{"local", "dev", "test", "staging", "prod", "production"}, env) {
		return fmt.Errorf("config app.env must be one of local, dev, test, staging, prod, production")
	}

	if _, _, err := net.SplitHostPort(c.HTTP.Addr); err != nil {
		return fmt.Errorf("config http.addr must be host:port or :port: %w", err)
	}

	if err := positiveDuration("http.read_header_timeout", c.HTTP.ReadHeaderTimeout); err != nil {
		return err
	}
	if err := positiveDuration("http.read_timeout", c.HTTP.ReadTimeout); err != nil {
		return err
	}
	if err := positiveDuration("http.write_timeout", c.HTTP.WriteTimeout); err != nil {
		return err
	}
	if err := positiveDuration("http.idle_timeout", c.HTTP.IdleTimeout); err != nil {
		return err
	}
	if err := positiveDuration("http.shutdown_timeout", c.HTTP.ShutdownTimeout); err != nil {
		return err
	}

	if c.HTTP.MaxHeaderBytes <= 0 {
		return fmt.Errorf("config http.max_header_bytes must be positive")
	}
	if c.HTTP.MaxBodyBytes <= 0 {
		return fmt.Errorf("config http.max_body_bytes must be positive")
	}

	for _, proxy := range c.HTTP.TrustedProxies {
		if err := validateProxy(proxy); err != nil {
			return fmt.Errorf("config http.trusted_proxies contains invalid value %q: %w", proxy, err)
		}
	}

	if err := c.HTTP.CORS.Validate(env); err != nil {
		return err
	}
	if err := c.Database.Validate(); err != nil {
		return err
	}
	if err := c.Redis.Validate(); err != nil {
		return err
	}
	if err := c.Worker.Validate(c.Redis); err != nil {
		return err
	}
	if err := c.Auth.Validate(env); err != nil {
		return err
	}
	if err := c.Storage.Validate(env); err != nil {
		return err
	}
	if err := c.Collector.Validate(); err != nil {
		return err
	}
	if err := c.WeChat.Validate(env); err != nil {
		return err
	}
	if err := c.AI.Validate(env); err != nil {
		return err
	}
	if err := c.Observability.Validate(); err != nil {
		return err
	}

	switch strings.ToLower(strings.TrimSpace(c.Log.Level)) {
	case "debug", "info", "warn", "error":
	default:
		return fmt.Errorf("config log.level must be one of debug, info, warn, error")
	}

	return nil
}

func (c AIConfig) Validate(env string) error {
	if err := positiveDuration("ai.request_timeout", c.RequestTimeout); err != nil {
		return err
	}
	if c.MaxInputChars <= 0 {
		return fmt.Errorf("config ai.max_input_chars must be positive")
	}
	if c.MaxOutputTokens <= 0 {
		return fmt.Errorf("config ai.max_output_tokens must be positive")
	}
	if math.IsNaN(c.Temperature) || c.Temperature < 0 || c.Temperature > 2 {
		return fmt.Errorf("config ai.temperature must be between 0 and 2")
	}
	provider := strings.ToLower(strings.TrimSpace(c.Provider))
	if provider != "openai-compatible" && (c.Enabled || provider != "") {
		return fmt.Errorf("config ai.provider only supports openai-compatible")
	}

	baseURL := strings.TrimSpace(c.BaseURL)
	if c.Enabled {
		if baseURL == "" {
			return fmt.Errorf("config ai.base_url is required when ai.enabled is true")
		}
		if strings.TrimSpace(c.APIKey) == "" {
			return fmt.Errorf("config ai.api_key is required when ai.enabled is true")
		}
		if strings.TrimSpace(c.Model) == "" {
			return fmt.Errorf("config ai.model is required when ai.enabled is true")
		}
	}
	if baseURL == "" {
		return nil
	}

	parsed, err := url.Parse(baseURL)
	if err != nil {
		return fmt.Errorf("config ai.base_url must be an absolute HTTP or HTTPS URL")
	}
	scheme := strings.ToLower(parsed.Scheme)
	if parsed.Host == "" || (scheme != "http" && scheme != "https") {
		return fmt.Errorf("config ai.base_url must be an absolute HTTP or HTTPS URL")
	}
	normalizedEnv := strings.ToLower(strings.TrimSpace(env))
	if c.Enabled && (normalizedEnv == "prod" || normalizedEnv == "production") && scheme != "https" {
		return fmt.Errorf("config ai.base_url must use HTTPS in production")
	}
	return nil
}

func (c WeChatConfig) Validate(env string) error {
	if err := positiveDuration("wechat.request_timeout", c.RequestTimeout); err != nil {
		return err
	}
	parsed, err := url.Parse(strings.TrimSpace(c.APIBase))
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return fmt.Errorf("config wechat.api_base must be an absolute HTTP or HTTPS URL")
	}
	if (env == "prod" || env == "production") && parsed.Scheme != "https" {
		return fmt.Errorf("config wechat.api_base must use HTTPS in production")
	}
	if c.Enabled && (strings.TrimSpace(c.AppID) == "" || strings.TrimSpace(c.AppSecret) == "") {
		return fmt.Errorf("config wechat.app_id and wechat.app_secret are required when publishing is enabled")
	}
	return nil
}

func (c AuthConfig) Validate(env string) error {
	if len(c.JWTSecret) < 32 {
		return fmt.Errorf("config auth.jwt_secret must contain at least 32 characters")
	}
	if len(c.MediaSigningKey) < 32 {
		return fmt.Errorf("config auth.media_signing_key must contain at least 32 characters")
	}
	if strings.TrimSpace(c.RefreshCookie) == "" {
		return fmt.Errorf("config auth.refresh_cookie is required")
	}
	if (env == "prod" || env == "production") && !c.CookieSecure {
		return fmt.Errorf("config auth.cookie_secure must be true in production")
	}
	if err := positiveDuration("auth.access_ttl", c.AccessTTL); err != nil {
		return err
	}
	if err := positiveDuration("auth.refresh_ttl", c.RefreshTTL); err != nil {
		return err
	}
	return positiveDuration("auth.media_url_ttl", c.MediaURLTTL)
}

func (c StorageConfig) Validate(env string) error {
	switch strings.ToLower(strings.TrimSpace(c.Driver)) {
	case "local":
		if strings.TrimSpace(c.LocalDir) == "" {
			return fmt.Errorf("config storage.local_dir is required")
		}
		if env == "prod" || env == "production" {
			return fmt.Errorf("config storage.driver must be qiniu in production")
		}
	case "qiniu":
		if strings.TrimSpace(c.Qiniu.AccessKey) == "" || strings.TrimSpace(c.Qiniu.SecretKey) == "" || strings.TrimSpace(c.Qiniu.Bucket) == "" || strings.TrimSpace(c.Qiniu.Domain) == "" {
			return fmt.Errorf("config storage.qiniu access_key, secret_key, bucket and domain are required")
		}
	default:
		return fmt.Errorf("config storage.driver must be one of local, qiniu")
	}
	return nil
}

func (c CollectorConfig) Validate() error {
	if c.PageMaxBytes <= 0 || c.ImageMaxBytes <= 0 || c.ArticleMaxBytes <= 0 {
		return fmt.Errorf("config collector byte limits must be positive")
	}
	if c.ArticleMaxBytes < c.ImageMaxBytes {
		return fmt.Errorf("config collector.article_max_bytes must not be less than image_max_bytes")
	}
	if c.MaxImages <= 0 || c.ImageConcurrency <= 0 || c.MaxRedirects < 0 {
		return fmt.Errorf("config collector count limits are invalid")
	}
	if err := positiveDuration("collector.request_timeout", c.RequestTimeout); err != nil {
		return err
	}
	if err := positiveDuration("collector.image_timeout", c.ImageTimeout); err != nil {
		return err
	}
	if strings.TrimSpace(c.UserAgent) == "" {
		return fmt.Errorf("config collector.user_agent is required")
	}
	return nil
}

func (c CORSConfig) Validate(env string) error {
	if len(c.AllowedOrigins) == 0 {
		return fmt.Errorf("config http.cors.allowed_origins must not be empty")
	}
	if len(c.AllowedMethods) == 0 {
		return fmt.Errorf("config http.cors.allowed_methods must not be empty")
	}
	if len(c.AllowedHeaders) == 0 {
		return fmt.Errorf("config http.cors.allowed_headers must not be empty")
	}

	hasWildcardOrigin := slices.Contains(c.AllowedOrigins, "*")
	if c.AllowCredentials && hasWildcardOrigin {
		return fmt.Errorf("config http.cors cannot use wildcard origin with credentials")
	}
	if (env == "prod" || env == "production") && hasWildcardOrigin {
		return fmt.Errorf("config http.cors.allowed_origins cannot contain wildcard in production")
	}

	for _, method := range c.AllowedMethods {
		switch strings.ToUpper(method) {
		case "GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS", "HEAD":
		default:
			return fmt.Errorf("config http.cors.allowed_methods contains unsupported method %q", method)
		}
	}

	return nil
}

func (c DatabaseConfig) Validate() error {
	if strings.TrimSpace(c.Driver) == "" {
		return fmt.Errorf("config database.driver is required")
	}
	if c.Driver != "mysql" {
		return fmt.Errorf("config database.driver only supports mysql in this template")
	}
	if !c.Enabled {
		return nil
	}
	if strings.TrimSpace(c.DSN) == "" {
		return fmt.Errorf("config database.dsn is required when database.enabled is true")
	}
	if c.MaxOpenConns <= 0 {
		return fmt.Errorf("config database.max_open_conns must be positive")
	}
	if c.MaxIdleConns < 0 {
		return fmt.Errorf("config database.max_idle_conns must not be negative")
	}
	if c.MaxIdleConns > c.MaxOpenConns {
		return fmt.Errorf("config database.max_idle_conns must not exceed database.max_open_conns")
	}
	if err := positiveDuration("database.conn_max_lifetime", c.ConnMaxLifetime); err != nil {
		return err
	}
	if err := positiveDuration("database.conn_max_idle_time", c.ConnMaxIdleTime); err != nil {
		return err
	}
	if err := positiveDuration("database.ping_timeout", c.PingTimeout); err != nil {
		return err
	}
	return nil
}

func (c RedisConfig) Validate() error {
	if !c.Enabled {
		return nil
	}
	if strings.TrimSpace(c.Addr) == "" {
		return fmt.Errorf("config redis.addr is required when redis.enabled is true")
	}
	if _, _, err := net.SplitHostPort(c.Addr); err != nil {
		return fmt.Errorf("config redis.addr must be host:port: %w", err)
	}
	if c.DB < 0 {
		return fmt.Errorf("config redis.db must not be negative")
	}
	if err := positiveDuration("redis.dial_timeout", c.DialTimeout); err != nil {
		return err
	}
	if err := positiveDuration("redis.read_timeout", c.ReadTimeout); err != nil {
		return err
	}
	if err := positiveDuration("redis.write_timeout", c.WriteTimeout); err != nil {
		return err
	}
	if err := positiveDuration("redis.ping_timeout", c.PingTimeout); err != nil {
		return err
	}
	if c.PoolSize <= 0 {
		return fmt.Errorf("config redis.pool_size must be positive")
	}
	if c.MinIdleConns < 0 {
		return fmt.Errorf("config redis.min_idle_conns must not be negative")
	}
	if c.MinIdleConns > c.PoolSize {
		return fmt.Errorf("config redis.min_idle_conns must not exceed redis.pool_size")
	}
	if strings.TrimSpace(c.KeyPrefix) == "" {
		return fmt.Errorf("config redis.key_prefix is required when redis.enabled is true")
	}
	return nil
}

func (c WorkerConfig) Validate(redis RedisConfig) error {
	if !c.Enabled {
		return nil
	}
	if !redis.Enabled {
		return fmt.Errorf("config worker.enabled requires redis.enabled to be true")
	}
	if c.Concurrency <= 0 {
		return fmt.Errorf("config worker.concurrency must be positive")
	}
	if err := positiveDuration("worker.shutdown_timeout", c.ShutdownTimeout); err != nil {
		return err
	}
	if len(c.Queues) == 0 {
		return fmt.Errorf("config worker.queues must not be empty when worker.enabled is true")
	}
	for name, priority := range c.Queues {
		if strings.TrimSpace(name) == "" {
			return fmt.Errorf("config worker.queues contains an empty queue name")
		}
		if priority <= 0 {
			return fmt.Errorf("config worker.queues[%s] priority must be positive", name)
		}
	}
	return nil
}

func (c ObservabilityConfig) Validate() error {
	if err := c.Metrics.Validate(); err != nil {
		return err
	}
	if err := c.Tracing.Validate(); err != nil {
		return err
	}
	return nil
}

func (c MetricsConfig) Validate() error {
	if !c.Enabled {
		return nil
	}
	path := strings.TrimSpace(c.Path)
	if path == "" || !strings.HasPrefix(path, "/") {
		return fmt.Errorf("config observability.metrics.path must start with /")
	}
	if strings.ContainsAny(path, " \t\r\n") {
		return fmt.Errorf("config observability.metrics.path must not contain whitespace")
	}
	if !metricNamespacePattern.MatchString(strings.TrimSpace(c.Namespace)) {
		return fmt.Errorf("config observability.metrics.namespace must be a valid prometheus namespace")
	}
	return nil
}

func (c TracingConfig) Validate() error {
	if !c.Enabled {
		return nil
	}
	switch strings.ToLower(strings.TrimSpace(c.Exporter)) {
	case "stdout", "otlp":
	default:
		return fmt.Errorf("config observability.tracing.exporter must be one of stdout, otlp")
	}
	if strings.EqualFold(strings.TrimSpace(c.Exporter), "otlp") && strings.TrimSpace(c.Endpoint) == "" {
		return fmt.Errorf("config observability.tracing.endpoint is required when exporter is otlp")
	}
	if c.SampleRatio < 0 || c.SampleRatio > 1 {
		return fmt.Errorf("config observability.tracing.sample_ratio must be between 0 and 1")
	}
	return nil
}

func positiveDuration(name string, value time.Duration) error {
	if value <= 0 {
		return fmt.Errorf("config %s must be positive", name)
	}
	return nil
}

func validateProxy(value string) error {
	if _, err := netip.ParsePrefix(value); err == nil {
		return nil
	}
	if _, err := netip.ParseAddr(value); err == nil {
		return nil
	}
	return fmt.Errorf("expected IP address or CIDR prefix")
}
