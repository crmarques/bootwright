package installation

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/substrate"
)

const (
	firewallHostKey  = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	firewallApplyOp  = "op-0123456789abcdef0123456789abcdef"
	firewallVIPCIDR  = "198.51.100.60/32"
	firewallPublish  = "ceph-ingress-rgw"
	firewallInstall  = "os-install-rhel-01"
	firewallMachine  = "rhel-01"
	firewallEnabled  = true
	firewallDisabled = false
)

func firewallProfile(enabled bool) api.Object {
	return installProfile(field("customizations", api.MapValue(
		field("localization", api.MapValue(text("language", "en_US.UTF-8"), text("keyboard", "us"), text("timezone", "Etc/UTC"))),
		field("packages", api.MapValue(text("environment", "minimal"), field("install", api.StringList("chrony", "firewalld")))),
		field("services", api.MapValue(field("enabled", api.StringList("chronyd", "firewalld")))),
		field("security", api.MapValue(field("selinux", api.MapValue(text("mode", "enforcing"))), field("firewall", api.MapValue(field("enabled", api.BoolValue(enabled)))))),
	)))
}

func ingressRequirement(machine string) HostPortRequirement {
	return HostPortRequirement{Machine: machine, Publisher: firewallPublish, Networks: []string{firewallVIPCIDR},
		Protocols: []string{"vrrp"}, Ports: []HostPort{{Number: 443, Transport: "tcp"}, {Number: 29443, Transport: "tcp"}}}
}

func firewallInput(catalog api.Catalog, verb reconciliation.Verb) lifecycle.PlanInput {
	return lifecycle.PlanInput{Verb: verb, State: compilation.NewState(catalog, catalog, nil), Controller: "controller", Context: lifecycle.ContextIdentity{Name: testContext}}
}

func staticPorts(requirements ...HostPortRequirement) HostPortRequirements {
	return func(api.Catalog) ([]HostPortRequirement, error) { return requirements, nil }
}

func firewallPlan(t *testing.T, catalog api.Catalog, ports HostPortRequirements) lifecycle.CapabilityPlan {
	t.Helper()
	plan, err := NewFirewall(nil).WithHostPorts(ports).Plan(context.Background(), firewallInput(catalog, reconciliation.Apply))
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	return plan
}

func TestAContextWithoutHostPortRequirementsPlansNoFirewallBlock(t *testing.T) {
	catalog := labCatalog(firewallProfile(firewallEnabled))
	for name, ports := range map[string]HostPortRequirements{"unbound": nil, "empty": staticPorts()} {
		if plan := firewallPlan(t, catalog, ports); len(plan.Definitions) != 0 || len(plan.Secrets) != 0 {
			t.Fatalf("%s: plan = %+v", name, plan)
		}
	}
}

func TestOnlyAnInstalledMachineWithItsFirewallEnabledRealizesHostPorts(t *testing.T) {
	enabled := labCatalog(firewallProfile(firewallEnabled))
	for name, test := range map[string]struct {
		catalog api.Catalog
		machine string
		want    bool
	}{
		"enabled":  {enabled, firewallMachine, true},
		"disabled": {labCatalog(firewallProfile(firewallDisabled)), firewallMachine, false},
		"absent":   {labCatalog(), firewallMachine, false},
		"provided": {labCatalog(firewallProfile(firewallEnabled), guest(field("os", api.MapValue(field("provided", api.BoolValue(true)))))), firewallMachine, false},
		"unknown":  {enabled, "metal-09", false},
	} {
		if got := RealizesHostPorts(test.catalog, test.machine); got != test.want {
			t.Errorf("%s: realizes = %v", name, got)
		}
		if plan := firewallPlan(t, test.catalog, staticPorts(ingressRequirement(test.machine))); (len(plan.Definitions) == 1) != test.want {
			t.Errorf("%s: plan = %+v", name, plan.Definitions)
		}
	}
}

