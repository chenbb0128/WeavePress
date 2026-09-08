package queue

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/hibiken/asynq"

	"github.com/chenbb0128/weavepress/server/internal/config"
)

type Client struct {
	Asynq *asynq.Client
}

func RedisClientOptFromConfig(cfg config.RedisConfig) asynq.RedisClientOpt {
	return asynq.RedisClientOpt{
		Network:      "tcp",
		Addr:         cfg.Addr,
		Username:     cfg.Username,
		Password:     cfg.Password,
		DB:           cfg.DB,
		DialTimeout:  cfg.DialTimeout,
		ReadTimeout:  cfg.ReadTimeout,
		WriteTimeout: cfg.WriteTimeout,
		PoolSize:     cfg.PoolSize,
	}
}

func NewClient(cfg config.RedisConfig) *Client {
	return &Client{Asynq: asynq.NewClient(RedisClientOptFromConfig(cfg))}
}

func (c *Client) Close() error {
	if c == nil || c.Asynq == nil {
		return nil
	}
	return c.Asynq.Close()
}

func NewServer(redisCfg config.RedisConfig, workerCfg config.WorkerConfig, logger *slog.Logger) *asynq.Server {
	if logger == nil {
		logger = slog.Default()
	}

	queues := map[string]int{"default": 1}
	if len(workerCfg.Queues) > 0 {
		queues = make(map[string]int, len(workerCfg.Queues))
		for name, priority := range workerCfg.Queues {
			queues[name] = priority
		}
	}

	return asynq.NewServer(RedisClientOptFromConfig(redisCfg), asynq.Config{
		Concurrency:     workerCfg.Concurrency,
		Queues:          queues,
		ShutdownTimeout: workerCfg.ShutdownTimeout,
		Logger:          slogAdapter{logger: logger.With("component", "asynq")},
		RetryDelayFunc:  collectionRetryDelay,
		ErrorHandler: asynq.ErrorHandlerFunc(func(ctx context.Context, task *asynq.Task, err error) {
			attrs := []any{"error", err}
			if task != nil {
				attrs = append(attrs, "task_type", task.Type())
			}
			logger.ErrorContext(ctx, "asynq task failed", attrs...)
		}),
	})
}

func collectionRetryDelay(n int, _ error, _ *asynq.Task) time.Duration {
	switch n {
	case 0:
		return 30 * time.Second
	case 1:
		return 2 * time.Minute
	default:
		return 10 * time.Minute
	}
}

type slogAdapter struct {
	logger *slog.Logger
}

func (l slogAdapter) Debug(args ...interface{}) { l.log(slog.LevelDebug, args...) }
func (l slogAdapter) Info(args ...interface{})  { l.log(slog.LevelInfo, args...) }
func (l slogAdapter) Warn(args ...interface{})  { l.log(slog.LevelWarn, args...) }
func (l slogAdapter) Error(args ...interface{}) { l.log(slog.LevelError, args...) }
func (l slogAdapter) Fatal(args ...interface{}) { l.log(slog.LevelError, args...) }

func (l slogAdapter) log(level slog.Level, args ...interface{}) {
	logger := l.logger
	if logger == nil {
		logger = slog.Default()
	}
	logger.Log(context.Background(), level, fmt.Sprint(args...))
}
