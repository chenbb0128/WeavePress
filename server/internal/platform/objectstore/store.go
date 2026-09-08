package objectstore

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/chenbb0128/weavepress/server/internal/config"
)

var ErrLocalOnly = errors.New("object is stored remotely")

type Store interface {
	Put(context.Context, string, []byte, string) error
	Open(context.Context, string) (io.ReadCloser, error)
	PrivateURL(string, time.Duration) (string, error)
}

func New(cfg config.StorageConfig) (Store, error) {
	switch strings.ToLower(cfg.Driver) {
	case "local":
		absolute, err := filepath.Abs(cfg.LocalDir)
		if err != nil {
			return nil, err
		}
		return &Local{base: absolute}, nil
	case "qiniu":
		return NewQiniu(cfg.Qiniu), nil
	default:
		return nil, fmt.Errorf("unsupported storage driver %q", cfg.Driver)
	}
}

type Local struct{ base string }

func (s *Local) Put(_ context.Context, key string, data []byte, _ string) error {
	target, err := s.target(key)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	return os.WriteFile(target, data, 0o640)
}

func (s *Local) Open(_ context.Context, key string) (io.ReadCloser, error) {
	target, err := s.target(key)
	if err != nil {
		return nil, err
	}
	return os.Open(target)
}

func (s *Local) PrivateURL(string, time.Duration) (string, error) { return "", nil }

func (s *Local) target(key string) (string, error) {
	normalized := strings.ReplaceAll(key, "\\", "/")
	clean := path.Clean(normalized)
	if strings.HasPrefix(normalized, "/") || clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("invalid object key")
	}
	target := filepath.Join(s.base, filepath.FromSlash(clean))
	relative, err := filepath.Rel(s.base, target)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("object key escapes storage root")
	}
	return target, nil
}

type Qiniu struct {
	accessKey string
	secretKey string
	bucket    string
	domain    string
	uploadURL string
	client    *http.Client
}

func NewQiniu(cfg config.QiniuStorageConfig) *Qiniu {
	return &Qiniu{
		accessKey: cfg.AccessKey, secretKey: cfg.SecretKey, bucket: cfg.Bucket,
		domain: strings.TrimRight(cfg.Domain, "/"), uploadURL: "https://upload.qiniup.com",
		client: &http.Client{Timeout: 60 * time.Second},
	}
}

func (s *Qiniu) Put(ctx context.Context, key string, data []byte, mediaType string) error {
	policy, err := json.Marshal(map[string]any{"scope": s.bucket + ":" + key, "deadline": time.Now().Add(time.Hour).Unix()})
	if err != nil {
		return err
	}
	encodedPolicy := base64.RawURLEncoding.EncodeToString(policy)
	mac := hmac.New(sha1.New, []byte(s.secretKey))
	_, _ = mac.Write([]byte(encodedPolicy))
	token := s.accessKey + ":" + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)) + ":" + encodedPolicy
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("token", token); err != nil {
		return err
	}
	if err := writer.WriteField("key", key); err != nil {
		return err
	}
	part, err := writer.CreateFormFile("file", path.Base(key))
	if err != nil {
		return err
	}
	if _, err := part.Write(data); err != nil {
		return err
	}
	if err := writer.Close(); err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, s.uploadURL, &body)
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", writer.FormDataContentType())
	if mediaType != "" {
		request.Header.Set("X-Upload-Content-Type", mediaType)
	}
	response, err := s.client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		message, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return fmt.Errorf("qiniu upload failed: status=%d body=%s", response.StatusCode, strings.TrimSpace(string(message)))
	}
	return nil
}

func (s *Qiniu) Open(context.Context, string) (io.ReadCloser, error) { return nil, ErrLocalOnly }

func (s *Qiniu) PrivateURL(key string, ttl time.Duration) (string, error) {
	base, err := url.JoinPath(s.domain, key)
	if err != nil {
		return "", err
	}
	separator := "?"
	if strings.Contains(base, "?") {
		separator = "&"
	}
	signedURL := fmt.Sprintf("%s%se=%d", base, separator, time.Now().Add(ttl).Unix())
	mac := hmac.New(sha1.New, []byte(s.secretKey))
	_, _ = mac.Write([]byte(signedURL))
	token := s.accessKey + ":" + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return signedURL + "&token=" + token, nil
}
