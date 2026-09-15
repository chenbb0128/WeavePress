package netguard

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"
)

type Resolver interface {
	LookupNetIP(context.Context, string, string) ([]netip.Addr, error)
}

var blockedPrefixes = mustPrefixes(
	"0.0.0.0/8", "10.0.0.0/8", "100.64.0.0/10", "127.0.0.0/8", "169.254.0.0/16",
	"172.16.0.0/12", "192.0.0.0/24", "192.0.2.0/24", "192.168.0.0/16", "198.18.0.0/15",
	"198.51.100.0/24", "203.0.113.0/24", "224.0.0.0/4", "240.0.0.0/4",
	"::/128", "::1/128", "fc00::/7", "fe80::/10", "ff00::/8", "2001:db8::/32",
)

func ValidateHTTPSURL(ctx context.Context, resolver Resolver, raw string) error {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || !strings.EqualFold(parsed.Scheme, "https") || parsed.Hostname() == "" {
		return fmt.Errorf("AI base URL must be an absolute HTTPS URL")
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" {
		return fmt.Errorf("AI base URL must not contain user info, query or fragment")
	}
	if port := parsed.Port(); port != "" && port != "443" {
		return fmt.Errorf("AI base URL only allows port 443")
	}
	host := strings.ToLower(strings.TrimSuffix(parsed.Hostname(), "."))
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return fmt.Errorf("AI base URL must use a public host")
	}
	return validateHost(ctx, resolver, host)
}

func NewHTTPClient(timeout time.Duration, resolver Resolver) *http.Client {
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.DialContext = restrictedDialer(resolver)
	return &http.Client{
		Transport: transport,
		Timeout:   timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

func restrictedDialer(resolver Resolver) func(context.Context, string, string) (net.Conn, error) {
	dialer := &net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		addresses, err := resolvePublic(ctx, resolver, strings.TrimSuffix(host, "."))
		if err != nil {
			return nil, err
		}
		var lastErr error
		for _, resolved := range addresses {
			connection, err := dialer.DialContext(ctx, network, net.JoinHostPort(resolved.String(), port))
			if err == nil {
				return connection, nil
			}
			lastErr = err
		}
		if lastErr != nil {
			return nil, lastErr
		}
		return nil, fmt.Errorf("AI target has no public IP address")
	}
}

func validateHost(ctx context.Context, resolver Resolver, host string) error {
	if address, err := netip.ParseAddr(host); err == nil {
		if blockedAddress(address) {
			return fmt.Errorf("AI base URL must use a public host")
		}
		return nil
	}
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	_, err := resolvePublic(ctx, resolver, host)
	return err
}

func resolvePublic(ctx context.Context, resolver Resolver, host string) ([]netip.Addr, error) {
	addresses, err := resolver.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return nil, fmt.Errorf("resolve AI base URL: %w", err)
	}
	if len(addresses) == 0 {
		return nil, fmt.Errorf("AI base URL has no IP address")
	}
	result := make([]netip.Addr, 0, len(addresses))
	for _, address := range addresses {
		if address.Is4In6() {
			address = address.Unmap()
		}
		if blockedAddress(address) {
			return nil, fmt.Errorf("AI base URL resolved to a private address")
		}
		result = append(result, address)
	}
	return result, nil
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

func mustPrefixes(values ...string) []netip.Prefix {
	result := make([]netip.Prefix, 0, len(values))
	for _, value := range values {
		result = append(result, netip.MustParsePrefix(value))
	}
	return result
}
