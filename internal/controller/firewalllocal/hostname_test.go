package firewalllocal

import (
	"context"
	"errors"
	"net/netip"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/controller/firewall"
)

func TestHostnameAdmissionRequiresOneLocalAddressAndTheRuntimeZone(t *testing.T) {
	local := netip.MustParseAddr("192.0.2.1")
	localV6 := netip.MustParseAddr("2001:db8::1")
	oversized := make([]netip.Addr, 65)
	for i := range oversized {
		oversized[i] = local
	}
	for _, test := range []struct {
		name      string
		addresses []netip.Addr
		lookupErr error
		state     string
		status    string
		query     bool
	}{
		{"local", []netip.Addr{local}, nil, "running", "open", true},
		{"local IPv6", []netip.Addr{localV6}, nil, "running", "open", true},
		{"duplicate records", []netip.Addr{local, local}, nil, "running", "open", true},
		{"mapped duplicate", []netip.Addr{local, netip.MustParseAddr("::ffff:192.0.2.1")}, nil, "running", "open", true},
		{"answer at limit", oversized[:64], nil, "running", "open", true},
		{"lookup failed", nil, errors.New("resolver failed"), "running", "unknown", false},
		{"lookup deadline", nil, context.DeadlineExceeded, "running", "unknown", false},
		{"empty answer", nil, nil, "running", "unknown", false},
		{"invalid answer", []netip.Addr{{}}, nil, "running", "unknown", false},
		{"unspecified", []netip.Addr{netip.MustParseAddr("0.0.0.0")}, nil, "running", "unknown", false},
		{"loopback", []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil, "running", "unknown", false},
		{"multicast", []netip.Addr{netip.MustParseAddr("224.0.0.1")}, nil, "running", "unknown", false},
		{"foreign", []netip.Addr{netip.MustParseAddr("192.0.2.2")}, nil, "running", "unknown", false},
		{"ambiguous", []netip.Addr{local, netip.MustParseAddr("192.0.2.2")}, nil, "running", "unknown", false},
		{"multiple local addresses", []netip.Addr{local, localV6}, nil, "running", "unknown", false},
		{"large answer", oversized, nil, "running", "unknown", false},
		{"daemon unavailable", []netip.Addr{local}, nil, "stopped", "unknown", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			lookups, queries := 0, 0
			inspector := Inspector{
				Interfaces: func() ([]Interface, error) {
					return []Interface{{Name: "synthetic0", Addresses: []netip.Addr{local, localV6}}}, nil
				},
				LookupIP: func(ctx context.Context, host string) ([]netip.Addr, error) {
					lookups++
					if host != "controller.example.test" {
						t.Fatalf("resolved %q", host)
					}
					if _, bounded := ctx.Deadline(); !bounded {
						t.Fatal("resolver was given no inspection deadline")
					}
					return test.addresses, test.lookupErr
				},
				Read: func(_ context.Context, args []string) ([]byte, error) {
					queries++
					return hostnameZoneRead(args, test.state)
				},
			}
			checks, err := firewall.CheckSockets(context.Background(), inspector, []firewall.Socket{{Address: "controller.example.test", Protocol: "tcp", Port: 8443}})
			if err != nil || len(checks) != 1 || checks[0].Observation.Status != test.status || lookups != 1 || (queries > 0) != test.query {
				t.Fatalf("checks=%+v, lookups=%d, queries=%d, err=%v", checks, lookups, queries, err)
			}
			if test.status == "open" && (checks[0].Observation.Interface != "synthetic0" || checks[0].Observation.Zone != "public") {
				t.Fatalf("hostname lost its proved interface and zone: %+v", checks)
			}
		})
	}
}

func TestHostnameCannotBypassAClosedRuntimePort(t *testing.T) {
	local := netip.MustParseAddr("192.0.2.1")
	inspector := Inspector{
		Interfaces: func() ([]Interface, error) {
			return []Interface{{Name: "synthetic0", Addresses: []netip.Addr{local}}}, nil
		},
		LookupIP: func(context.Context, string) ([]netip.Addr, error) { return []netip.Addr{local}, nil },
		Read:     func(_ context.Context, args []string) ([]byte, error) { return hostnameZoneRead(args, "running") },
	}
	checks, err := firewall.CheckSockets(context.Background(), inspector, []firewall.Socket{{Address: "controller.example.test", Protocol: "tcp", Port: 8080}})
	if err != nil || len(checks) != 1 || checks[0].Observation.Status != "closed" {
		t.Fatalf("a hostname bypassed the runtime port proof: %+v (%v)", checks, err)
	}
}

func TestHostnameLookupCancellationPreventsFirewallQueries(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	inspector := Inspector{
		Interfaces: func() ([]Interface, error) { return nil, nil },
		LookupIP: func(ctx context.Context, _ string) ([]netip.Addr, error) {
			cancel()
			return nil, ctx.Err()
		},
		Read: func(context.Context, []string) ([]byte, error) {
			t.Fatal("queried a firewall after cancelled resolution")
			return nil, nil
		},
	}
	if _, err := firewall.CheckSockets(ctx, inspector, []firewall.Socket{{Address: "controller.example.test", Protocol: "tcp", Port: 8443}}); !errors.Is(err, context.Canceled) {
		t.Fatalf("lost parent cancellation: %v", err)
	}
}

func TestLiteralAddressNeverInvokesHostnameLookup(t *testing.T) {
	inspector := Inspector{
		Interfaces: func() ([]Interface, error) {
			return []Interface{{Name: "synthetic0", Addresses: []netip.Addr{netip.MustParseAddr("192.0.2.1")}}}, nil
		},
		LookupIP: func(context.Context, string) ([]netip.Addr, error) {
			t.Fatal("looked up an IP literal")
			return nil, nil
		},
		Read: func(_ context.Context, args []string) ([]byte, error) { return hostnameZoneRead(args, "running") },
	}
	result, err := inspector.Inspect(context.Background(), firewall.Socket{Address: "192.0.2.1", Protocol: "tcp", Port: 8443})
	if err != nil || result.Status != "open" {
		t.Fatal(result, err)
	}
}

func hostnameZoneRead(args []string, state string) ([]byte, error) {
	values := map[string]string{
		"--state": state, "--query-panic": "no", "--get-active-zones": "public\n  interfaces: synthetic0",
		"--get-active-policies": "", "--direct --get-all-rules": "", "--direct --get-all-passthroughs": "",
		"--get-zone-of-interface=synthetic0": "public", "--zone=public --list-rich-rules": "",
		"--zone=public --list-all": "public\n  target: default\n  sources:\n  services:\n  ports: 8443/tcp\n  protocols:\n  forward-ports:\n  source-ports:\n  rich rules:",
	}
	value, found := values[strings.Join(args, " ")]
	if !found {
		return nil, errors.New("unqualified firewall query")
	}
	return []byte(value), nil
}
