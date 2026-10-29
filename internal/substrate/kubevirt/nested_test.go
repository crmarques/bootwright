package kubevirt

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/substrate"
)

const adminCanary = "admin-kubeconfig-canary"

func hostedProvider() api.Object {
	return api.NewObject(api.InfraProvider, "lab-kubevirt", api.Value{}, api.MapValue(
		field("kubevirt", api.MapValue(text("hostClusterRef", "host"), text("namespace", "guests"),
			field("machineProfiles", api.ListValue(api.MapValue(text("name", "node"), number("cpu", "8"), number("memoryMiB", "16384"), number("diskGiB", "120")))))),
		field("networkAttachments", api.ListValue(
			api.MapValue(text("name", "lab-guests"), field("kubevirt", api.MapValue(field("networkRef", api.MapValue(
				text("apiGroup", "k8s.ovn.org"), text("kind", "ClusterUserDefinedNetwork"), text("name", "guest-l2")))))),
			api.MapValue(text("name", "storage"), field("kubevirt", api.MapValue(field("networkRef", api.MapValue(
				text("apiGroup", "k8s.ovn.org"), text("kind", "ClusterUserDefinedNetwork"), text("name", "storage-net")))))),
		)),
	))
}

func hostCluster(networks ...api.Value) api.Object {
	return api.NewObject(api.OpenShiftCluster, "host", api.Value{}, api.MapValue(field("networking", api.MapValue(field("secondaryNetworks", api.ListValue(networks...))))))
}

func secondary(name string, labels ...api.FieldValue) api.Value {
	return api.MapValue(text("name", name), field("namespaceSelector", api.MapValue(field("matchLabels", api.MapValue(labels...)))))
}

func childCluster(roles ...string) api.Object {
	var nodes []api.Value
	for index, role := range roles {
		nodes = append(nodes, api.MapValue(text("name", "node-"+string(rune('0'+index))), text("role", role), text("machineRef", "master-"+string(rune('0'+index)))))
	}
	return api.NewObject(api.OpenShiftCluster, "child", api.Value{}, api.MapValue(field("nodes", api.ListValue(nodes...))))
}

func nestedCatalog(objects ...api.Object) api.Catalog {
	return labCatalog(append([]api.Object{hostedProvider(), hostCluster(secondary("guest-l2", text("tier", "hub")), secondary("storage-net", text("net", "storage")))}, objects...)...)
}

func hosting(api.Catalog, string) ([]reconciliation.ObjectRef, bool) {
	return []reconciliation.ObjectRef{{Kind: "ClusterAddonBinding", Object: "host.kubevirt"}, {Kind: "OpenShiftClusterNetwork", Object: "host"}}, true
}

func TestAHostedProviderNamesItsHostAndAccountAndNoSecret(t *testing.T) {
	catalog := nestedCatalog()
	hosts, err := HostRequests(catalog, "controller", testContext)
	if err != nil || len(hosts) != 1 {
		t.Fatal(hosts, err)
	}
	machines, err := MachineRequests(catalog, "controller", testContext)
	if err != nil || len(machines) != 1 {
		t.Fatal(machines, err)
	}
	if hosts[0].HostCluster != "host" || hosts[0].KubeconfigRef != "" || hosts[0].ServiceAccount != substrate.HostAccount || len(hosts[0].SecretReferences()) != 0 ||
		machines[0].HostCluster != "host" || machines[0].ServiceAccount != substrate.HostAccount || len(machines[0].SecretReferences()) != 0 {
		t.Fatalf("host %+v, machine %+v", hosts[0], machines[0])
	}
	for _, block := range []reconciliation.Block{nestedBlock(t, hosts[0]), nestedBlock(t, machines[0])} {
		var capability interface {
			lifecycle.AccessCredentialReader
			lifecycle.ProducedInputConsumer
		} = NewHost(nil, hosting)
		if block.Implementation == MachineImplementation {
			capability = NewMachine(nil)
		}
		access, _ := capability.AccessCredentials(block)
		inputs, err := capability.ProducedInputs(block)
		if err != nil || len(access) != 0 || !slices.Equal(inputs, []lifecycle.ProducedInput{{Kind: "OpenShiftCluster", Object: "host", Output: "kubeconfig"}}) {
			t.Fatalf("%s: access %v, inputs %v, %v", block.Implementation, access, inputs, err)
		}
	}
}