func TestTheFirewallBlockWaitsForTheInstallationAndFreezesOneServicePerPublisher(t *testing.T) {
	other := ingressRequirement(firewallMachine)
	other.Publisher, other.Protocols, other.Ports = "ceph-ingress-nfs", nil, []HostPort{{Number: 2049, Transport: "tcp"}}
	plan := firewallPlan(t, labCatalog(firewallProfile(firewallEnabled)), staticPorts(ingressRequirement(firewallMachine), other))
	if len(plan.Definitions) != 1 {
		t.Fatalf("definitions = %+v", plan.Definitions)
	}
	definition := plan.Definitions[0]
	if definition.ID != "os-firewall-rhel-01" || definition.Kind != Kind || definition.Object != firewallMachine || definition.Stage != reconciliation.StageMachines ||
		definition.Implementation != FirewallImplementation || definition.ContentDigest != FirewallContentDigest() {
		t.Fatalf("definition = %+v", definition)
	}
	if want := []reconciliation.ObjectRef{{Kind: Kind, Object: firewallMachine, Family: InstallFamily}}; !slices.Equal(definition.Requires, want) {
		t.Fatalf("requires = %v", definition.Requires)
	}
	request, err := DecodeFirewallRequest(definition.Request)
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	install, _ := onlyRequest(t, labCatalog(firewallProfile(firewallEnabled)))
	if request.Address != install.Address || request.User != install.User || request.FleetKeyRef != install.FleetKeyRef || request.Placement != install.Placement {
		t.Fatalf("connection = %+v", request)
	}
	if len(request.Services) != 2 || request.Services[0].Publisher != "ceph-ingress-nfs" || request.Services[1].Name != "bootwright-lab-ceph-ingress-rgw" ||
		!slices.Equal(request.Services[1].Protocols, []string{"vrrp"}) || len(request.Services[1].Ports) != 2 {
		t.Fatalf("services = %+v", request.Services)
	}
	if !slices.Contains(plan.Secrets, install.FleetKeyRef) {
		t.Fatalf("secrets = %v", plan.Secrets)
	}
}

func TestTheFirewallOrdersBetweenTheInstallationAndItsConsumer(t *testing.T) {
	catalog := labCatalog(firewallProfile(firewallEnabled))
	install, err := New(nil).WithMedia(labMedia()).Plan(context.Background(), firewallInput(catalog, reconciliation.Apply))
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	firewall := firewallPlan(t, catalog, staticPorts(ingressRequirement(firewallMachine)))
	definitions := realizing(slices.DeleteFunc(slices.Clone(install.Definitions[0].Requires), func(reference reconciliation.ObjectRef) bool { return reference.Kind == Kind }))
	definitions = append(definitions, reconciliation.BlockDefinition{ID: "machine-rhel-01", Description: "realize rhel-01", Stage: reconciliation.StageMachines,
		Kind: Kind, Object: firewallMachine, Implementation: "placeholder", Family: substrate.MachineFamily, ContentDigest: strings.Repeat("a", 64), Request: json.RawMessage(`{"machine":1}`)})
	definitions = append(definitions, reconciliation.BlockDefinition{ID: "storage-consumer", Description: "consume rhel-01", Stage: reconciliation.StageClusters,
		Requires: []reconciliation.ObjectRef{{Kind: Kind, Object: firewallMachine}}, Kind: "CephCluster", Object: "ceph", Implementation: "placeholder",
		ContentDigest: strings.Repeat("b", 64), Request: json.RawMessage(`{"cluster":1}`)})
	definitions = append(definitions, install.Definitions[0], firewall.Definitions[0])
	plan, err := reconciliation.NewPlan(reconciliation.Apply, definitions)
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	dependencies := map[string][]string{}
	for _, block := range plan.Blocks {
		dependencies[block.ID] = block.Dependencies
	}
	if slices.Contains(dependencies[firewallInstall], "os-firewall-rhel-01") || !slices.Equal(dependencies["os-firewall-rhel-01"], []string{firewallInstall}) {
		t.Fatalf("dependencies = %v", dependencies)
	}
	if !slices.Contains(dependencies["storage-consumer"], "os-firewall-rhel-01") || !slices.Contains(dependencies["storage-consumer"], firewallInstall) {
		t.Fatalf("consumer dependencies = %v", dependencies["storage-consumer"])
	}
	inverse, err := plan.Inverse()
	if err != nil {
		t.Fatal(err)
	}
	order := []string{}
	for _, block := range inverse.Blocks {
		order = append(order, block.ID)
	}
	if slices.Index(order, "storage-consumer") > slices.Index(order, "os-firewall-rhel-01") || slices.Index(order, "os-firewall-rhel-01") > slices.Index(order, firewallInstall) ||
		slices.Index(order, firewallInstall) > slices.Index(order, "machine-rhel-01") {
		t.Fatalf("removal order = %v", order)
	}
}

