package httpapi

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/chenbb0128/weavepress/server/internal/config"
	platformmetrics "github.com/chenbb0128/weavepress/server/internal/platform/metrics"
	"github.com/chenbb0128/weavepress/server/internal/transport/httpapi/middleware"
	"github.com/chenbb0128/weavepress/server/internal/transport/httpapi/response"
)

type ReadyCheck struct {
	Name  string
	Check func(context.Context) error
}

type RouterOptions struct {
	App             config.AppConfig
	HTTP            config.HTTPConfig
	Logger          *slog.Logger
	ReadyTimeout    time.Duration
	ReadinessChecks []ReadyCheck
	Metrics         *platformmetrics.Metrics
	RegisterRoutes  func(*gin.Engine)
}

func NewRouter(opts RouterOptions) (*gin.Engine, error) {
	if opts.Logger == nil {
		return nil, fmt.Errorf("http router logger is required")
	}
	if opts.ReadyTimeout <= 0 {
		opts.ReadyTimeout = 2 * time.Second
	}

	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	router.HandleMethodNotAllowed = true

	if err := router.SetTrustedProxies(opts.HTTP.TrustedProxies); err != nil {
		return nil, fmt.Errorf("set trusted proxies: %w", err)
	}

	router.Use(
		middleware.RequestID(opts.Logger),
		middleware.Tracing(opts.App.Name),
		middleware.Recovery(opts.Logger),
		middleware.SecurityHeaders(),
		middleware.CORS(opts.HTTP.CORS),
		middleware.BodyLimit(opts.HTTP.MaxBodyBytes),
		middleware.Metrics(opts.Metrics),
		middleware.AccessLog(opts.Logger),
	)

	router.NoRoute(func(c *gin.Context) {
		response.Error(c, response.NotFound())
	})
	router.NoMethod(func(c *gin.Context) {
		response.Error(c, response.MethodNotAllowed())
	})

	if opts.Metrics != nil {
		router.GET(opts.Metrics.Path(), gin.WrapH(opts.Metrics.Handler()))
	}
	router.GET("/health", func(c *gin.Context) {
		response.OK(c, gin.H{"status": "ok"})
	})
	router.GET("/ready", readyHandler(opts.Logger, opts.ReadyTimeout, opts.ReadinessChecks))
	if opts.RegisterRoutes != nil {
		opts.RegisterRoutes(router)
	}

	return router, nil
}

func readyHandler(logger *slog.Logger, timeout time.Duration, checks []ReadyCheck) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), timeout)
		defer cancel()

		for _, check := range checks {
			if check.Check == nil {
				continue
			}
			if err := check.Check(ctx); err != nil {
				logger.Warn("readiness check failed", "check", check.Name, "error", err)
				response.Error(c, response.DependencyUnavailable(err))
				return
			}
		}

		response.OK(c, gin.H{"status": "ready"})
	}
}
