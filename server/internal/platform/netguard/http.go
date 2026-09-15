package netguard

import (
	"context"
	"errors"
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

type guardedTransport struct {
	base     http.RoundTripper
	resolver Resolver
}

type unsafeURLError struct {
	message string
}

func (e *unsafeURLError) Error() string   { return e.message }
func (e *unsafeURLError) UnsafeURL() bool { return true }

func IsUnsafeURL(err error) bool {
	var target interface{ UnsafeURL() bool }
	return errors.As(err, &target) && target.UnsafeURL()
}

func unsafeURL(message string) error {
	return &unsafeURLError{message: message}
}

var blockedPrefixes = mustPrefixes(
	"0.0.0.0/8", "10.0.0.0/8", "100.64.0.0/10", "127.0.0.0/8", "169.254.0.0/16",
	"172.16.0.0/12", "192.0.0.0/24", "192.0.2.0/24", "192.168.0.0/16", "198.18.0.0/15",
	"192.31.196.0/24", "192.52.193.0/24", "192.88.99.0/24", "192.175.48.0/24",
	"198.51.100.0/24", "203.0.113.0/24", "224.0.0.0/4", "240.0.0.0/4",
	"::/96", "::ffff:0:0:0/96", "64:ff9b::/96", "64:ff9b:1::/48", "100::/64",
	"2001::/23", "2001:db8::/32", "2002::/16", "2620:4f:8000::/48", "3fff::/20", "5f00::/16",
	"fc00::/7", "fe80::/10", "fec0::/10", "ff00::/8",
)

func ValidateHTTPSURL(ctx context.Context, resolver Resolver, raw string) error {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || !strings.EqualFold(parsed.Scheme, "https") || parsed.Hostname() == "" {
		return unsafeURL("AI base URL must be an absolute HTTPS URL")
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" {
		return unsafeURL("AI base URL must not contain user info, query or fragment")
	}
	if port := parsed.Port(); port != "" && port != "443" {
		return unsafeURL("AI base URL only allows port 443")
	}
	host := strings.ToLower(strings.TrimSuffix(parsed.Hostname(), "."))
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return unsafeURL("AI base URL must use a public host")
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
		Transport: newGuardedTransport(transport, resolver),
		Timeout:   timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

func newGuardedTransport(base http.RoundTripper, resolver Resolver) http.RoundTripper {
	return &guardedTransport{base: base, resolver: resolver}
}

func (t *guardedTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if request == nil || request.URL == nil {
		return nil, fmt.Errorf("AI request URL is required")
	}
	if err := ValidateHTTPSURL(request.Context(), t.resolver, request.URL.String()); err != nil {
		return nil, err
	}
	return t.base.RoundTrip(request)
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
			return unsafeURL("AI base URL must use a public host")
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
			return nil, unsafeURL("AI base URL resolved to a private address")
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
