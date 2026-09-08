package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/chenbb0128/weavepress/server/internal/config"
	"github.com/chenbb0128/weavepress/server/internal/modules/authn"
	"github.com/chenbb0128/weavepress/server/internal/modules/workspace"
	"github.com/chenbb0128/weavepress/server/internal/modules/workspace/mysqlstore"
	"github.com/chenbb0128/weavepress/server/internal/platform/database"
)

func main() {
	configPath := flag.String("config", "", "path to a YAML configuration file")
	username := flag.String("username", "", "administrator username")
	password := flag.String("password", "", "administrator password (or use WEAVEPRESS_ADMIN_PASSWORD)")
	nickname := flag.String("nickname", "管理员", "administrator display name")
	flag.Parse()
	if *password == "" {
		*password = os.Getenv("WEAVEPRESS_ADMIN_PASSWORD")
	}
	if len(strings.TrimSpace(*username)) < 3 || len(*password) < 8 {
		fmt.Fprintln(os.Stderr, "username must be at least 3 characters and password at least 8 characters")
		os.Exit(2)
	}
	cfg, err := config.Load(*configPath)
	if err != nil {
		slog.Error("load config failed", "error", err)
		os.Exit(1)
	}
	db, err := database.Open(context.Background(), cfg.Database)
	if err != nil {
		slog.Error("open database failed", "error", err)
		os.Exit(1)
	}
	defer db.Close()
	service := authn.New(mysqlstore.New(db.SQL), nil, cfg.Auth)
	user, err := service.CreateUser(context.Background(), *username, *password, *nickname, workspace.RoleAdmin)
	if err != nil {
		slog.Error("create administrator failed", "error", err)
		os.Exit(1)
	}
	fmt.Printf("created administrator %s (id=%d)\n", user.Username, user.ID)
}