func nestedBlock(t *testing.T, request interface{ Canonical() ([]byte, error) }) reconciliation.Block {
	t.Helper()
	canonical, err := request.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	if host, ok := request.(HostRequest); ok {
		return reconciliation.Block{BlockDefinition: reconciliation.BlockDefinition{ID: HostBlockID("lab-kubevirt"), Kind: HostKind, Object: host.Identity.Object,
			Implementation: HostImplementation, Request: canonical}, RequestDigest: strings.Repeat("a", 64)}
	}
	return reconciliation.Block{BlockDefinition: reconciliation.BlockDefinition{ID: MachineBlockID("master-0"), Kind: MachineKind, Object: "master-0",
		Implementation: MachineImplementation, Request: canonical}, RequestDigest: strings.Repeat("b", 64)}
}

func TestAHostedProofWaitsForItsHostsInstallationAddOnsAndNetworks(t *testing.T) {
	plan, err := NewHost(nil, hosting).Plan(context.Background(), planInput(nestedCatalog(), reconciliation.Apply))
	if err != nil || len(plan.Definitions) != 1 || len(plan.Secrets) != 0 {
		t.Fatal(plan, err)
	}
	want := []reconciliation.ObjectRef{{Kind: "OpenShiftCluster", Object: "host"}, {Kind: "ClusterAddonBinding", Object: "host.kubevirt"}, {Kind: "OpenShiftClusterNetwork", Object: "host"}}
	if !slices.Equal(plan.Definitions[0].Requires, want) {
		t.Fatalf("requires %v", plan.Definitions[0].Requires)
	}
	external, _ := NewHost(nil, hosting).Plan(context.Background(), planInput(labCatalog(), reconciliation.Apply))
	if len(external.Definitions[0].Requires) != 0 {
		t.Fatalf("an external host's proof waits for %v", external.Definitions[0].Requires)
	}
}

func TestAHostedProviderRefusesAHostWithoutKubeVirtAndANetworkItDoesNotDeclare(t *testing.T) {
	state := compilation.NewState(nestedCatalog(), nestedCatalog(), nil)
	if refused := NewHost(nil, hosting).Unsupported(state); len(refused) != 0 {
		t.Fatalf("a served host refused: %+v", refused)
	}
	refused := NewHost(nil, func(api.Catalog, string) ([]reconciliation.ObjectRef, bool) { return nil, false }).Unsupported(state)
	if len(refused) != 1 || !strings.Contains(refused[0].Reason, "provides kubevirt") {
		t.Fatalf("a host without kubevirt: %+v", refused)
	}
	bare := labCatalog(hostedProvider(), hostCluster(secondary("guest-l2", text("tier", "hub"))))
	refused = NewHost(nil, hosting).Unsupported(compilation.NewState(bare, bare, nil))
	if len(refused) != 1 || !strings.Contains(refused[0].Reason, "attachment storage names another network") {
		t.Fatalf("an undeclared network: %+v", refused)
	}
}

func TestTheNodesOfOneRoleShareASpreadAndOtherRolesDoNot(t *testing.T) {
	catalog := api.NewCatalog(append(nestedCatalog().Objects(), node("master-1"), node("master-2"), childCluster("controlPlane", "controlPlane", "worker")))
	requests, err := MachineRequests(catalog, "controller", testContext)
	if err != nil || len(requests) != 3 {
		t.Fatal(requests, err)
	}
	if requests[0].Spread == "" || len(requests[0].Spread) != 16 || requests[0].Spread != requests[1].Spread || requests[2].Spread == requests[0].Spread || requests[2].Spread == "" {
		t.Fatalf("spreads %q %q %q", requests[0].Spread, requests[1].Spread, requests[2].Spread)
	}
	if alone := machineRequest(t); alone.Spread != "" {
		t.Fatalf("a Machine no cluster names spreads as %q", alone.Spread)
	}
}

type lendingRunner struct {
	runs    []lifecycle.RunRequest
	granted int64
	lost    bool
}

func (r *lendingRunner) Run(_ context.Context, run lifecycle.RunRequest) (lifecycle.RunResult, error) {
	r.runs = append(r.runs, run)
	var request LendRequest
	_ = json.Unmarshal(run.Canonical, &request)
	granted := request.ExpirationSeconds
	if r.granted != 0 {
		granted = r.granted
	}
	evidence, _ := json.Marshal(LendEvidence{ExpirationSeconds: granted, Postcondition: true, Request: run.Digest, Server: "https://api.host.example.test:6443", Version: lendEvidence})
	result := lifecycle.RunResult{Outcome: "unchanged", Evidence: evidence}
	if !r.lost {
		result.Produced = []lifecycle.Produced{{Name: "kubeconfig", Material: secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: []byte("token-kubeconfig")})}}
	}
	return result, nil
}

