package database

import (
	"strings"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
)

func TestNormalizeMySQLDSN(t *testing.T) {
	dsn, err := normalizeMySQLDSN("user:pass@tcp(127.0.0.1:3306)/weavepress")
	if err != nil {
		t.Fatalf("normalizeMySQLDSN() error = %v", err)
	}
	if !strings.Contains(dsn, "charset=utf8mb4") {
		t.Fatalf("dsn = %q, want charset=utf8mb4", dsn)
	}

	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		t.Fatalf("ParseDSN() error = %v", err)
	}
	if !cfg.ParseTime {
		t.Fatal("ParseTime = false, want true")
	}
	if cfg.Loc != time.UTC {
		t.Fatalf("Loc = %v, want UTC", cfg.Loc)
	}
	if cfg.Params["time_zone"] != "'+00:00'" {
		t.Fatalf("time_zone = %q, want '+00:00'", cfg.Params["time_zone"])
	}
}
