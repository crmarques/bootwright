package firewalllocal

import (
	"context"
	"errors"
	"net/netip"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/controller/firewall"
)

func TestRuntimeZoneAdmissionAndFailClosedShapes(t *testing.T) {
	base := map[string]string{"--state": "running", "--query-panic": "no", "--get-active-zones": "public\n  interfaces: synthetic0", "--get-active-policies": "", "--direct --get-all-rules": "", "--direct --get-all-passthroughs": "", "--get-zone-of-interface=synthetic0": "public", "--zone=public --list-rich-rules": "", "--zone=public --list-all": "public\n  target: default\n  sources:\n  services:\n  ports: 8443/tcp\n  protocols:\n  forward-ports:\n  source-ports:\n  rich rules:"}
	for name, test := range map[string]struct{ key, value, status string }{
		"open":              {"", "", "open"},
		"closed":            {"--zone=public --list-all", strings.ReplaceAll(base["--zone=public --list-all"], "8443/tcp", "53/udp"), "closed"},
		"no daemon":         {"--state", "unavailable", "unknown"},
		"panic":             {"--query-panic", "yes", "unknown"},
		"source zone":       {"--get-active-zones", "public\n  interfaces: synthetic0\nother\n  sources: 192.0.2.0/24", "unknown"},
		"policy":            {"--get-active-policies", "host-policy", "unknown"},
		"direct":            {"--direct --get-all-rules", "ipv4 filter INPUT 0 -j DROP", "unknown"},
		"passthrough":       {"--direct --get-all-passthroughs", "ipv4 -A INPUT -j DROP", "unknown"},
		"rich":              {"--zone=public --list-rich-rules", "rule drop", "unknown"},
		"service expansion": {"--zone=public --list-all", "public\n target: default\n services: https\n ports:", "unknown"},
		"incomplete":        {"--zone=public --list-all", "public\n ports: 8443/tcp", "unknown"},
	} {
		t.Run(name, func(t *testing.T) {
			inspector := Inspector{Interfaces: func() ([]Interface, error) {
				return []Interface{{Name: "synthetic0", Addresses: []netip.Addr{netip.MustParseAddr("192.0.2.1")}}}, nil
			}, Read: func(_ context.Context, args []string) ([]byte, error) {
				key := strings.Join(args, " ")
				if strings.Contains(key, "--add") || strings.Contains(key, "--remove") || strings.Contains(key, "--permanent") {
					t.Fatal("mutating firewall query", key)
				}
				if key == test.key {
					return []byte(test.value), nil
				}
				value, found := base[key]
				if !found {
					return nil, errors.New("unsupported query")
				}
				return []byte(value), nil
			}}
			result, err := inspector.Inspect(context.Background(), firewall.Socket{Address: "192.0.2.1", Protocol: "tcp", Port: 8443})
			if err != nil || result.Status != test.status {
				t.Fatalf("result=%+v err=%v", result, err)
			}
		})
	}
}

func TestAddressOwnershipMustBeUnique(t *testing.T) {
	for _, links := range [][]Interface{nil, {{Name: "one", Addresses: []netip.Addr{netip.MustParseAddr("192.0.2.1")}}, {Name: "two", Addresses: []netip.Addr{netip.MustParseAddr("192.0.2.1")}}}} {
		inspector := Inspector{Interfaces: func() ([]Interface, error) { return links, nil }, Read: func(context.Context, []string) ([]byte, error) {
			t.Fatal("queried a firewall without exact interface identity")
			return nil, nil
		}}
		result, err := inspector.Inspect(context.Background(), firewall.Socket{Address: "192.0.2.1", Protocol: "tcp", Port: 443})
		if err != nil || result.Status != "unknown" {
			t.Fatal(result, err)
		}
	}
}

func TestOnlyDocumentedNegativeQueryResultsAreQualified(t *testing.T) {
	for _, test := range []struct {
		args            []string
		output, failure string
		code            int
		valid           bool
	}{
		{[]string{"--query-panic"}, "no\n", "", 1, true},
		{[]string{"--get-zone-of-interface=synthetic0"}, "", "no zone\n", 2, true},
		{[]string{"--get-zone-of-interface=synthetic0"}, "", "permission denied\n", 2, false},
		{[]string{"--get-zone-of-interface=synthetic0"}, "", "no zone\n", 13, false},
		{[]string{"--state"}, "no\n", "", 1, false},
	} {
		if got := qualifiedNegative(test.args, test.output, test.failure, test.code); got != test.valid {
			t.Fatal(test, got)
		}
	}
}