func TestAFirewallServiceNameStaysBoundedForTheLongestNames(t *testing.T) {
	longContext := strings.Repeat("c", 63)
	longPublisher := strings.Repeat("p", 63)
	names := map[string]bool{}
	for _, publisher := range []string{longPublisher, longPublisher[:62] + "q", "ceph.ingress.rgw", firewallPublish} {
		for _, contextName := range []string{longContext, testContext} {
			name := FirewallServiceName(contextName, publisher)
			if len(name) > 63 || !firewallName.MatchString(name) {
				t.Fatalf("%s/%s: name %q", contextName, publisher, name)
			}
			names[name] = true
		}
	}
	if len(names) != 8 {
		t.Fatalf("names = %v", names)
	}
	if FirewallServiceName(testContext, firewallPublish) != "bootwright-lab-ceph-ingress-rgw" {
		t.Fatal(FirewallServiceName(testContext, firewallPublish))
	}
	if FirewallServiceName(testContext, "ceph.ingress.rgw") == FirewallServiceName(testContext, "ceph-ingress-rgw") {
		t.Fatal("a dotted publisher collides with its dashed spelling")
	}
}

func TestAnInvalidRequirementRefusesBeforeAnyBlock(t *testing.T) {
	catalog := labCatalog(firewallProfile(firewallEnabled))
	for name, change := range map[string]func(*HostPortRequirement){
		"no network":    func(r *HostPortRequirement) { r.Networks = nil },
		"host bits":     func(r *HostPortRequirement) { r.Networks = []string{"198.51.100.7/24"} },
		"subnet first":  func(r *HostPortRequirement) { r.Networks = []string{"198.51.100.0/24", firewallVIPCIDR} },
		"v6 subnet":     func(r *HostPortRequirement) { r.Networks = []string{"2001:db8::/64"} },
		"no admission":  func(r *HostPortRequirement) { r.Ports, r.Protocols = nil, nil },
		"bad transport": func(r *HostPortRequirement) { r.Ports = []HostPort{{Number: 443, Transport: "sctp"}} },
		"port zero":     func(r *HostPortRequirement) { r.Ports = []HostPort{{Number: 0, Transport: "tcp"}} },
		"bad protocol":  func(r *HostPortRequirement) { r.Protocols = []string{"VRRP"} },
		"no publisher":  func(r *HostPortRequirement) { r.Publisher = "" },
	} {
		requirement := ingressRequirement(firewallMachine)
		change(&requirement)
		if _, err := NewFirewall(nil).WithHostPorts(staticPorts(requirement)).Plan(context.Background(), firewallInput(catalog, reconciliation.Apply)); err == nil {
			t.Errorf("%s: planned", name)
		}
	}
	if _, err := NewFirewall(nil).WithHostPorts(staticPorts(ingressRequirement(firewallMachine), ingressRequirement(firewallMachine))).Plan(context.Background(), firewallInput(catalog, reconciliation.Apply)); err == nil {
		t.Error("a repeated publisher planned")
	}
	for networks, admitted := range map[string]bool{
		firewallVIPCIDR: true, firewallVIPCIDR + " 198.51.100.0/24": true, "2001:db8::60/128 2001:db8::/64": true,
		"198.51.100.0/24": false, "198.51.100.0/24 " + firewallVIPCIDR: false, "2001:db8::/64": false,
	} {
		service := FirewallService{Name: "bootwright-lab-p", Networks: strings.Fields(networks), Protocols: []string{"vrrp"}, Publisher: "p"}
		if err := validService(service); (err == nil) != admitted {
			t.Errorf("%s: admitted = %v", networks, err == nil)
		}
	}
}

