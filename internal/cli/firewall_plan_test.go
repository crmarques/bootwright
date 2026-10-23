package cli

import "github.com/crmarques/bootwright/internal/reconciliation/lifecycle"

func firewallPlanFixture() *lifecycle.PlanResult {
	return &lifecycle.PlanResult{
		Context: lifecycle.ContextIdentity{Name: "synthetic"}, Verb: "apply",
		Checks: []lifecycle.PlanCheck{
			{ID: "listener-firewall", Status: "open", Summary: "DNSServer/resolver 192.0.2.1:53/udp: open (interface synthetic0, zone public); explicit runtime port"},
			{ID: "listener-firewall", Status: "closed", Summary: "ArtifactServer/media 192.0.2.1:8443/tcp: closed (interface synthetic0, zone public); runtime port missing", Remedy: "configure the operator-owned firewall and repeat preflight"},
			{ID: "listener-firewall", Status: "unknown", Summary: "Proxy/egress 192.0.2.1:3128/tcp: unknown (interface synthetic0, zone public); unqualified rules", Remedy: "verify runtime firewall admission manually"},
		},
		Receipt: lifecycle.Receipt{Operation: "none", Verb: "plan", State: "preview", Next: "apply"},
	}
}
