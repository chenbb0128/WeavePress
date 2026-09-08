package objectstore

import (
	"context"
	"io"
	"testing"

	"github.com/chenbb0128/weavepress/server/internal/config"
)

func TestLocalStoreRoundTripAndPathGuard(t *testing.T) {
	store, err := New(config.StorageConfig{Driver: "local", LocalDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Put(context.Background(), "articles/one.txt", []byte("weavepress"), "text/plain"); err != nil {
		t.Fatal(err)
	}
	reader, err := store.Open(context.Background(), "articles/one.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	data, _ := io.ReadAll(reader)
	if string(data) != "weavepress" {
		t.Fatalf("data = %q", data)
	}
	if err := store.Put(context.Background(), "../../escape", []byte("x"), "text/plain"); err == nil {
		t.Fatal("path traversal was accepted")
	}
}
