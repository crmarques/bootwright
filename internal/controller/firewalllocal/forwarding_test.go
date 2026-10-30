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

func TestForwardingPolicyLeavesHostAdmissionToZone(t *testing.T) {
	const name = "transit-policy"
	const policy = `transit-policy (active)
  disable: no
  priority: -1
  target: ACCEPT
  ingress-zones: ANY
  egress-zones: workloads
  services:
  ports:
  protocols:
  masquerade: no
  forward-ports:
  source-ports:
  icmp-blocks:
  rich rules:`

	for label, test := range map[string]struct {
		old, replacement string
		zonePorts        string
		protocol         string
		want             string
	}{
		"explicit TCP port":      {zonePorts: "8443/tcp", protocol: "tcp", want: "open"},
		"explicit UDP port":      {zonePorts: "8443/udp", protocol: "udp", want: "open"},
		"closed zone":            {zonePorts: "53/udp", want: "closed"},
		"renamed policy":         {old: name, replacement: "another-policy", zonePorts: "8443/tcp", want: "open"},
		"concrete ingress zones": {old: "ingress-zones: ANY", replacement: "ingress-zones: clients guests", zonePorts: "8443/tcp", want: "open"},
		"any egress zone":        {old: "egress-zones: workloads", replacement: "egress-zones: ANY", zonePorts: "8443/tcp", want: "open"},
		"host ingress":           {old: "ingress-zones: ANY", replacement: "ingress-zones: HOST", want: "unknown"},
		"host egress":            {old: "egress-zones: workloads", replacement: "egress-zones: HOST", want: "unknown"},
		"mixed any ingress":      {old: "ingress-zones: ANY", replacement: "ingress-zones: ANY clients", want: "unknown"},
		"mixed any egress":       {old: "egress-zones: workloads", replacement: "egress-zones: ANY workloads", want: "unknown"},
		"mixed host egress":      {old: "egress-zones: workloads", replacement: "egress-zones: HOST workloads", want: "unknown"},
		"duplicate zone":         {old: "egress-zones: workloads", replacement: "egress-zones: workloads workloads", want: "unknown"},
		"missing zone field":     {old: "  egress-zones: workloads\n", replacement: "", want: "unknown"},
		"empty zone set":         {old: "egress-zones: workloads", replacement: "egress-zones:", want: "unknown"},
		"invalid zone":           {old: "egress-zones: workloads", replacement: "egress-zones: *", want: "unknown"},
		"unknown target":         {old: "target: ACCEPT", replacement: "target: OTHER", want: "unknown"},
		"drop target":            {old: "target: ACCEPT", replacement: "target: DROP", want: "unknown"},
		"reject target":          {old: "target: ACCEPT", replacement: "target: REJECT", want: "unknown"},
		"unknown field":          {old: "target: ACCEPT", replacement: "target: ACCEPT\n  unknown: yes", want: "unknown"},
		"duplicate field":        {old: "target: ACCEPT", replacement: "target: ACCEPT\n  target: ACCEPT", want: "unknown"},
		"invalid priority":       {old: "priority: -1", replacement: "priority: first", want: "unknown"},
		"disabled active policy": {old: "disable: no", replacement: "disable: yes", want: "unknown"},
		"service":                {old: "  services:", replacement: "  services: https", want: "unknown"},
		"port":                   {old: "  ports:", replacement: "  ports: 8443/tcp", want: "unknown"},
		"protocol":               {old: "  protocols:", replacement: "  protocols: tcp", want: "unknown"},
		"masquerade":             {old: "masquerade: no", replacement: "masquerade: yes", want: "unknown"},
		"redirect":               {old: "  forward-ports:", replacement: "  forward-ports: port=8443:proto=tcp:toport=9443:toaddr=", want: "unknown"},
		"source port":            {old: "  source-ports:", replacement: "  source-ports: 8443/tcp", want: "unknown"},
		"icmp block":             {old: "  icmp-blocks:", replacement: "  icmp-blocks: echo-request", want: "unknown"},
		"rich transport rule":    {old: "rich rules:", replacement: "rich rules:\n    rule port port=\"8443\" protocol=\"tcp\" drop", want: "unknown"},
		"rich ICMP rule":         {old: "rich rules:", replacement: "rich rules:\n    rule family=\"ipv6\" icmp-type name=\"echo-request\" accept", want: "unknown"},
	} {
		t.Run(label, func(t *testing.T) {
			data, policyName := policy, name
			if test.old != "" {
				data = strings.ReplaceAll(data, test.old, test.replacement)
				if test.old == name {
					policyName = test.replacement
				}
			}
			values := map[string]string{
				"--state":                            "running",
				"--query-panic":                      "no",
				"--get-active-zones":                 "public\n  interfaces: synthetic0",
				"--get-active-policies":              policyName + "\n  ingress-zones: ANY\n  egress-zones: workloads",
				"--info-policy=" + policyName:        data,
				"--direct --get-all-rules":           "",
				"--direct --get-all-passthroughs":    "",
				"--get-zone-of-interface=synthetic0": "public",
				"--zone=public --list-rich-rules":    "",
				"--zone=public --list-all":           "public\n  target: default\n  sources:\n  services:\n  ports: " + test.zonePorts + "\n  protocols:\n  forward-ports:\n  source-ports:\n  rich rules:",
			}
			inspector := Inspector{Interfaces: func() ([]Interface, error) {
				return []Interface{{Name: "synthetic0", Addresses: []netip.Addr{netip.MustParseAddr("192.0.2.1")}}}, nil
			}, Read: func(_ context.Context, args []string) ([]byte, error) {
				value, found := values[strings.Join(args, " ")]
				if !found {
					return nil, errors.New("unsupported query")
				}
				return []byte(value), nil
			}}
			protocol := test.protocol
			if protocol == "" {
				protocol = "tcp"
			}
			result, err := inspector.Inspect(context.Background(), firewall.Socket{Address: "192.0.2.1", Protocol: protocol, Port: 8443})
			if err != nil || result.Status != test.want {
				t.Fatalf("result=%+v err=%v; want status %s", result, err, test.want)
			}
		})
	}
}

func TestPolicyZoneSetsMustBeUnambiguous(t *testing.T) {
	data, err := os.ReadFile("testdata/allow-host-ipv6.txt")
	if err != nil {
		t.Fatal(err)
	}
	policy := strings.TrimSpace(string(data))
	for _, zones := range []string{"", "ANY clients", "HOST clients", "clients clients", "*", strings.Repeat("a", 129)} {
		for _, direction := range []string{"ingress-zones", "egress-zones"} {
			t.Run(direction+"/"+zones, func(t *testing.T) {
				ingress, egress := "ANY", "HOST"
				if direction == "ingress-zones" {
					ingress = zones
				} else {
					egress = zones
				}
				active := "allow-host-ipv6\n  ingress-zones: " + ingress + "\n  egress-zones: " + egress
				if _, valid := activePolicies(active); valid {
					t.Fatal("ambiguous active policy zones accepted", active)
				}
				mutated := strings.Replace(policy, "ingress-zones: ANY", "ingress-zones: "+ingress, 1)
				mutated = strings.Replace(mutated, "egress-zones: HOST", "egress-zones: "+egress, 1)
				if harmlessPolicy(mutated, "allow-host-ipv6") {
					t.Fatal("ambiguous policy zones accepted", mutated)
				}
			})
		}
	}
}
