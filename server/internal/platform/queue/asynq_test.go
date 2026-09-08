package queue

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/hibiken/asynq"

	"github.com/chenbb0128/weavepress/server/internal/config"
)

func TestRedisClientOptFromConfig(t *testing.T) {
	cfg := config.RedisConfig{
		Addr:         "127.0.0.1:6379",
		Username:     "user",
		Password:     "pass",
		DB:           2,
		DialTimeout:  time.Second,
		ReadTimeout:  2 * time.Second,
		WriteTimeout: 3 * time.Second,
		PoolSize:     11,
	}

	opt := RedisClientOptFromConfig(cfg)
	if opt.Network != "tcp" {
		t.Fatalf("Network = %q, want tcp", opt.Network)
	}
	if opt.Addr != cfg.Addr || opt.Username != cfg.Username || opt.Password != cfg.Password || opt.DB != cfg.DB {
		t.Fatalf("RedisClientOptFromConfig() did not copy connection fields")
	}
	if opt.DialTimeout != cfg.DialTimeout || opt.ReadTimeout != cfg.ReadTimeout || opt.WriteTimeout != cfg.WriteTimeout {
		t.Fatalf("RedisClientOptFromConfig() did not copy timeout fields")
	}
	if opt.PoolSize != cfg.PoolSize {
		t.Fatalf("PoolSize = %d, want %d", opt.PoolSize, cfg.PoolSize)
	}
}

func TestCollectionRetryDelay(t *testing.T) {
	for _, test := range []struct {
		retried int
		want    time.Duration
	}{{0, 30 * time.Second}, {1, 2 * time.Minute}, {2, 10 * time.Minute}, {3, 10 * time.Minute}} {
		if got := collectionRetryDelay(test.retried, nil, nil); got != test.want {
			t.Fatalf("collectionRetryDelay(%d) = %s, want %s", test.retried, got, test.want)
		}
	}
}

func TestRedisIntegrationTaskIDIsIdempotent(t *testing.T) {
	address := os.Getenv("WEAVEPRESS_TEST_REDIS_ADDR")
	if address == "" {
		t.Skip("WEAVEPRESS_TEST_REDIS_ADDR is not set")
	}
	cfg := config.RedisConfig{Addr: address, DialTimeout: 3 * time.Second, ReadTimeout: 3 * time.Second, WriteTimeout: 3 * time.Second, PoolSize: 2}
	client := NewClient(cfg)
	defer client.Close()

	taskID := fmt.Sprintf("weavepress-integration-%d", time.Now().UnixNano())
	queueName := "weavepress-integration"
	task := asynq.NewTask("integration:idempotency", []byte(`{"ok":true}`))
	if _, err := client.Asynq.EnqueueContext(context.Background(), task, asynq.Queue(queueName), asynq.TaskID(taskID)); err != nil {
		t.Fatal(err)
	}
	inspector := asynq.NewInspector(RedisClientOptFromConfig(cfg))
	defer inspector.Close()
	defer inspector.DeleteTask(queueName, taskID)

	if _, err := client.Asynq.EnqueueContext(context.Background(), task, asynq.Queue(queueName), asynq.TaskID(taskID)); !errors.Is(err, asynq.ErrTaskIDConflict) {
		t.Fatalf("second enqueue error = %v, want %v", err, asynq.ErrTaskIDConflict)
	}
}
