package firewalllocal

import (
	"context"
	"net/netip"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/controller/firewall"
)

func TestOneCheckReadsHostWideStateOnceAndEachZoneOnce(t *testing.T) {
	answers := map[string]string{
		"--state": "running", "--query-panic": "no", "--get-active-zones": "public\n  interfaces: synthetic0",
		"--get-active-policies":         "allow-host-ipv6\n  ingress-zones: ANY\n  egress-zones: HOST",
		"--info-policy=allow-host-ipv6": "allow-host-ipv6 (active)\n  priority: -15000\n  target: CONTINUE\n  ingress-zones: ANY\n  egress-zones: HOST\n  services:\n  ports:\n  protocols:\n  masquerade: no\n  forward-ports:\n  source-ports:\n  icmp-blocks:\n  rich rules:\n    rule family=\"ipv6\" icmp-type name=\"neighbour-solicitation\" accept",
		"--direct --get-all-rules":      "", "--direct --get-all-passthroughs": "",
		"--get-zone-of-interface=synthetic0": "public", "--zone=public --list-rich-rules": "",
		"--zone=public --list-all":                "public\n  target: default\n  sources:\n  services: bootwright-lab-artifacts\n  ports: 8443/tcp\n  protocols:\n  forward-ports:\n  source-ports:\n  rich rules:",
		"--info-service=bootwright-lab-artifacts": "bootwright-lab-artifacts\n  ports: 9000/tcp\n  protocols:\n  source-ports:\n  modules:\n  destination:\n  includes:\n  helpers:",
	}
	counts := map[string]int{}
	inspector := Inspector{
		Interfaces: func() ([]Interface, error) {
			return []Interface{{Name: "synthetic0", Addresses: []netip.Addr{netip.MustParseAddr("192.0.2.1")}}}, nil
		},
		Read: func(_ context.Context, args []string) ([]byte, error) {
			key := strings.Join(args, " ")
			counts[key]++
			return []byte(answers[key]), nil
		},
	}
	sockets := []firewall.Socket{
		{Object: "a", Address: "192.0.2.1", Protocol: "tcp", Port: 8443},
		{Object: "b", Address: "192.0.2.1", Protocol: "tcp", Port: 9000},
		{Object: "c", Address: "192.0.2.1", Protocol: "udp", Port: 53},
	}
	checks, err := firewall.CheckSockets(context.Background(), inspector, sockets)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"open", "open", "closed"}
	for index, check := range checks {
		if check.Observation.Status != want[index] || check.Observation.Zone != "public" {
			t.Fatalf("check %d = %+v", index, check)
		}
	}
	total := 0
	for key, count := range counts {
		total += count
		if count != 1 {
			t.Fatalf("%q read %d times in one check", key, count)
		}
	}
	if total != len(answers) {
		t.Fatalf("started %d firewall reads, want %d", total, len(answers))
	}
}

func TestACheckNeverReusesAnotherChecksReads(t *testing.T) {
	calls := 0
	inspector := Inspector{
		Interfaces: func() ([]Interface, error) {
			return []Interface{{Name: "synthetic0", Addresses: []netip.Addr{netip.MustParseAddr("192.0.2.1")}}}, nil
		},
		Read: func(context.Context, []string) ([]byte, error) { calls++; return []byte("unavailable"), nil },
	}
	socket := []firewall.Socket{{Address: "192.0.2.1", Protocol: "tcp", Port: 8443}}
	for range 2 {
		if _, err := firewall.CheckSockets(context.Background(), inspector, socket); err != nil {
			t.Fatal(err)
		}
	}
	if calls != 2 {
		t.Fatalf("two checks started %d reads, want 2", calls)
	}
}
