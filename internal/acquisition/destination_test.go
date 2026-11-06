package acquisition

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"testing"
)

func TestAcquisitionChecksAndPinsResolvedDestination(t *testing.T) {
	for _, test := range []struct {
		name, address    string
		private, allowed bool
	}{
		{"public", "93.184.216.34", false, true}, {"private", "10.1.2.3", false, false},
		{"private-mirror", "10.1.2.3", true, true}, {"ula", "fd00::1", false, false},
		{"ula-mirror", "fd00::1", true, true}, {"loopback-rebind", "127.0.0.1", true, false},
		{"linklocal-rebind", "169.254.169.254", true, false}, {"v6-linklocal", "fe80::1", true, false},
		{"mapped-loopback", "::ffff:127.0.0.1", false, false}, {"shared-carrier", "100.64.0.1", false, false},
		{"unspecified", "0.0.0.0", true, false}, {"multicast", "224.0.0.1", true, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls, lookups := 0, 0
			base := &http.Transport{DialContext: func(_ context.Context, network, address string) (net.Conn, error) {
				calls++
				if address != net.JoinHostPort(netip.MustParseAddr(test.address).Unmap().String(), "443") {
					t.Fatalf("dialed unchecked address %q", address)
				}
				return nil, errors.New("injected dial refusal")
			}}
			guard := GuardAcquisitionTransport(base, func(*url.URL) bool { return true }).(*acquisitionTransport)
			guard.lookup = func(context.Context, string, string) ([]netip.Addr, error) {
				lookups++
				return []netip.Addr{netip.MustParseAddr(test.address)}, nil
			}
			request, _ := http.NewRequest(http.MethodGet, "https://publisher.example/wheel", nil)
			if test.private {
				request = request.WithContext(PrivateAcquisitionEndpoint(request.Context(), request.URL))
			}
			_, err := guard.RoundTrip(request)
			if err == nil || lookups != 1 || (calls == 1) != test.allowed {
				t.Fatalf("calls=%d lookups=%d error=%v", calls, lookups, err)
			}
			if !test.allowed {
				var boundary *Failure
				if !errors.As(err, &boundary) || boundary.Routable() {
					t.Fatalf("missing boundary refusal: %v", err)
				}
			}
		})
	}
}

func TestAcquisitionRechecksDNSAndNeverDialsMixedPrivateAnswers(t *testing.T) {
	calls, lookups := 0, 0
	base := &http.Transport{DialContext: func(context.Context, string, string) (net.Conn, error) {
		calls++
		return nil, errors.New("no connection")
	}}
	guard := GuardAcquisitionTransport(base, func(*url.URL) bool { return true }).(*acquisitionTransport)
	guard.lookup = func(context.Context, string, string) ([]netip.Addr, error) {
		lookups++
		if lookups == 1 {
			return []netip.Addr{netip.MustParseAddr("93.184.216.34")}, nil
		}
		return []netip.Addr{netip.MustParseAddr("93.184.216.34"), netip.MustParseAddr("127.0.0.1")}, nil
	}
	for range 2 {
		request, _ := http.NewRequest(http.MethodGet, "https://publisher.example/wheel", nil)
		if _, err := guard.RoundTrip(request); err == nil {
			t.Fatal("unexpected request success")
		}
	}
	if calls != 1 || lookups != 2 {
		t.Fatalf("rebound endpoint dialed: calls=%d lookups=%d", calls, lookups)
	}
}

func TestAcquisitionExplicitProxyAndLiteralMirrorPolicy(t *testing.T) {
	for _, proxy := range []bool{false, true} {
		calls := 0
		base := &http.Transport{DialContext: func(_ context.Context, _, address string) (net.Conn, error) {
			calls++
			if address != "127.0.0.1:443" {
				t.Fatal(address)
			}
			return nil, errors.New("no connection")
		}}
		endpoint, _ := url.Parse("https://127.0.0.1")
		if proxy {
			base.Proxy = func(*http.Request) (*url.URL, error) { return endpoint, nil }
		}
		guard := GuardAcquisitionTransport(base, func(*url.URL) bool { return true }).(*acquisitionTransport)
		guard.lookup = func(context.Context, string, string) ([]netip.Addr, error) {
			t.Fatal("literal was resolved again")
			return nil, nil
		}
		target := "https://publisher.example/wheel"
		if !proxy {
			target = "https://127.0.0.1/wheel"
		}
		request, _ := http.NewRequest(http.MethodGet, target, nil)
		if !proxy {
			request = request.WithContext(PrivateAcquisitionEndpoint(request.Context(), request.URL))
		}
		if _, err := guard.RoundTrip(request); err == nil || calls != 1 {
			t.Fatalf("explicit endpoint was not used: %v %d", err, calls)
		}
	}
}

func TestAcquisitionRejectsURLBeforeResolverOrDial(t *testing.T) {
	base := &http.Transport{DialContext: func(context.Context, string, string) (net.Conn, error) {
		t.Fatal("refused URL dialed")
		return nil, nil
	}}
	guard := GuardAcquisitionTransport(base, func(*url.URL) bool { return false }).(*acquisitionTransport)
	guard.lookup = func(context.Context, string, string) ([]netip.Addr, error) {
		t.Fatal("refused URL resolved")
		return nil, nil
	}
	request, _ := http.NewRequest(http.MethodGet, "https://unapproved.example/wheel", nil)
	if _, err := guard.RoundTrip(request); err == nil {
		t.Fatal("unapproved initial URL accepted")
	}
}

func TestPrivateMirrorAuthorizationDoesNotTransferToAnotherHost(t *testing.T) {
	calls := 0
	base := &http.Transport{DialContext: func(context.Context, string, string) (net.Conn, error) {
		calls++
		return nil, errors.New("no connection")
	}}
	guard := GuardAcquisitionTransport(base, func(*url.URL) bool { return true }).(*acquisitionTransport)
	guard.lookup = func(context.Context, string, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("10.1.2.3")}, nil
	}
	original, _ := url.Parse("https://mirror.example")
	for _, target := range []string{"https://mirror.example:443/wheel", "https://other.example/wheel"} {
		request, _ := http.NewRequestWithContext(PrivateAcquisitionEndpoint(context.Background(), original), http.MethodGet, target, nil)
		if _, err := guard.RoundTrip(request); err == nil {
			t.Fatal("unexpected connection success")
		}
	}
	if calls != 1 {
		t.Fatalf("private authority widened: %d calls", calls)
	}
}
