package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/chenbb0128/weavepress/server/internal/config"
	"github.com/chenbb0128/weavepress/server/internal/modules/authn"
	"github.com/chenbb0128/weavepress/server/internal/modules/content"
	"github.com/chenbb0128/weavepress/server/internal/modules/workspace/mysqlstore"
	"github.com/chenbb0128/weavepress/server/internal/platform/database"
	platformmetrics "github.com/chenbb0128/weavepress/server/internal/platform/metrics"
	"github.com/chenbb0128/weavepress/server/internal/platform/objectstore"
	"github.com/chenbb0128/weavepress/server/internal/platform/queue"
	redisclient "github.com/chenbb0128/weavepress/server/internal/platform/redis"
	"github.com/chenbb0128/weavepress/server/internal/transport/httpapi"
	"github.com/chenbb0128/weavepress/server/internal/transport/weaveapi"
)

type API struct {
	server          *http.Server
	logger          *slog.Logger
	shutdownTimeout time.Duration
	database        *database.DB
	redis           *redisclient.Client
	queue           *queue.Client
}

func NewAPI(cfg config.Config, logger *slog.Logger) (*API, error) {
	var db *database.DB
	var redis *redisclient.Client
	var metrics *platformmetrics.Metrics
	var checks []httpapi.ReadyCheck

	if cfg.Database.Enabled {
		openCtx, cancel := context.WithTimeout(context.Background(), cfg.Database.PingTimeout)
		defer cancel()

		opened, err := database.Open(openCtx, cfg.Database)
		if err != nil {
			return nil, err
		}
		db = opened
		checks = append(checks, httpapi.ReadyCheck{
			Name: "mysql",
			Check: func(ctx context.Context) error {
				pingCtx, cancel := context.WithTimeout(ctx, cfg.Database.PingTimeout)
				defer cancel()
				return db.Ping(pingCtx)
			},
		})
	}

	if cfg.Redis.Enabled {
		openCtx, cancel := context.WithTimeout(context.Background(), cfg.Redis.PingTimeout)
		defer cancel()

		opened, err := redisclient.Open(openCtx, cfg.Redis)
		if err != nil {
			if db != nil {
				_ = db.Close()
			}
			return nil, err
		}
		redis = opened
		checks = append(checks, httpapi.ReadyCheck{
			Name: "redis",
			Check: func(ctx context.Context) error {
				pingCtx, cancel := context.WithTimeout(ctx, cfg.Redis.PingTimeout)
				defer cancel()
				return redis.Ping(pingCtx)
			},
		})
	}

	if cfg.Observability.Metrics.Enabled {
		created, err := platformmetrics.New(cfg.Observability.Metrics)
		if err != nil {
			if db != nil {
				_ = db.Close()
			}
			if redis != nil {
				_ = redis.Close()
			}
			return nil, err
		}
		metrics = created
	}
	if db == nil || redis == nil {
		return nil, fmt.Errorf("weavepress API requires database.enabled and redis.enabled")
	}
	objects, err := objectstore.New(cfg.Storage)
	if err != nil {
		return nil, fmt.Errorf("create object store: %w", err)
	}
	store := mysqlstore.New(db.SQL)
	queueClient := queue.NewClient(cfg.Redis)
	authService := authn.New(store, redis, cfg.Auth)
	contentService := content.New(store, queueClient, objects, cfg)
	businessAPI := weaveapi.New(store, authService, contentService, cfg)

	router, err := httpapi.NewRouter(httpapi.RouterOptions{
		App:             cfg.App,
		HTTP:            cfg.HTTP,
		Logger:          logger,
		ReadyTimeout:    readinessTimeout(cfg),
		ReadinessChecks: checks,
		Metrics:         metrics,
		RegisterRoutes:  businessAPI.Register,
	})
	if err != nil {
		if db != nil {
			_ = db.Close()
		}
		if redis != nil {
			_ = redis.Close()
		}
		return nil, err
	}

	server := &http.Server{
		Addr:              cfg.HTTP.Addr,
		Handler:           router,
		ReadHeaderTimeout: cfg.HTTP.ReadHeaderTimeout,
		ReadTimeout:       cfg.HTTP.ReadTimeout,
		WriteTimeout:      cfg.HTTP.WriteTimeout,
		IdleTimeout:       cfg.HTTP.IdleTimeout,
		MaxHeaderBytes:    cfg.HTTP.MaxHeaderBytes,
	}

	return &API{
		server:          server,
		logger:          logger,
		shutdownTimeout: cfg.HTTP.ShutdownTimeout,
		database:        db,
		redis:           redis,
		queue:           queueClient,
	}, nil
}

func (a *API) Run(ctx context.Context) (err error) {
	defer func() {
		if closeErr := a.close(); closeErr != nil {
			if err != nil {
				err = errors.Join(err, closeErr)
				return
			}
			err = closeErr
		}
	}()

	errCh := make(chan error, 1)
	go func() {
		a.logger.Info("api listening", "addr", a.server.Addr)
		if err := a.server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
			return
		}
		errCh <- nil
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), a.shutdownTimeout)
	defer cancel()

	a.logger.Info("api shutting down", "timeout", a.shutdownTimeout.String())
	if err := a.server.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown api: %w", err)
	}
	return <-errCh
}

func (a *API) close() error {
	var err error
	if a.database != nil {
		if closeErr := a.database.Close(); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("close mysql: %w", closeErr))
		}
	}
	if a.redis != nil {
		if closeErr := a.redis.Close(); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("close redis: %w", closeErr))
		}
	}
	if a.queue != nil {
		if closeErr := a.queue.Close(); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("close queue: %w", closeErr))
		}
	}
	return err
}

func readinessTimeout(cfg config.Config) time.Duration {
	timeout := 2 * time.Second
	if cfg.Database.Enabled && cfg.Database.PingTimeout > timeout {
		timeout = cfg.Database.PingTimeout
	}
	if cfg.Redis.Enabled && cfg.Redis.PingTimeout > timeout {
		timeout = cfg.Redis.PingTimeout
	}
	return timeout
}
