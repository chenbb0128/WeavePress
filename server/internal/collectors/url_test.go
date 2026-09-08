package collectors

import (
	"context"
	"net"
	"net/url"
	"strings"
	"testing"
)

func TestCanonicalizeWechatKeepsArticleIdentity(t *testing.T) {
	canonical, sourceType, _, err := Canonicalize("https://mp.weixin.qq.com/s?scene=1&mid=2&__biz=abc&idx=1&sn=token&utm_source=x#part")
	if err != nil {
		t.Fatalf("Canonicalize() error = %v", err)
	}
	if sourceType != "wechat" {
		t.Fatalf("sourceType = %q", sourceType)
	}
	for _, expected := range []string{"__biz=abc", "mid=2", "idx=1", "sn=token"} {
		if !strings.Contains(canonical, expected) {
			t.Fatalf("canonical URL %q misses %q", canonical, expected)
		}
	}
	if strings.Contains(canonical, "scene=") || strings.Contains(canonical, "utm_") || strings.Contains(canonical, "#") {
		t.Fatalf("canonical URL keeps tracking data: %q", canonical)
	}
}

func TestCanonicalizeRejectsUnsupportedSchemeAndPort(t *testing.T) {
	for _, raw := range []string{"file:///etc/passwd", "http://example.com:8080/article"} {
		if _, _, _, err := Canonicalize(raw); err == nil {
			t.Fatalf("Canonicalize(%q) succeeded", raw)
		}
	}
}

func TestCanonicalizeNormalizesSchemeAndTrackingParameters(t *testing.T) {
	canonical, sourceType, _, err := Canonicalize("HTTPS://Example.COM:443/article?z=2&utm_source=test&a=1#fragment")
	if err != nil {
		t.Fatal(err)
	}
	if canonical != "https://example.com/article?a=1&z=2" || sourceType != "web" {
		t.Fatalf("canonical=%q sourceType=%q", canonical, sourceType)
	}
}

func TestValidateTargetRejectsPrivateAddresses(t *testing.T) {
	for _, raw := range []string{"http://127.0.0.1", "http://10.0.0.1", "http://[::1]", "http://169.254.169.254"} {
		parsed, _ := url.Parse(raw)
		if err := ValidateTarget(context.Background(), net.DefaultResolver, parsed); err == nil {
			t.Fatalf("ValidateTarget(%q) succeeded", raw)
		}
	}
}

func TestValidateTargetRejectsUnsafeResourceURL(t *testing.T) {
	for _, raw := range []string{
		"ftp://example.com/image.jpg",
		"https://user:pass@example.com/image.jpg",
		"https://example.com:8080/image.jpg",
	} {
		parsed, _ := url.Parse(raw)
		if err := ValidateTarget(context.Background(), net.DefaultResolver, parsed); err == nil {
			t.Fatalf("ValidateTarget(%q) succeeded", raw)
		}
	}
}
