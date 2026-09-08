package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/chenbb0128/weavepress/server/internal/app"
	"github.com/chenbb0128/weavepress/server/internal/config"
	"github.com/chenbb0128/weavepress/server/internal/platform/logging"
	"github.com/chenbb0128/weavepress/server/internal/platform/telemetry"
)

func main() {
	configPath := flag.String("config", "", "path to a YAML configuration file")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg, err := config.Load(*configPath)
	if err != nil {
		slog.Error("load config failed", "error", err)
		os.Exit(1)
	}

	logger, err := logging.New(cfg.App, cfg.Log)
	if err != nil {
		slog.Error("create logger failed", "error", err)
		os.Exit(1)
	}

	shutdownTracing, err := telemetry.ConfigureTracing(ctx, cfg.App, cfg.Observability.Tracing, logger)
	if err != nil {
		logger.Error("configure tracing failed", "error", err)
		os.Exit(1)
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := shutdownTracing(shutdownCtx); err != nil {
			logger.Error("shutdown tracing failed", "error", err)
		}
	}()

	worker := app.NewWorker(cfg, logger)
	if err := worker.Run(ctx); err != nil {
		logger.Error("worker stopped with error", "error", err)
		os.Exit(1)
	}
}
