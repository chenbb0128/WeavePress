package collectors

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/chenbb0128/weavepress/server/internal/config"
)

type FetchResult struct {
	Body        []byte
	FinalURL    *url.URL
	ContentType string
}

type Fetcher interface {
	Validate(context.Context, *url.URL) error
	FetchHTML(context.Context, string) (FetchResult, error)
	FetchImage(context.Context, string, string) (FetchResult, error)
}

type HTTPFetcher struct {
	cfg             config.CollectorConfig
	resolver        *net.Resolver
	client          *http.Client
	hostMu          sync.Mutex
	hostNextRequest map[string]time.Time
}

func NewFetcher(cfg config.CollectorConfig) *HTTPFetcher {
	resolver := net.DefaultResolver
	fetcher := &HTTPFetcher{cfg: cfg, resolver: resolver, hostNextRequest: make(map[string]time.Time)}
	dialer := &net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		addresses, err := resolver.LookupNetIP(ctx, "ip", host)
		if err != nil {
			return nil, err
		}
		for _, item := range addresses {
			address := item
			if address.Is4In6() {
				address = address.Unmap()
			}
			if blockedAddress(address) {
				continue
			}
			return dialer.DialContext(ctx, network, net.JoinHostPort(address.String(), port))
		}
		return nil, fmt.Errorf("target has no public IP address")
	}
	fetcher.client = &http.Client{Transport: transport, Timeout: cfg.RequestTimeout, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) > cfg.MaxRedirects {
			return collectError("TOO_MANY_REDIRECTS", "页面重定向次数过多", false, nil)
		}
		if err := ValidateTarget(req.Context(), resolver, req.URL); err != nil {
			return err
		}
		return fetcher.waitForHost(req.Context(), req.URL.Hostname())
	}}
	return fetcher
}

func (f *HTTPFetcher) Validate(ctx context.Context, parsed *url.URL) error {
	return ValidateTarget(ctx, f.resolver, parsed)
}

func (f *HTTPFetcher) FetchHTML(ctx context.Context, rawURL string) (FetchResult, error) {
	return f.fetch(ctx, rawURL, "", f.cfg.PageMaxBytes, true)
}

func (f *HTTPFetcher) FetchImage(ctx context.Context, rawURL, referer string) (FetchResult, error) {
	return f.fetch(ctx, rawURL, referer, f.cfg.ImageMaxBytes, false)
}

func (f *HTTPFetcher) fetch(ctx context.Context, rawURL, referer string, limit int64, html bool) (FetchResult, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return FetchResult{}, collectError("INVALID_URL", "资源链接格式不正确", false, err)
	}
	if err := ValidateTarget(ctx, f.resolver, parsed); err != nil {
		return FetchResult{}, err
	}
	if err := f.waitForHost(ctx, parsed.Hostname()); err != nil {
		return FetchResult{}, collectError("NETWORK_ERROR", "等待目标站点请求限速时取消", true, err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return FetchResult{}, err
	}
	request.Header.Set("User-Agent", f.cfg.UserAgent)
	request.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.5")
	if html {
		request.Header.Set("Accept", "text/html,application/xhtml+xml")
	} else {
		request.Header.Set("Accept", "image/avif,image/webp,image/png,image/jpeg,image/gif;q=0.9,*/*;q=0.1")
	}
	if referer != "" {
		request.Header.Set("Referer", referer)
	}
	response, err := f.client.Do(request)
	if err != nil {
		if typed, ok := err.(*Error); ok {
			return FetchResult{}, typed
		}
		return FetchResult{}, collectError("NETWORK_ERROR", "网络请求失败", true, err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		retryable := response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= 500
		code := fmt.Sprintf("HTTP_%d", response.StatusCode)
		return FetchResult{}, collectError(code, fmt.Sprintf("目标站点返回 HTTP %d", response.StatusCode), retryable, nil)
	}
	if response.ContentLength > limit {
		return FetchResult{}, collectError("RESOURCE_TOO_LARGE", "资源大小超过限制", false, nil)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return FetchResult{}, collectError("NETWORK_ERROR", "读取资源失败", true, err)
	}
	if int64(len(body)) > limit {
		return FetchResult{}, collectError("RESOURCE_TOO_LARGE", "资源大小超过限制", false, nil)
	}
	contentType := strings.ToLower(strings.TrimSpace(strings.Split(response.Header.Get("Content-Type"), ";")[0]))
	if contentType == "" || contentType == "application/octet-stream" {
		contentType = strings.ToLower(strings.Split(http.DetectContentType(body), ";")[0])
	}
	if html && contentType != "text/html" && contentType != "application/xhtml+xml" {
		return FetchResult{}, collectError("UNSUPPORTED_CONTENT_TYPE", "链接不是 HTML 页面", false, nil)
	}
	if !html && !allowedImageType(contentType) {
		return FetchResult{}, collectError("UNSUPPORTED_IMAGE_TYPE", "图片格式不受支持", false, nil)
	}
	return FetchResult{Body: body, FinalURL: response.Request.URL, ContentType: contentType}, nil
}

const hostRequestInterval = 100 * time.Millisecond

func (f *HTTPFetcher) waitForHost(ctx context.Context, host string) error {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	now := time.Now()
	f.hostMu.Lock()
	next := f.hostNextRequest[host]
	if next.Before(now) {
		next = now
	}
	f.hostNextRequest[host] = next.Add(hostRequestInterval)
	f.hostMu.Unlock()

	delay := time.Until(next)
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func allowedImageType(value string) bool {
	switch value {
	case "image/jpeg", "image/png", "image/gif", "image/webp":
		return true
	default:
		return false
	}
}

func publicIP(value string) bool {
	address, err := netip.ParseAddr(value)
	return err == nil && !blockedAddress(address)
}
