package collectors

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"sort"
	"strings"
)

var blockedPrefixes = mustPrefixes(
	"0.0.0.0/8", "10.0.0.0/8", "100.64.0.0/10", "127.0.0.0/8", "169.254.0.0/16",
	"172.16.0.0/12", "192.0.0.0/24", "192.0.2.0/24", "192.168.0.0/16", "198.18.0.0/15",
	"198.51.100.0/24", "203.0.113.0/24", "224.0.0.0/4", "240.0.0.0/4",
	"::/128", "::1/128", "fc00::/7", "fe80::/10", "ff00::/8", "2001:db8::/32",
)

func Canonicalize(raw string) (string, string, [32]byte, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", "", [32]byte{}, collectError("INVALID_URL", "链接格式不正确", false, err)
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", "", [32]byte{}, collectError("UNSUPPORTED_SCHEME", "只支持 HTTP 或 HTTPS 链接", false, nil)
	}
	if parsed.User != nil || parsed.Hostname() == "" {
		return "", "", [32]byte{}, collectError("INVALID_URL", "链接不能包含用户信息且必须包含域名", false, nil)
	}
	port := parsed.Port()
	if port != "" && port != "80" && port != "443" {
		return "", "", [32]byte{}, collectError("UNSUPPORTED_PORT", "只允许访问 80 或 443 端口", false, nil)
	}
	host := strings.ToLower(strings.TrimSuffix(parsed.Hostname(), "."))
	if (parsed.Scheme == "http" && port == "80") || (parsed.Scheme == "https" && port == "443") {
		port = ""
	}
	parsed.Host = host
	if port != "" {
		parsed.Host = net.JoinHostPort(host, port)
	}
	parsed.Fragment = ""
	query := parsed.Query()
	if isWechatHost(host) {
		allowed := map[string]bool{"__biz": true, "mid": true, "idx": true, "sn": true}
		for key := range query {
			if !allowed[key] {
				query.Del(key)
			}
		}
	} else {
		for key := range query {
			lower := strings.ToLower(key)
			if strings.HasPrefix(lower, "utm_") || lower == "spm" || lower == "from" || lower == "source" {
				query.Del(key)
			}
		}
	}
	parsed.RawQuery = stableQuery(query)
	canonical := parsed.String()
	return canonical, DetectSourceType(parsed), sha256.Sum256([]byte(canonical)), nil
}

func DetectSourceType(parsed *url.URL) string {
	if isWechatHost(strings.ToLower(parsed.Hostname())) {
		return "wechat"
	}
	return "web"
}

func ValidateTarget(ctx context.Context, resolver *net.Resolver, parsed *url.URL) error {
	if parsed == nil {
		return collectError("INVALID_URL", "链接格式不正确", false, nil)
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return collectError("UNSUPPORTED_SCHEME", "只支持 HTTP 或 HTTPS 链接", false, nil)
	}
	if parsed.User != nil || parsed.Hostname() == "" {
		return collectError("INVALID_URL", "链接不能包含用户信息且必须包含域名", false, nil)
	}
	port := parsed.Port()
	if port != "" && port != "80" && port != "443" {
		return collectError("UNSUPPORTED_PORT", "只允许访问 80 或 443 端口", false, nil)
	}
	host := strings.ToLower(strings.TrimSuffix(parsed.Hostname(), "."))
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return collectError("PRIVATE_ADDRESS", "不允许访问本机或内网地址", false, nil)
	}
	if address, err := netip.ParseAddr(host); err == nil {
		if blockedAddress(address) {
			return collectError("PRIVATE_ADDRESS", "不允许访问本机或内网地址", false, nil)
		}
		return nil
	}
	addresses, err := resolver.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return collectError("DNS_FAILED", "域名解析失败", true, err)
	}
	if len(addresses) == 0 {
		return collectError("DNS_FAILED", "域名没有可用地址", true, nil)
	}
	for _, address := range addresses {
		if blockedAddress(address) {
			return collectError("PRIVATE_ADDRESS", "域名解析到了本机或内网地址", false, nil)
		}
	}
	return nil
}

func blockedAddress(address netip.Addr) bool {
	if address.Is4In6() {
		address = address.Unmap()
	}
	if !address.IsValid() || address.IsPrivate() || address.IsLoopback() || address.IsLinkLocalUnicast() || address.IsLinkLocalMulticast() || address.IsMulticast() || address.IsUnspecified() {
		return true
	}
	for _, prefix := range blockedPrefixes {
		if prefix.Contains(address) {
			return true
		}
	}
	return false
}

func isWechatHost(host string) bool {
	return host == "mp.weixin.qq.com" || strings.HasSuffix(host, ".mp.weixin.qq.com")
}

func stableQuery(values url.Values) string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	ordered := make(url.Values, len(values))
	for _, key := range keys {
		ordered[key] = values[key]
	}
	return ordered.Encode()
}

func mustPrefixes(values ...string) []netip.Prefix {
	result := make([]netip.Prefix, 0, len(values))
	for _, value := range values {
		prefix, err := netip.ParsePrefix(value)
		if err != nil {
			panic(fmt.Sprintf("invalid prefix %s", value))
		}
		result = append(result, prefix)
	}
	return result
}