func TestTheFirewallClaimsEachMachineItPlansABlockFor(t *testing.T) {
	catalog := labCatalog(firewallProfile(firewallEnabled))
	if claimed := NewFirewall(nil).Claimed(catalog); claimed != nil {
		t.Fatalf("unbound claims = %v", claimed)
	}
	capability := NewFirewall(nil).WithHostPorts(staticPorts(ingressRequirement(firewallMachine), ingressRequirement("metal-09")))
	if claimed := capability.Claimed(catalog); !slices.Equal(claimed, []string{"Machine/" + firewallMachine}) {
		t.Fatalf("claims = %v", claimed)
	}
	if claimed := capability.Claimed(labCatalog(firewallProfile(firewallDisabled))); len(claimed) != 0 {
		t.Fatalf("a disabled firewall claimed %v", claimed)
	}
}

func firewallBlock(t *testing.T) reconciliation.Block {
	t.Helper()
	plan := firewallPlan(t, labCatalog(firewallProfile(firewallEnabled)), staticPorts(ingressRequirement(firewallMachine)))
	if _, err := reconciliation.NewPlan(reconciliation.Apply, plan.Definitions[:1:1]); err == nil {
		t.Fatal("a firewall block without its installation was accepted")
	}
	definition := plan.Definitions[0]
	definition.Requires = nil
	frozen, err := reconciliation.NewPlan(reconciliation.Apply, []reconciliation.BlockDefinition{definition})
	if err != nil {
		t.Fatal(err)
	}
	return frozen.Blocks[0]
}

func installationProof(t *testing.T) lifecycle.BlockEvidence {
	t.Helper()
	request := pinnedRequest(t, labCatalog(firewallProfile(firewallEnabled)))
	canonical, err := request.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	digest := strings.Repeat("1", 64)
	marker, err := MarkerFor(request, digest)
	if err != nil {
		t.Fatal(err)
	}
	evidence, _ := json.Marshal(Evidence{Address: request.Address, HostKey: firewallHostKey, Image: true, Marker: string(marker), Postcondition: true, Power: "On", Reachable: true,
		Request: digest, Tree: true, TreeContent: true, TreeIdentity: dvdDigest, Network: true, NetworkReboot: true, NetworkDifferences: []string{}, Update: UpdateEvidence{State: updateNone}})
	block := reconciliation.Block{BlockDefinition: reconciliation.BlockDefinition{ID: firewallInstall, Kind: Kind, Object: firewallMachine, Implementation: Implementation, Request: canonical}, RequestDigest: digest}
	return lifecycle.BlockEvidence{Block: block, Kind: Kind, Object: firewallMachine, Implementation: Implementation, Verb: reconciliation.Apply, State: reconciliation.BlockDone, Evidence: evidence}
}

func firewallExecution(t *testing.T, block reconciliation.Block) lifecycle.Execution {
	t.Helper()
	return lifecycle.Execution{Context: testContext, Operation: firewallApplyOp, ApplyOperation: firewallApplyOp, Attempt: 1, Block: block,
		ApplyProved: []lifecycle.BlockEvidence{installationProof(t)}}
}

