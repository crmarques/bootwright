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
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/substrate"
)

const adminCanary = "admin-kubeconfig-canary"

func hostedProvider() api.Object {
	return api.NewObject(api.InfraProvider, "lab-kubevirt", api.Value{}, api.MapValue(
		field("kubevirt", api.MapValue(text("hostClusterRef", "host"), text("namespace", "guests"),
			field("machineProfiles", api.ListValue(api.MapValue(text("name", "node"), number("cpu", "8"), number("memoryMiB", "16384"), number("diskGiB", "120")))))),
		field("segments", api.ListValue(
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
			lifecycle.SecretClassifier
			lifecycle.ProducedInputConsumer
		} = NewHost(nil, hosting)
		if block.Implementation == MachineImplementation {
			capability = NewMachine(nil)
		}
		classes, _ := capability.SecretClasses(block)
		access := classes.Named(secrets.AccessCredential)
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
	if len(refused) != 1 || !strings.Contains(refused[0].Reason, "segment storage names another network") {
		t.Fatalf("an undeclared network: %+v", refused)
	}
}

func TestAHostedProviderRefusesAHostWhoseKubeVirtAddOnIsAuthored(t *testing.T) {
	state := compilation.NewState(nestedCatalog(), nestedCatalog(), nil)
	authored := func(_ api.Catalog, cluster string) []string {
		if cluster != "host" {
			t.Fatalf("asked about %q", cluster)
		}
		return []string{"widgets"}
	}
	unserved := func(api.Catalog, string) ([]reconciliation.ObjectRef, bool) { return nil, false }
	refused := NewHost(nil, unserved).WithAuthoredProviders(authored).Unsupported(state)
	if len(refused) != 1 || refused[0].Name != "lab-kubevirt" || !strings.Contains(refused[0].Reason, "ClusterAddon/widgets that provides kubevirt does not") ||
		!strings.Contains(refused[0].Reason, "built-in kubevirt package") || !strings.Contains(refused[0].Remediation, "bind the kubevirt package to OpenShiftCluster/host") {
		t.Fatalf("an authored kubevirt add-on: %+v", refused)
	}
	if refused := NewHost(nil, hosting).WithAuthoredProviders(authored).Unsupported(state); len(refused) != 0 {
		t.Fatalf("a built-in kubevirt package beside an authored one was refused: %+v", refused)
	}
	external := labCatalog()
	if refused := NewHost(nil, unserved).WithAuthoredProviders(authored).Unsupported(compilation.NewState(external, external, nil)); len(refused) != 0 {
		t.Fatalf("an external host was refused: %+v", refused)
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

// lendingRunner answers a lend as the access role does, a revoke with its
// proof, and any other run as an observation that holds; fail names the
// operations it fails, and cancelled the context of each run.
type lendingRunner struct {
	runs      []lifecycle.RunRequest
	cancelled []bool
	granted   int64
	lost      bool
	fail      map[string]bool
	refuse    map[string]bool
}

func (r *lendingRunner) Run(ctx context.Context, run lifecycle.RunRequest) (lifecycle.RunResult, error) {
	r.runs = append(r.runs, run)
	r.cancelled = append(r.cancelled, ctx.Err() != nil)
	if r.fail[run.Operation] {
		return lifecycle.RunResult{}, refusal("lifecycle.state", "the host did not answer the "+run.Operation, "")
	}
	if r.refuse[run.Operation] {
		return lifecycle.RunResult{NotStarted: true}, refusal("lifecycle.state", "the runner refused the "+run.Operation+" before its job", "")
	}
	var request LendRequest
	_ = json.Unmarshal(run.Canonical, &request)
	granted := request.ExpirationSeconds
	if r.granted != 0 {
		granted = r.granted
	}
	if run.Operation == "revoke" {
		granted = 0
	}
	evidence, _ := json.Marshal(LendEvidence{ExpirationSeconds: granted, Postcondition: true, Request: run.Digest, Secret: request.Secret, Server: "https://api.host.example.test:6443", Version: lendEvidence})
	result := lifecycle.RunResult{Outcome: "unchanged", Evidence: evidence}
	if !r.lost && run.Operation == "lend" {
		result.Produced = []lifecycle.Produced{{Name: "kubeconfig", Material: secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: []byte("token-kubeconfig")})}}
	}
	return result, nil
}

func (r *lendingRunner) operations() []string {
	var operations []string
	for _, run := range r.runs {
		operations = append(operations, run.Operation)
	}
	return operations
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
	var warned []operationstore.Warning
	runner := &lendingRunner{}
	execution := lendExecution(&lent)
	execution.Warn = func(_ context.Context, warnings []operationstore.Warning) { warned = append(warned, warnings...) }
	material, back, err := NewLender(runner).LendHostAccess(context.Background(), execution, "host", "guests", hostRequest(t).Placement, 20*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if value, _ := material.Part(secrets.ValuePart); string(value) != "token-kubeconfig" {
		t.Fatalf("lent %q", value)
	}
	run := runner.runs[0]
	var request LendRequest
	if err := json.Unmarshal(run.Canonical, &request); err != nil || !strings.HasPrefix(request.Secret, lendSecretName) || len(request.Secret) != len(lendSecretName)+16 {
		t.Fatalf("request %+v, %v", request, err)
	}
	if want := (LendRequest{Cluster: "host", ExpirationSeconds: 1800, Namespace: "guests", Secret: request.Secret, ServiceAccount: "bootwright-kubevirt", Version: lendVersion}); request != want {
		t.Fatalf("request %+v", request)
	}
	if run.Operation != "lend" || len(run.Outputs) != 1 || len(run.Materials) != 1 || run.Materials[0].Secret != adminKubeconfig || strings.Contains(string(run.Canonical), adminCanary) {
		t.Fatalf("the lend run %+v", run)
	}
	if value, _ := lent[0].Part(secrets.ValuePart); strings.Contains(string(value), adminCanary) {
		t.Fatalf("the administrator kubeconfig outlived its lend: %q", value)
	}
	if len(runner.runs) != 1 {
		t.Fatalf("the token was revoked before its run returned: %v", runner.operations())
	}
	back()
	back()
	if !slices.Equal(runner.operations(), []string{"lend", "revoke"}) || len(warned) != 0 {
		t.Fatalf("runs %v, warned %+v", runner.operations(), warned)
	}
	revoke := runner.runs[1]
	if string(revoke.Canonical) != string(run.Canonical) || len(revoke.Outputs) != 0 || len(revoke.Materials) != 1 || revoke.Materials[0].Secret != adminKubeconfig ||
		revoke.Implementation != HostImplementation || revoke.Deadline != revokeDeadline {
		t.Fatalf("the revoke run %+v", revoke)
	}
	if value, _ := material.Part(secrets.ValuePart); strings.Contains(string(value), "token") {
		t.Fatal("the lent kubeconfig outlived its return")
	}
	if len(lent) != 2 {
		t.Fatalf("the lend and its revoke borrowed %d administrator kubeconfigs", len(lent))
	}
	for _, borrowed := range lent {
		if value, _ := borrowed.Part(secrets.ValuePart); strings.Contains(string(value), adminCanary) {
			t.Fatal("the administrator kubeconfig outlived its revoke")
		}
	}
}

// A lend that fails once it reached the host revokes before it answers, since
// its Secret may exist; one that never reached it, outside custody, runs
// nothing.
func TestALendThatGrantsTooLittleOrLeavesNothingLendsNothingAndRevokes(t *testing.T) {
	for name, runner := range map[string]*lendingRunner{"short": {granted: 900}, "lost": {lost: true}, "unanswered": {fail: map[string]bool{"lend": true}}} {
		var lent []secrets.Material
		if _, back, err := NewLender(runner).LendHostAccess(context.Background(), lendExecution(&lent), "host", "guests", hostRequest(t).Placement, 20*time.Minute); err == nil {
			t.Fatalf("%s: a lend was accepted", name)
		} else {
			back()
		}
		if !slices.Equal(runner.operations(), []string{"lend", "revoke"}) || string(runner.runs[0].Canonical) != string(runner.runs[1].Canonical) {
			t.Fatalf("%s: runs %v", name, runner.operations())
		}
	}
	none := &lendingRunner{}
	if _, _, err := NewLender(none).LendHostAccess(context.Background(), lifecycle.Execution{}, "host", "guests", hostRequest(t).Placement, time.Minute); err == nil || len(none.runs) != 0 {
		t.Fatalf("a lend outside an operation's custody was accepted or ran %v", none.operations())
	}
	var warned []operationstore.Warning
	refused := &lendingRunner{refuse: map[string]bool{"lend": true}}
	var lent []secrets.Material
	execution := lendExecution(&lent)
	execution.Warn = func(_ context.Context, warnings []operationstore.Warning) { warned = append(warned, warnings...) }
	if _, back, err := NewLender(refused).LendHostAccess(context.Background(), execution, "host", "guests", hostRequest(t).Placement, time.Minute); err == nil {
		t.Fatal("a lend the runner refused was accepted")
	} else {
		back()
	}
	if !slices.Equal(refused.operations(), []string{"lend"}) || len(warned) != 0 {
		t.Fatalf("a lend refused before its job started ran %v and warned %+v", refused.operations(), warned)
	}
}

// Only a recorded attempt that may change its host sweeps leftover lend
// Secrets; a lend of a read-only or unrecorded execution creates its own
// Secret alone.
func TestOnlyARecordedMutatingLendSweeps(t *testing.T) {
	cases := map[string]struct {
		attempt, resolution int
		readOnly, sweep     bool
	}{
		"apply":      {attempt: 1, sweep: true},
		"read-only":  {attempt: 1, readOnly: true},
		"unrecorded": {},
		"resolution": {attempt: 1, resolution: 1},
	}
	for name, c := range cases {
		var lent []secrets.Material
		runner := &lendingRunner{}
		execution := lendExecution(&lent)
		execution.Attempt, execution.Resolution, execution.ReadOnly = c.attempt, c.resolution, c.readOnly
		_, back, err := NewLender(runner).LendHostAccess(context.Background(), execution, "host", "guests", hostRequest(t).Placement, time.Minute)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		back()
		var request LendRequest
		if err := json.Unmarshal(runner.runs[0].Canonical, &request); err != nil || request.Sweep != c.sweep {
			t.Fatalf("%s: the lend request sweeps %v (%v), want %v", name, request.Sweep, err, c.sweep)
		}
	}
}

// The return revokes under its own bound when the run it was lent to was
// cancelled, and each lend binds its token to a Secret of its own.
func TestACancelledRunStillRevokesItsToken(t *testing.T) {
	var lent []secrets.Material
	runner := &lendingRunner{}
	ctx, cancel := context.WithCancel(context.Background())
	_, back, err := NewLender(runner).LendHostAccess(ctx, lendExecution(&lent), "host", "guests", hostRequest(t).Placement, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	_, again, err := NewLender(runner).LendHostAccess(ctx, lendExecution(&lent), "host", "guests", hostRequest(t).Placement, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	back()
	again()
	if !slices.Equal(runner.operations(), []string{"lend", "lend", "revoke", "revoke"}) || slices.Contains(runner.cancelled, true) {
		t.Fatalf("runs %v, cancelled %v", runner.operations(), runner.cancelled)
	}
	if string(runner.runs[0].Canonical) == string(runner.runs[1].Canonical) || string(runner.runs[2].Canonical) != string(runner.runs[0].Canonical) {
		t.Fatal("two lends shared one per-lend Secret, or a return revoked another lend's")
	}
}

// A revoke that fails leaves the run's result alone and reports the Secret
// left behind and the expiry that still bounds its token.
func TestARevokeThatFailsWarnsAndFailsNothing(t *testing.T) {
	var lent []secrets.Material
	var warned []operationstore.Warning
	runner := &lendingRunner{fail: map[string]bool{"revoke": true}}
	execution := lendExecution(&lent)
	execution.Warn = func(_ context.Context, warnings []operationstore.Warning) { warned = append(warned, warnings...) }
	_, back, err := NewLender(runner).LendHostAccess(context.Background(), execution, "host", "guests", hostRequest(t).Placement, 20*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	back()
	var request LendRequest
	_ = json.Unmarshal(runner.runs[0].Canonical, &request)
	if len(warned) != 1 || warned[0].Code != LendNotRevoked || !strings.Contains(warned[0].Message, "guests/"+request.Secret) || !strings.Contains(warned[0].Message, "30m0s") {
		t.Fatalf("warned %+v", warned)
	}
}

// A capability's run that fails still returns its access, so its token is
// revoked as on success.
func TestAFailedHostedRunRevokesItsToken(t *testing.T) {
	var lent []secrets.Material
	runner := &lendingRunner{fail: map[string]bool{"observe": true}}
	execution := lendExecution(&lent)
	execution.Block = nestedBlock(t, func() HostRequest { r, _ := HostRequests(nestedCatalog(), "controller", testContext); return r[0] }())
	if _, err := NewHost(runner, hosting).Observe(context.Background(), execution); err == nil {
		t.Fatal("a failed observation was accepted")
	}
	if !slices.Equal(runner.operations(), []string{"lend", "observe", "revoke"}) {
		t.Fatalf("runs %v", runner.operations())
	}
}

func TestEachHostedRunIsLentAFreshTokenAndNeverTheSecret(t *testing.T) {
	var lent []secrets.Material
	runner := &lendingRunner{}
	execution := lendExecution(&lent)
	execution.Block = nestedBlock(t, func() HostRequest { r, _ := HostRequests(nestedCatalog(), "controller", testContext); return r[0] }())
	_, _ = NewHost(runner, hosting).Observe(context.Background(), execution)
	_, _ = NewHost(runner, hosting).Observe(context.Background(), execution)
	if !slices.Equal(runner.operations(), []string{"lend", "observe", "revoke", "lend", "observe", "revoke"}) {
		t.Fatalf("runs %v", runner.operations())
	}
	for _, run := range []lifecycle.RunRequest{runner.runs[1], runner.runs[4]} {
		if len(run.Materials) != 1 || run.Materials[0].Secret != lentKubeconfig || run.Materials[0].Variable != "kubeconfig" {
			t.Fatalf("the proof was lent %+v", run.Materials)
		}
		if _, held := run.Material[adminKubeconfig]; held {
			t.Fatal("the proof was lent the administrator kubeconfig")
		}
	}
}

// A lent token outlives the run it is lent to by the minimum the API server
// admits, however long that run is: an installation's deadline grows with its
// nodes and no ceiling bounds it (B926).
func TestExpiryCoversTheRunAndTheAPIServersMinimum(t *testing.T) {
	for deadline, want := range map[time.Duration]int64{0: 600, 20 * time.Minute: 1800, 8 * time.Hour: 8*3600 + 600} {
		if got := expiresIn(deadline); got != want {
			t.Fatalf("expiresIn(%s) = %d, want %d", deadline, got, want)
		}
	}
}

func TestTheProofsRemovalReadsNothingAndMintsNoToken(t *testing.T) {
	hosted, _ := HostRequests(nestedCatalog(), "controller", testContext)
	for name, block := range map[string]reconciliation.Block{"hosted": nestedBlock(t, hosted[0]), "external": hostBlock(t)} {
		var lent []secrets.Material
		released := encoded(t, HostEvidence{Absent: true, Failures: []string{}, Networks: []NetworkEvidence{}, Postcondition: true, Request: block.RequestDigest})
		run := &runner{result: lifecycle.RunResult{Outcome: "unchanged", Evidence: released}}
		execution := lendExecution(&lent)
		execution.Block = block
		result, err := NewHost(run, hosting).Destroy(context.Background(), execution)
		if err != nil || result.Outcome != reconciliation.OutcomeUnchanged || string(result.Evidence) != string(released) {
			t.Fatalf("%s: destroy = %+v, %v", name, result, err)
		}
		if len(lent) != 0 || len(run.requests) != 1 || run.requests[0].Operation != "destroy" || len(run.requests[0].Materials) != 0 || len(run.requests[0].Material) != 0 {
			t.Fatalf("%s: the removal borrowed %d kubeconfigs and ran %+v", name, len(lent), run.requests)
		}
	}
}

func TestOnlyAHostedVirtualMachineIsRemovedWhileItsHostAnswers(t *testing.T) {
	hosted, err := MachineRequests(nestedCatalog(), "controller", testContext)
	if err != nil || len(hosted) != 1 {
		t.Fatal(hosted, err)
	}
	if !NewMachine(nil).RequiresOnlineRemoval(nestedBlock(t, hosted[0])) {
		t.Fatal("a VirtualMachine on a host this context builds is not an online removal")
	}
	if NewMachine(nil).RequiresOnlineRemoval(machineBlock(t)) {
		t.Fatal("a VirtualMachine on an external host defers its host's Machines")
	}
}
