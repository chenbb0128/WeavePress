package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/pressly/goose/v3"

	"github.com/chenbb0128/weavepress/server/internal/config"
	"github.com/chenbb0128/weavepress/server/internal/platform/database"
)

func main() {
	configPath := flag.String("config", "", "path to a YAML configuration file")
	migrationsDir := flag.String("dir", "database/migrations", "path to migrations directory")
	command := flag.String("command", "status", "migration command: up, down, status, redo")
	dsnOverride := flag.String("dsn", "", "optional database DSN override")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		slog.Error("load config failed", "error", err)
		os.Exit(1)
	}

	dsn := strings.TrimSpace(*dsnOverride)
	if dsn == "" {
		dsn = strings.TrimSpace(cfg.Database.DSN)
	}
	if dsn == "" {
		slog.Error("database dsn is required for migrations")
		os.Exit(1)
	}
	if cfg.Database.Driver != "mysql" {
		slog.Error("unsupported migration database driver", "driver", cfg.Database.Driver)
		os.Exit(1)
	}

	dsn, err = database.NormalizeMySQLDSN(dsn)
	if err != nil {
		slog.Error("normalize mysql dsn failed", "error", err)
		os.Exit(1)
	}

	db, err := sql.Open(cfg.Database.Driver, dsn)
	if err != nil {
		slog.Error("open database failed", "error", err)
		os.Exit(1)
	}
	defer func() {
		if err := db.Close(); err != nil {
			slog.Error("close database failed", "error", err)
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), cfg.Database.PingTimeout)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		slog.Error("ping database failed", "error", err)
		os.Exit(1)
	}

	if err := goose.SetDialect(cfg.Database.Driver); err != nil {
		slog.Error("set goose dialect failed", "error", err)
		os.Exit(1)
	}

	if err := runMigrationCommand(db, *migrationsDir, *command); err != nil {
		slog.Error("migration failed", "command", *command, "error", err)
		os.Exit(1)
	}
}

func runMigrationCommand(db *sql.DB, dir string, command string) error {
	switch strings.ToLower(strings.TrimSpace(command)) {
	case "up":
		return goose.Up(db, dir)
	case "down":
		return goose.Down(db, dir)
	case "status":
		return goose.Status(db, dir)
	case "redo":
		return goose.Redo(db, dir)
	default:
		return fmt.Errorf("unsupported migration command %q", command)
	}
}
