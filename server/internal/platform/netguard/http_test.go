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
		"private.example": {netip.MustParseAddr("10.0.0.8")},
		"public.example":  {netip.MustParseAddr("93.184.216.34")},
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
		"https://private.example/v1",
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

func TestDialerRevalidatesDNSAndRejectsPrivateAddress(t *testing.T) {
	client := NewHTTPClient(time.Second, &fakeResolver{addresses: map[string][]netip.Addr{
		"rebind.example": {netip.MustParseAddr("127.0.0.1")},
	}})
	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport type = %T", client.Transport)
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

var _ Resolver = (*net.Resolver)(nil)