func firewallServiceEvidence(t *testing.T, block reconciliation.Block, state string) FirewallEvidence {
	t.Helper()
	request, err := DecodeFirewallRequest(block.Request)
	if err != nil {
		t.Fatal(err)
	}
	_, digests, err := firewallContents(request, FirewallMark(testContext, block.ID, firewallApplyOp))
	if err != nil {
		t.Fatal(err)
	}
	evidence := FirewallEvidence{Request: block.RequestDigest, Version: FirewallEvidenceVersion}
	for _, service := range request.Services {
		entry := FirewallServiceEvidence{Name: service.Name, State: firewallAbsent, Memberships: []FirewallMembership{}}
		switch state {
		case "complete":
			entry = FirewallServiceEvidence{Name: service.Name, State: firewallOwned, Digest: digests[service.Name], Covered: true, Memberships: []FirewallMembership{{Interface: "enp1s0", Zone: "public"}}}
		case "written":
			entry = FirewallServiceEvidence{Name: service.Name, State: firewallOwned, Digest: digests[service.Name], Memberships: []FirewallMembership{}}
		case "drifted":
			entry = FirewallServiceEvidence{Name: service.Name, State: firewallOwned, Digest: strings.Repeat("e", 64), Covered: true, Memberships: []FirewallMembership{{Interface: "enp1s0", Zone: "public"}}}
		case "foreign":
			entry = FirewallServiceEvidence{Name: service.Name, State: "foreign", Digest: strings.Repeat("e", 64), Memberships: []FirewallMembership{}}
		case "orphaned":
			entry.Memberships = []FirewallMembership{{Zone: "public"}}
		}
		evidence.Services = append(evidence.Services, entry)
	}
	return evidence
}

