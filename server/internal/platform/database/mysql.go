package database

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/go-sql-driver/mysql"

	"github.com/chenbb0128/weavepress/server/internal/config"
)

type DB struct {
	SQL *sql.DB
}

func Open(ctx context.Context, cfg config.DatabaseConfig) (*DB, error) {
	if cfg.Driver != "mysql" {
		return nil, fmt.Errorf("unsupported database driver %q", cfg.Driver)
	}

	dsn, err := NormalizeMySQLDSN(cfg.DSN)
	if err != nil {
		return nil, err
	}

	db, err := sql.Open(cfg.Driver, dsn)
	if err != nil {
		return nil, fmt.Errorf("open mysql: %w", err)
	}

	db.SetMaxOpenConns(cfg.MaxOpenConns)
	db.SetMaxIdleConns(cfg.MaxIdleConns)
	db.SetConnMaxLifetime(cfg.ConnMaxLifetime)
	db.SetConnMaxIdleTime(cfg.ConnMaxIdleTime)

	pingCtx, cancel := context.WithTimeout(ctx, cfg.PingTimeout)
	defer cancel()
	if err := db.PingContext(pingCtx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping mysql: %w", err)
	}

	return &DB{SQL: db}, nil
}

func (db *DB) Close() error {
	if db == nil || db.SQL == nil {
		return nil
	}
	return db.SQL.Close()
}

func (db *DB) Ping(ctx context.Context) error {
	if db == nil || db.SQL == nil {
		return fmt.Errorf("mysql is not configured")
	}
	return db.SQL.PingContext(ctx)
}

func NormalizeMySQLDSN(raw string) (string, error) {
	cfg, err := mysql.ParseDSN(raw)
	if err != nil {
		return "", fmt.Errorf("parse mysql dsn: %w", err)
	}
	cfg.ParseTime = true
	cfg.Loc = time.UTC
	if cfg.Params == nil {
		cfg.Params = map[string]string{}
	}
	cfg.Params["charset"] = "utf8mb4"
	cfg.Params["time_zone"] = "'+00:00'"
	return cfg.FormatDSN(), nil
}

func normalizeMySQLDSN(raw string) (string, error) {
	return NormalizeMySQLDSN(raw)
}
