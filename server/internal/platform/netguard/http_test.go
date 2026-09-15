package netguard

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"testing"
	"time"
)

type fakeResolver struct {
	addresses map[string][]netip.Addr
	err       error
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func (r *fakeResolver) LookupNetIP(_ context.Context, _, host string) ([]netip.Addr, error) {
	if r.err != nil {
		return nil, r.err
	}
	return r.addresses[host], nil
}

func TestValidateHTTPSURLAllowsPublicHTTPS(t *testing.T) {
	resolver := &fakeResolver{addresses: map[string][]netip.Addr{
		"public.example": {netip.MustParseAddr("93.184.216.34")},
	}}
	if err := ValidateHTTPSURL(context.Background(), resolver, "https://public.example/v1"); err != nil {
		t.Fatal(err)
	}
}

func TestValidateHTTPSURLRejectsUnsafeTargets(t *testing.T) {
	resolver := &fakeResolver{addresses: map[string][]netip.Addr{
		"private.example":    {netip.MustParseAddr("10.0.0.8")},
		"public.example":     {netip.MustParseAddr("93.184.216.34")},
		"special.example":    {netip.MustParseAddr("64:ff9b:1::c0a8:1")},
		"site-local.example": {netip.MustParseAddr("fec0::1")},
	}}
	values := []string{
		"http://public.example/v1",
		"https://user@public.example/v1",
		"https://public.example:8443/v1",
		"https://public.example/v1?token=x",
		"https://public.example/v1#fragment",
		"https://localhost/v1",
		"https://127.0.0.1/v1",
		"https://169.254.169.254/latest/meta-data",
		"https://192.31.196.1/v1",
		"https://[64:ff9b::c0a8:1]/v1",
		"https://[64:ff9b:1::c0a8:1]/v1",
		"https://[100::1]/v1",
		"https://[2001:2::1]/v1",
		"https://[2002:c0a8:1::]/v1",
		"https://[fec0::1]/v1",
		"https://private.example/v1",
		"https://site-local.example/v1",
		"https://special.example/v1",
	}
	for _, raw := range values {
		t.Run(raw, func(t *testing.T) {
			if err := ValidateHTTPSURL(context.Background(), resolver, raw); err == nil {
				t.Fatalf("accepted unsafe URL %q", raw)
			}
		})
	}
}

func TestHTTPClientRejectsRedirects(t *testing.T) {
	client := NewHTTPClient(time.Second, &fakeResolver{})
	err := client.CheckRedirect(&http.Request{}, []*http.Request{{}})
	if !errors.Is(err, http.ErrUseLastResponse) {
		t.Fatalf("CheckRedirect() error = %v", err)
	}
}

func TestGuardedTransportRejectsUnsafeURLBeforeSendingRequest(t *testing.T) {
	called := false
	transport := newGuardedTransport(roundTripFunc(func(*http.Request) (*http.Response, error) {
		called = true
		return nil, errors.New("request was sent")
	}), &fakeResolver{addresses: map[string][]netip.Addr{
		"public.example": {netip.MustParseAddr("93.184.216.34")},
	}})
	request, err := http.NewRequest(http.MethodPost, "http://public.example/v1/chat/completions", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer sensitive-key")
	_, err = transport.RoundTrip(request)
	if err == nil {
		t.Fatal("RoundTrip() accepted an unsafe URL")
	}
	if called {
		t.Fatal("unsafe request reached the underlying transport")
	}
}

func TestDialerRevalidatesDNSAndRejectsPrivateAddress(t *testing.T) {
	client := NewHTTPClient(time.Second, &fakeResolver{addresses: map[string][]netip.Addr{
		"rebind.example": {netip.MustParseAddr("127.0.0.1")},
	}})
	guard, ok := client.Transport.(*guardedTransport)
	if !ok {
		t.Fatalf("transport type = %T", client.Transport)
	}
	transport, ok := guard.base.(*http.Transport)
	if !ok {
		t.Fatalf("base transport type = %T", guard.base)
	}
	_, err := transport.DialContext(context.Background(), "tcp", "rebind.example:443")
	if err == nil {
		t.Fatal("dialer accepted a private rebound address")
	}
}

func TestValidateHTTPSURLReturnsDNSFailure(t *testing.T) {
	want := errors.New("dns unavailable")
	err := ValidateHTTPSURL(context.Background(), &fakeResolver{err: want}, "https://public.example/v1")
	if err == nil || !errors.Is(err, want) {
		t.Fatalf("ValidateHTTPSURL() error = %v", err)
	}
}

func TestValidateHTTPSURLClassifiesPolicyButNotResolverFailure(t *testing.T) {
	policyErr := ValidateHTTPSURL(context.Background(), &fakeResolver{}, "http://public.example/v1")
	if !IsUnsafeURL(policyErr) {
		t.Fatalf("policy error was not classified as unsafe: %v", policyErr)
	}
	dnsErr := errors.New("DNS temporarily unavailable")
	resolveErr := ValidateHTTPSURL(context.Background(), &fakeResolver{err: dnsErr}, "https://public.example/v1")
	if IsUnsafeURL(resolveErr) || !errors.Is(resolveErr, dnsErr) {
		t.Fatalf("resolver error classification = %v", resolveErr)
	}
}

var _ Resolver = (*net.Resolver)(nil)
