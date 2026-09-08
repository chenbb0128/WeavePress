package objectstore

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestQiniuOpenUsesPrivateDownloadURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/articles/test.jpg" {
			t.Fatalf("path = %q", request.URL.Path)
		}
		if request.URL.Query().Get("e") == "" || request.URL.Query().Get("token") == "" {
			t.Fatal("private URL signature is missing")
		}
		_, _ = io.WriteString(writer, "image-data")
	}))
	defer server.Close()

	store := NewQiniu(config.QiniuStorageConfig{AccessKey: "ak", SecretKey: "sk", Bucket: "bucket", Domain: server.URL})
	reader, err := store.Open(context.Background(), "articles/test.jpg")
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	body, _ := io.ReadAll(reader)
	if string(body) != "image-data" {
		t.Fatalf("body = %q", body)
	}
}

func TestQiniuOpenRejectsFailedDownload(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		http.Error(writer, "missing", http.StatusNotFound)
	}))
	defer server.Close()
	store := NewQiniu(config.QiniuStorageConfig{AccessKey: "ak", SecretKey: "sk", Bucket: "bucket", Domain: server.URL})
	_, err := store.Open(context.Background(), "missing.jpg")
	if err == nil || !strings.Contains(err.Error(), "status=404") {
		t.Fatalf("Open() error = %v", err)
	}
}
