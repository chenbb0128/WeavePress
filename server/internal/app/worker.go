package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/chenbb0128/weavepress/server/internal/config"
	"github.com/chenbb0128/weavepress/server/internal/modules/content"
	"github.com/chenbb0128/weavepress/server/internal/modules/editorial"
	"github.com/chenbb0128/weavepress/server/internal/modules/workspace/mysqlstore"
	"github.com/chenbb0128/weavepress/server/internal/platform/database"
	"github.com/chenbb0128/weavepress/server/internal/platform/objectstore"
	"github.com/chenbb0128/weavepress/server/internal/platform/queue"
	redisclient "github.com/chenbb0128/weavepress/server/internal/platform/redis"
	"github.com/chenbb0128/weavepress/server/internal/platform/wechat"
	"github.com/chenbb0128/weavepress/server/internal/workers"
)

type Worker struct {
	cfg    config.Config
	logger *slog.Logger
}

func NewWorker(cfg config.Config, logger *slog.Logger) *Worker {
	return &Worker{cfg: cfg, logger: logger}
}

func (w *Worker) Run(ctx context.Context) (err error) {
	if !w.cfg.Worker.Enabled {
		w.logger.Info("worker disabled", "app", w.cfg.App.Name)
		<-ctx.Done()
		w.logger.Info("worker stopped")
		return nil
	}

	openCtx, cancel := context.WithTimeout(context.Background(), w.cfg.Redis.PingTimeout)
	defer cancel()

	redis, err := redisclient.Open(openCtx, w.cfg.Redis)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := redis.Close(); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("close redis: %w", closeErr))
		}
	}()

	db, err := database.Open(openCtx, w.cfg.Database)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := db.Close(); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("close mysql: %w", closeErr))
		}
	}()
	objects, err := objectstore.New(w.cfg.Storage)
	if err != nil {
		return err
	}
	queueClient := queue.NewClient(w.cfg.Redis)
	defer queueClient.Close()
	store := mysqlstore.New(db.SQL)
	contentService := content.New(store, queueClient, objects, w.cfg)
	wechatPublisher := wechat.New(w.cfg.WeChat, objects)
	editorialService := editorial.New(store, store, queueClient, wechatPublisher, w.cfg.WeChat.Enabled)

	server := queue.NewServer(w.cfg.Redis, w.cfg.Worker, w.logger)
	mux := workers.NewMux(contentService, editorialService)

	w.logger.Info(
		"worker starting",
		"concurrency", w.cfg.Worker.Concurrency,
		"queues", w.cfg.Worker.Queues,
		"shutdown_timeout", w.cfg.Worker.ShutdownTimeout.String(),
	)
	if err := server.Start(mux); err != nil {
		return fmt.Errorf("start worker: %w", err)
	}

	<-ctx.Done()
	w.logger.Info("worker shutting down")
	server.Shutdown()
	w.logger.Info("worker stopped")
	return nil
}
