package firewalllocal

import (
	"context"
	"errors"
	"net/netip"
	"os"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/controller/firewall"
)

func TestStockIPv6PolicyDoesNotBlockDeclaredTCPPort(t *testing.T) {
	data, err := os.ReadFile("testdata/allow-host-ipv6.txt")
	if err != nil {
		t.Fatal(err)
	}
	policy := strings.TrimSpace(string(data))
	for _, name := range []string{"allow-host-ipv6", "equivalent-renamed-policy"} {
		inspector := Inspector{Interfaces: func() ([]Interface, error) {
			return []Interface{{Name: "synthetic0", Addresses: []netip.Addr{netip.MustParseAddr("192.0.2.1")}}}, nil
		}, Read: func(_ context.Context, args []string) ([]byte, error) {
			values := map[string]string{"--state": "running", "--query-panic": "no", "--get-active-zones": "public\n interfaces: synthetic0", "--get-active-policies": name + "\n  ingress-zones: ANY\n  egress-zones: HOST", "--info-policy=" + name: strings.ReplaceAll(policy, "allow-host-ipv6", name), "--get-zone-of-interface=synthetic0": "public", "--zone=public --list-all": "public\n target: default\n sources:\n services: ssh dhcpv6-client\n ports: 8443/tcp\n protocols:\n forward-ports:\n source-ports:\n rich rules:"}
			value, found := values[strings.Join(args, " ")]
			if !found {
				if len(args) == 2 && args[0] == "--direct" || len(args) == 2 && args[1] == "--list-rich-rules" {
					return nil, nil
				}
				return nil, errors.New("unqualified query")
			}
			return []byte(value), nil
		}}
		result, err := inspector.Inspect(context.Background(), firewall.Socket{Address: "192.0.2.1", Protocol: "tcp", Port: 8443})
		if err != nil || result.Status != "open" {
			t.Fatalf("stock or renamed policy blocks an explicit listener: %+v %v", result, err)
		}
	}
	for _, mutated := range []string{strings.ReplaceAll(policy, "CONTINUE", "DROP"), strings.ReplaceAll(policy, "accept", "drop"), strings.ReplaceAll(policy, "icmp-type", "port"), strings.ReplaceAll(policy, "ports:", "ports: 8443/tcp"), strings.ReplaceAll(policy, "masquerade: no", "masquerade: yes"), strings.ReplaceAll(policy, "target: CONTINUE", "target: CONTINUE\n unknown: enabled")} {
		if harmlessPolicy(mutated, "allow-host-ipv6") {
			t.Fatal("unsafe policy was qualified", mutated)
		}
		inspector := Inspector{Interfaces: func() ([]Interface, error) {
			return []Interface{{Name: "synthetic0", Addresses: []netip.Addr{netip.MustParseAddr("192.0.2.1")}}}, nil
		}, Read: func(_ context.Context, args []string) ([]byte, error) {
			values := map[string]string{"--state": "running", "--query-panic": "no", "--get-active-zones": "public\n interfaces: synthetic0", "--get-active-policies": "allow-host-ipv6\n  ingress-zones: ANY\n  egress-zones: HOST", "--info-policy=allow-host-ipv6": mutated}
			value, found := values[strings.Join(args, " ")]
			if !found {
				t.Fatal("unqualified policy reached later firewall queries", args)
			}
			return []byte(value), nil
		}}
		observed, err := inspector.Inspect(context.Background(), firewall.Socket{Address: "192.0.2.1", Protocol: "tcp", Port: 8443})
		if err != nil || observed.Status != "unknown" {
			t.Fatal(observed, err)
		}
	}
}