func TestFirewallEvidenceReadsEachObservationForItsVerb(t *testing.T) {
	block := firewallBlock(t)
	request, _ := DecodeFirewallRequest(block.Request)
	_, digests, _ := firewallContents(request, FirewallMark(testContext, block.ID, firewallApplyOp))
	for state, want := range map[string][2]reconciliation.EffectState{
		"complete": {reconciliation.EffectCompleted, reconciliation.EffectNoEffect},
		"absent":   {reconciliation.EffectNoEffect, reconciliation.EffectCompleted},
		"written":  {reconciliation.EffectPartial, reconciliation.EffectPartial},
		"drifted":  {reconciliation.EffectPartial, reconciliation.EffectPartial},
		"foreign":  {reconciliation.EffectUnknown, reconciliation.EffectUnknown},
		"orphaned": {reconciliation.EffectUnknown, reconciliation.EffectUnknown},
	} {
		evidence := firewallServiceEvidence(t, block, state)
		if got := firewallEffect(evidence, request, digests, false); got != want[0] {
			t.Errorf("%s apply: %s", state, got)
		}
		if got := firewallEffect(evidence, request, digests, true); got != want[1] {
			t.Errorf("%s removal: %s", state, got)
		}
	}
	other := firewallServiceEvidence(t, block, "complete")
	if _, err := decodeFirewallEvidence(mustJSON(t, other), strings.Repeat("9", 64)); err == nil {
		t.Error("evidence of another request was read")
	}
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestAFirewallApplyHandsTheGuestItsServicesAndProvesThem(t *testing.T) {
	block := firewallBlock(t)
	complete := firewallServiceEvidence(t, block, "complete")
	complete.Postcondition = true
	runner := &fakeRunner{result: lifecycle.RunResult{Outcome: "changed", Evidence: mustJSON(t, complete)}}
	result, err := NewFirewall(runner).Apply(context.Background(), firewallExecution(t, block))
	if err != nil || result.Outcome != reconciliation.OutcomeChanged {
		t.Fatalf("result = %+v, %v", result, diagnostics.Of(err))
	}
	sent := runner.requests[0]
	if sent.Implementation != FirewallImplementation || sent.Operation != "apply" || sent.Variable != firewallVariable {
		t.Fatalf("request = %+v", sent)
	}
	mark := FirewallMark(testContext, block.ID, firewallApplyOp)
	if sent.MaterialValues["mark"] != mark || sent.MaterialValues["hostKey"] != firewallHostKey {
		t.Fatalf("values = %v", sent.MaterialValues)
	}
	var services []struct {
		Content  string   `json:"content"`
		Name     string   `json:"name"`
		Networks []string `json:"networks"`
	}
	if err := json.Unmarshal([]byte(sent.MaterialValues["services"]), &services); err != nil || len(services) != 1 {
		t.Fatalf("services = %s", sent.MaterialValues["services"])
	}
	want := "<?xml version=\"1.0\" encoding=\"utf-8\"?>\n<service>\n  <short>bootwright-lab-ceph-ingress-rgw</short>\n  <description>" + mark +
		"</description>\n  <port protocol=\"tcp\" port=\"443\"/>\n  <port protocol=\"tcp\" port=\"29443\"/>\n  <protocol value=\"vrrp\"/>\n</service>\n"
	if services[0].Content != want || !slices.Equal(services[0].Networks, []string{firewallVIPCIDR}) {
		t.Fatalf("content = %q", services[0].Content)
	}
	for _, reason := range []string{"host-firewall-foreign", "host-firewall-no-interface", "host-firewall-inactive", "host-firewall-failed"} {
		if sent.Refusals[reason] == nil {
			t.Errorf("no refusal for %s", reason)
		}
	}
	for state := range map[string]bool{"written": true, "drifted": true, "absent": true} {
		evidence := firewallServiceEvidence(t, block, state)
		evidence.Postcondition = true
		runner := &fakeRunner{result: lifecycle.RunResult{Outcome: "changed", Evidence: mustJSON(t, evidence)}}
		if result, err := NewFirewall(runner).Apply(context.Background(), firewallExecution(t, block)); err == nil || result.Outcome != reconciliation.OutcomeUnknown {
			t.Errorf("%s: apply accepted %+v", state, result)
		}
	}
}

func TestAFirewallRefusesWithoutTheInstallationsProvedHostKey(t *testing.T) {
	block := firewallBlock(t)
	execution := firewallExecution(t, block)
	execution.ApplyProved = nil
	runner := &fakeRunner{}
	if _, err := NewFirewall(runner).Apply(context.Background(), execution); err == nil || len(runner.requests) != 0 {
		t.Fatalf("applied without the installation proof: %v", err)
	}
	execution = firewallExecution(t, block)
	execution.ApplyOperation = ""
	if _, err := NewFirewall(runner).Apply(context.Background(), execution); err == nil || len(runner.requests) != 0 {
		t.Fatal("applied without an apply operation to mark the services with")
	}
}

func TestAFirewallObservationAndRemovalReadTheGuest(t *testing.T) {
	block := firewallBlock(t)
	for state, want := range map[string]reconciliation.EffectState{"complete": reconciliation.EffectCompleted, "written": reconciliation.EffectPartial, "absent": reconciliation.EffectNoEffect} {
		runner := &fakeRunner{result: lifecycle.RunResult{Outcome: "unchanged", Evidence: mustJSON(t, firewallServiceEvidence(t, block, state))}}
		observation, err := NewFirewall(runner).Observe(context.Background(), firewallExecution(t, block))
		if err != nil || observation.Effect != want || runner.requests[0].Operation != "observe" {
			t.Errorf("%s: observation = %+v, %v", state, observation, err)
		}
	}
	removed := firewallServiceEvidence(t, block, "absent")
	removed.Absent, removed.Postcondition = true, true
	runner := &fakeRunner{result: lifecycle.RunResult{Outcome: "changed", Evidence: mustJSON(t, removed)}}
	result, err := NewFirewall(runner).Destroy(context.Background(), firewallExecution(t, block))
	if err != nil || result.Outcome != reconciliation.OutcomeChanged || runner.requests[0].Operation != "destroy" {
		t.Fatalf("destroy = %+v, %v", result, diagnostics.Of(err))
	}
	left := firewallServiceEvidence(t, block, "written")
	left.Absent, left.Postcondition = true, true
	runner = &fakeRunner{result: lifecycle.RunResult{Outcome: "changed", Evidence: mustJSON(t, left)}}
	if result, err := NewFirewall(runner).Destroy(context.Background(), firewallExecution(t, block)); err == nil || result.Outcome != reconciliation.OutcomeUnknown {
		t.Fatalf("a destroy that left an owned service completed: %+v", result)
	}
}

func TestAFirewallRetiredWithItsMachineContactsNoGuest(t *testing.T) {
	block := firewallBlock(t)
	execution := firewallExecution(t, block)
	execution.RetiredWithHosts = true
	runner := &fakeRunner{}
	capability := NewFirewall(runner)
	result, err := capability.Destroy(context.Background(), execution)
	if err != nil || result.Outcome != reconciliation.OutcomeChanged {
		t.Fatalf("retire = %+v, %v", result, err)
	}
	observation, err := capability.ObserveRemoval(context.Background(), execution)
	if err != nil || observation.Effect != reconciliation.EffectCompleted || len(runner.requests) != 0 {
		t.Fatalf("observation = %+v, %v, runs %d", observation, err, len(runner.requests))
	}
	quiescence, err := capability.Quiescent(context.Background(), lifecycle.Probe{Block: block, RetiredWithHosts: true})
	if err != nil || !quiescence.Settled() {
		t.Fatalf("quiescence = %+v", quiescence)
	}
	hosts, err := capability.HostMachines(block)
	if err != nil || !slices.Equal(hosts, []string{firewallMachine}) || !capability.RequiresOnlineRemoval(block) {
		t.Fatalf("hosts = %v, %v", hosts, err)
	}
}

func TestAFirewallRefusalNamesTheServiceItsDetailNames(t *testing.T) {
	block := firewallBlock(t)
	request, _ := DecodeFirewallRequest(block.Request)
	refusals := firewallRefusals(testContext, request, false)
	type detailed interface{ WithDetail(string) error }
	for reason, want := range map[string]string{
		"host-firewall-foreign":      "holds a firewalld service bootwright-lab-ceph-ingress-rgw this context did not write",
		"host-firewall-no-interface": "has no interface whose network holds " + firewallVIPCIDR,
	} {
		message := diagnostics.Of(refusals[reason].(detailed).WithDetail("bootwright-lab-ceph-ingress-rgw"))[0].Message
		if !strings.Contains(message, want) {
			t.Errorf("%s: %s", reason, message)
		}
		if unknown := diagnostics.Of(refusals[reason].(detailed).WithDetail("other"))[0].Message; strings.Contains(unknown, "bootwright-lab") {
			t.Errorf("%s: an unknown detail named a service: %s", reason, unknown)
		}
	}
	if message := diagnostics.Of(refusals["host-firewall-failed"].(detailed).WithDetail("$(rm)"))[0].Message; !strings.HasSuffix(message, ": unknown") {
		t.Errorf("failed: %s", message)
	}
}

func TestAFirewallRemovalRefusalNamesDestroyAsItsRemedy(t *testing.T) {
	block := firewallBlock(t)
	request, _ := DecodeFirewallRequest(block.Request)
	type detailed interface{ WithDetail(string) error }
	for removal, verb := range map[bool]string{false: "bootwright apply --context ", true: "bootwright destroy --context "} {
		refusals := firewallRefusals(testContext, request, removal)
		for _, reason := range []string{"host-firewall-foreign", "host-firewall-inactive", "host-firewall-failed"} {
			refused := refusals[reason]
			if with, ok := refused.(detailed); ok {
				refused = with.WithDetail("bootwright-lab-ceph-ingress-rgw")
			}
			if remedy := diagnostics.Of(refused)[0].Remediation; !strings.HasSuffix(remedy, "then run "+verb+testContext+" again") {
				t.Errorf("removal %v %s: %s", removal, reason, remedy)
			}
		}
	}
	runner := &fakeRunner{result: lifecycle.RunResult{Outcome: "unchanged", Evidence: mustJSON(t, firewallServiceEvidence(t, block, "absent"))}}
	if _, err := NewFirewall(runner).ObserveRemoval(context.Background(), firewallExecution(t, block)); err != nil {
		t.Fatal(err)
	}
	if remedy := diagnostics.Of(runner.requests[0].Refusals["host-firewall-inactive"])[0].Remediation; !strings.Contains(remedy, "bootwright destroy") {
		t.Errorf("observing a removal names %s", remedy)
	}
}

func TestTheFirewallMarkSpellsItsContextBlockAndApplyOperation(t *testing.T) {
	if mark := FirewallMark("context", FirewallBlockID("machine"), "operation"); mark != "bootwright-host-firewall context=context block=os-firewall-machine apply=operation" {
		t.Fatalf("mark = %q", mark)
	}
}
