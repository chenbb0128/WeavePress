package logging

import (
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/chenbb0128/weavepress/server/internal/config"
)

func New(app config.AppConfig, cfg config.LogConfig) (*slog.Logger, error) {
	var level slog.Level
	switch strings.ToLower(strings.TrimSpace(cfg.Level)) {
	case "debug":
		level = slog.LevelDebug
	case "info":
		level = slog.LevelInfo
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		return nil, fmt.Errorf("unsupported log level %q", cfg.Level)
	}

	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level})
	return slog.New(handler).With("app", app.Name, "env", app.Env), nil
}