func lendExecution(lent *[]secrets.Material) lifecycle.Execution {
	return lifecycle.Execution{Block: nestedBlockDefinition(), LendProduced: func(_ context.Context, input lifecycle.ProducedInput) (secrets.Material, error) {
		material := secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: []byte(adminCanary + input.Object)})
		*lent = append(*lent, material)
		return material, nil
	}}
}

func nestedBlockDefinition() reconciliation.Block {
	return reconciliation.Block{BlockDefinition: reconciliation.BlockDefinition{ID: MachineBlockID("master-0")}, RequestDigest: strings.Repeat("b", 64)}
}

func TestALendHandsTheAdministratorKubeconfigToItsOwnRunAloneAndOutlivesTheConsumer(t *testing.T) {
	var lent []secrets.Material
	runner := &lendingRunner{}
	material, err := NewLender(runner).LendHostAccess(context.Background(), lendExecution(&lent), "host", "guests", hostRequest(t).Placement, 20*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if value, _ := material.Part(secrets.ValuePart); string(value) != "token-kubeconfig" {
		t.Fatalf("lent %q", value)
	}
	run := runner.runs[0]
	var request LendRequest
	if err := json.Unmarshal(run.Canonical, &request); err != nil || request != (LendRequest{Cluster: "host", ExpirationSeconds: 1800, Namespace: "guests", ServiceAccount: "bootwright-kubevirt", Version: lendVersion}) {
		t.Fatalf("request %+v, %v", request, err)
	}
	if run.Operation != "lend" || len(run.Outputs) != 1 || len(run.Materials) != 1 || run.Materials[0].Secret != adminKubeconfig || strings.Contains(string(run.Canonical), adminCanary) {
		t.Fatalf("the lend run %+v", run)
	}
	if value, _ := lent[0].Part(secrets.ValuePart); strings.Contains(string(value), adminCanary) {
		t.Fatalf("the administrator kubeconfig outlived its lend: %q", value)
	}
}

func TestALendThatGrantsTooLittleOrLeavesNothingLendsNothing(t *testing.T) {
	for name, runner := range map[string]*lendingRunner{"short": {granted: 900}, "lost": {lost: true}} {
		var lent []secrets.Material
		if _, err := NewLender(runner).LendHostAccess(context.Background(), lendExecution(&lent), "host", "guests", hostRequest(t).Placement, 20*time.Minute); err == nil {
			t.Fatalf("%s: a lend was accepted", name)
		}
	}
	if _, err := NewLender(&lendingRunner{}).LendHostAccess(context.Background(), lifecycle.Execution{}, "host", "guests", hostRequest(t).Placement, time.Minute); err == nil {
		t.Fatal("a lend outside an operation's custody was accepted")
	}
}

func TestEachHostedRunIsLentAFreshTokenAndNeverTheSecret(t *testing.T) {
	var lent []secrets.Material
	runner := &lendingRunner{}
	execution := lendExecution(&lent)
	execution.Block = nestedBlock(t, func() HostRequest { r, _ := HostRequests(nestedCatalog(), "controller", testContext); return r[0] }())
	_, _ = NewHost(runner, hosting).Observe(context.Background(), execution)
	_, _ = NewHost(runner, hosting).Observe(context.Background(), execution)
	if len(runner.runs) != 4 || runner.runs[0].Operation != "lend" || runner.runs[2].Operation != "lend" {
		t.Fatalf("runs %v", runner.runs)
	}
	for _, run := range []lifecycle.RunRequest{runner.runs[1], runner.runs[3]} {
		if len(run.Materials) != 1 || run.Materials[0].Secret != lentKubeconfig || run.Materials[0].Variable != "kubeconfig" {
			t.Fatalf("the proof was lent %+v", run.Materials)
		}
		if _, held := run.Material[adminKubeconfig]; held {
			t.Fatal("the proof was lent the administrator kubeconfig")
		}
	}
}

func TestExpiryCoversTheRunAndStaysWithinTheAPIServersBounds(t *testing.T) {
	for deadline, want := range map[time.Duration]int64{0: 600, 20 * time.Minute: 1800, 7 * time.Hour: maxLendExpiresIn} {
		if got := expiresIn(deadline); got != want {
			t.Fatalf("expiresIn(%s) = %d, want %d", deadline, got, want)
		}
	}
}
