package redis

import (
	"context"
	"fmt"

	goredis "github.com/redis/go-redis/v9"

	"github.com/chenbb0128/weavepress/server/internal/config"
)

type Client struct {
	Redis *goredis.Client
	keys  KeyBuilder
}

func Open(ctx context.Context, cfg config.RedisConfig) (*Client, error) {
	if !cfg.Enabled {
		return nil, fmt.Errorf("redis is disabled")
	}

	client := goredis.NewClient(&goredis.Options{
		Addr:         cfg.Addr,
		Username:     cfg.Username,
		Password:     cfg.Password,
		DB:           cfg.DB,
		DialTimeout:  cfg.DialTimeout,
		ReadTimeout:  cfg.ReadTimeout,
		WriteTimeout: cfg.WriteTimeout,
		PoolSize:     cfg.PoolSize,
		MinIdleConns: cfg.MinIdleConns,
	})

	pingCtx, cancel := context.WithTimeout(ctx, cfg.PingTimeout)
	defer cancel()
	if err := client.Ping(pingCtx).Err(); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("ping redis: %w", err)
	}

	return &Client{
		Redis: client,
		keys:  NewKeyBuilder(cfg.KeyPrefix),
	}, nil
}

func (c *Client) Close() error {
	if c == nil || c.Redis == nil {
		return nil
	}
	return c.Redis.Close()
}

func (c *Client) Ping(ctx context.Context) error {
	if c == nil || c.Redis == nil {
		return fmt.Errorf("redis is not configured")
	}
	return c.Redis.Ping(ctx).Err()
}

func (c *Client) Keys() KeyBuilder {
	if c == nil {
		return KeyBuilder{}
	}
	return c.keys
}
