package ceph

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

type recordingRunner struct {
	calls   int
	request lifecycle.RunRequest
	result  lifecycle.RunResult
}

func (r *recordingRunner) Run(_ context.Context, request lifecycle.RunRequest) (lifecycle.RunResult, error) {
	r.calls++
	r.request = request
	return r.result, nil
}

func coreRequest() Request {
	r := Request{Version: Version, ClusterSSHTransport: "native", Controller: "controller", Action: "cluster", Cluster: "example", Image: PinnedRepository + ":" + PinnedTag, Bootstrap: "node-1", ClusterKeyRef: "cluster-key", RegistryRef: "registry", Registry: PinnedRegistry}
	for i, name := range []string{"node-1", "node-2", "node-3"} {
		r.Hosts = append(r.Hosts, Host{Name: name, Machine: name, Address: fmt.Sprintf("192.0.2.%d", i+1), User: "operator", Port: 22, SSHKeyRef: "fleet-key", HostKeyRef: name + "-host-key", Devices: []Device{{Path: "/dev/vdb", Serial: name, SizeBytes: 1 << 30}}})
	}
	r.Services = []json.RawMessage{json.RawMessage(`{"placement":{"hosts":["node-1","node-2","node-3"]},"service_id":"data","service_type":"osd","spec":{"data_devices":{"paths":["/dev/vdb"]}}}`), json.RawMessage(`{"placement":{"hosts":["node-1","node-2","node-3"]},"service_type":"mon"}`), json.RawMessage(`{"placement":{"count":2,"hosts":["node-1","node-2","node-3"]},"service_type":"mgr"}`)}
	return r
}
func executionFor(t *testing.T, r Request) lifecycle.Execution {
	t.Helper()
	data, err := r.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	return lifecycle.Execution{ApplyOperation: "apply-original", Operation: "apply-original", Context: "example", Block: reconciliation.Block{BlockDefinition: reconciliation.BlockDefinition{ID: "ceph-cluster-example", Kind: string(api.StorageCluster), Object: "example", Request: data}, RequestDigest: strings.Repeat("b", 64)},
		ApplyProved: []lifecycle.BlockEvidence{imageProof(t, r.Cluster, r.Image, resolvedDigest)}}
}

func pinnedCatalog(t *testing.T, change func(api.Value) api.Value) api.Catalog {
	t.Helper()
	catalog := packageCatalog()
	cluster, _ := catalog.Find(api.StorageCluster, "example")
	spec := cluster.Spec().WithPath(api.StringValue(PinnedTransport), "ceph", "cephadm", "clusterSSH", "transport")
	if change != nil {
		spec = change(spec)
	}
	objects := catalog.Objects()
	for i := range objects {
		if objects[i].Identity() == cluster.Identity() {
			objects[i] = cluster.WithSpec(spec)
		}
	}
	objects = append(objects,
		api.NewObject(api.StoragePool, "volumes", api.Value{}, requestMap("clusterRef", "example", "type", "replicated", "role", "rbd", "replicated", requestMap("size", 2, "minSize", 2))),
		api.NewObject(api.StorageClient, "consumer", api.Value{}, requestMap("clusterRef", "example", "poolRef", "volumes", "access", "read-write")))
	return api.NewCatalog(objects)
}

func TestUnsupportedReleaseOrTransportRefusesBeforeRegistration(t *testing.T) {
	set := func(value string, path ...string) func(api.Value) api.Value {
		return func(spec api.Value) api.Value { return spec.WithPath(api.StringValue(value), path...) }
	}
	refused := map[string]func(api.Value) api.Value{
		"native transport": set("native", "ceph", "cephadm", "clusterSSH", "transport"),
		"omitted transport": func(spec api.Value) api.Value {
			return spec.WithPath(requestMap("user", "operator", "keyRef", "cluster-key"), "ceph", "cephadm", "clusterSSH")
		},
		"empty release":     set("", "ceph", "release"),
		"control character": set("bad\nversion", "ceph", "packageVersion"),
	}
	for name, change := range refused {
		t.Run(name, func(t *testing.T) {
			catalog := pinnedCatalog(t, change)
			state := compilation.NewState(catalog, catalog, nil)
			for _, kind := range []api.Kind{api.StorageCluster, api.StoragePool, api.StorageClient} {
				runner := &recordingRunner{}
				refusals := New(kind, runner).Unsupported(state)
				if len(refusals) != 1 || refusals[0].Reason != unsupportedReason || refusals[0].Remediation != unsupportedRemedy || runner.calls != 0 {
					t.Fatalf("%s: missing pre-registration refusal: %+v calls=%d", kind, refusals, runner.calls)
				}
			}
		})
	}
	for _, want := range []string{PinnedTransport, "native transport is refused", "host-key verification cannot be disabled"} {
		if !strings.Contains(unsupportedRemedy, want) {
			t.Fatalf("pinned remedy omits %q: %s", want, unsupportedRemedy)
		}
	}
	for name, version := range map[string]string{"pinned tag": PinnedTag, "digest": resolvedDigest} {
		t.Run(name, func(t *testing.T) {
			catalog := pinnedCatalog(t, set(version, "ceph", "image", "version"))
			state := compilation.NewState(catalog, catalog, nil)
			for _, kind := range []api.Kind{api.StorageCluster, api.StoragePool, api.StorageClient} {
				if refusals := New(kind, nil).Unsupported(state); len(refusals) != 0 {
					t.Fatalf("%s: pinned input refused: %+v", kind, refusals)
				}
				object := catalog.OfKind(kind)[0]
				r, err := New(kind, nil).request(catalog, object, "synthetic")
				if err != nil || r.ClusterSSHTransport != PinnedTransport {
					t.Fatalf("%s: pinned request refused: %v", kind, err)
				}
				want := PinnedRepository + ":" + PinnedTag
				if version != PinnedTag {
					want = PinnedRepository + "@" + version
				}
				if r.Image != want {
					t.Fatalf("%s: frozen image %q, want the authored %q", kind, r.Image, want)
				}
			}
		})
	}
}

func TestIdentityUsesOriginalApplyAcrossRetryRemovalAndRename(t *testing.T) {
	capability := New(api.StorageCluster, nil)
	first := executionFor(t, coreRequest())
	a, err := capability.prepare(context.Background(), first)
	if err != nil {
		t.Fatal(err)
	}
	next := first
	next.Operation = "destroy-later"
	next.Context = "renamed"
	next.Attempt = 4
	b, err := capability.prepare(context.Background(), next)
	if err != nil {
		t.Fatal(err)
	}
	if a.FSID != b.FSID || a.Owner != b.Owner || a.Owner != "apply-original" {
		t.Fatal("ownership changed across operation")
	}
	next.ApplyOperation = "apply-replacement"
	c, err := capability.prepare(context.Background(), next)
	if err != nil {
		t.Fatal(err)
	}
	if c.FSID == a.FSID {
		t.Fatal("replacement reused a prior cluster FSID")
	}
	next.ApplyOperation = ""
	if _, err := capability.prepare(context.Background(), next); err == nil {
		t.Fatal("missing original apply accepted")
	}
	authored := coreRequest()
	authored.Owner = "injected"
	if _, err := capability.prepare(context.Background(), executionFor(t, authored)); err == nil {
		t.Fatal("authored runtime ownership accepted")
	}
}

func TestRemovalRequiresOnlineHostsAndDataLoss(t *testing.T) {
	capability := New(api.StorageCluster, nil)
	e := executionFor(t, coreRequest())
	removal, err := capability.Removal(context.Background(), e.Block)
	if err != nil {
		t.Fatal(err)
	}
	if !capability.RequiresOnlineRemoval(e.Block) || len(removal.Consumes) != 1 || removal.Consumes[0] != reconciliation.AuthorizationDataLoss {
		t.Fatalf("unsafe removal contract: %+v", removal)
	}
}

func TestMissingPrincipalNeverProvesOldSessionsQuiescent(t *testing.T) {
	for _, kind := range []api.Kind{api.StorageCluster, api.StoragePool, api.StorageClient} {
		runner := &recordingRunner{}
		q, err := New(kind, runner).Quiescent(context.Background(), lifecycle.Probe{})
		if err != nil || q.State != lifecycle.Unproved || q.Reason != survivingReason || runner.calls != 0 {
			t.Fatalf("%s inferred quiescence without a session proof: %+v %v", kind, q, err)
		}
	}
}

func policyPlanInput(t *testing.T) lifecycle.PlanInput {
	t.Helper()
	objects := []api.Object{childOf(api.StoragePlacementPolicy, "fast", "ruleName", "fast-hosts", "crushDeviceClass", "ssd", "replicated", requestMap("size", 2, "minSize", 2))}
	for _, object := range pinnedCatalog(t, nil).Objects() {
		if object.Kind() == api.StoragePool {
			object = object.WithSpec(object.Spec().With("placementPolicyRef", api.StringValue("fast")))
		}
		objects = append(objects, object)
	}
	catalog := api.NewCatalog(objects)
	return lifecycle.PlanInput{Verb: reconciliation.Apply, Context: lifecycle.ContextIdentity{Name: "synthetic"}, State: compilation.NewState(catalog, catalog, nil), Controller: "controller"}
}

func TestPolicyExecutionMatchesItsFrozenIdentity(t *testing.T) {
	capability := New(api.StoragePlacementPolicy, nil)
	plan, err := capability.Plan(context.Background(), policyPlanInput(t))
	if err != nil || len(plan.Definitions) != 1 {
		t.Fatalf("the policy planned %+v (%v)", plan.Definitions, err)
	}
	r, err := DecodeRequest(plan.Definitions[0].Request)
	if err != nil {
		t.Fatal(err)
	}
	e := executionFor(t, r)
	e.Block.BlockDefinition = plan.Definitions[0]
	prepared, err := capability.prepare(context.Background(), e)
	if err != nil || prepared.Action != "policy" || prepared.Policy == nil || prepared.Policy.Name != "fast" || prepared.Owner != "apply-original" {
		t.Fatalf("a planned policy block did not match its frozen identity: %+v %v", prepared.Policy, err)
	}
	e.Block.Object = "example"
	if _, err := capability.prepare(context.Background(), e); len(diagnostics.Of(err)) != 1 || diagnostics.Of(err)[0].Code != "lifecycle.state" {
		t.Fatalf("a policy block named after its cluster matched the policy identity: %v", err)
	}
}

func TestPolicyBlockRequiresClusterAndPoolRequiresPolicy(t *testing.T) {
	input := policyPlanInput(t)
	definitions := map[string]reconciliation.BlockDefinition{}
	all := []reconciliation.BlockDefinition{}
	for _, capability := range []lifecycle.Capability{NewImage(nil), NewHost(nil), New(api.StorageCluster, nil), New(api.StoragePlacementPolicy, nil), New(api.StoragePool, nil), New(api.StorageClient, nil)} {
		plan, err := capability.Plan(context.Background(), input)
		if err != nil {
			t.Fatal(err)
		}
		for _, definition := range plan.Definitions {
			definitions[definition.ID] = definition
		}
		all = append(all, plan.Definitions...)
	}
	policy, pool := definitions["ceph-policy-fast"], definitions["ceph-pool-volumes"]
	if policy.Kind != string(api.StoragePlacementPolicy) || policy.Object != "fast" || len(policy.Requires) != 0 || !slices.Equal(policy.Dependencies, []string{ClusterBlockID("example")}) {
		t.Fatalf("the policy block is %+v", policy)
	}
	if !slices.Contains(pool.Requires, reconciliation.ObjectRef{Kind: string(api.StoragePlacementPolicy), Object: "fast"}) || !slices.Contains(pool.Requires, reconciliation.ObjectRef{Kind: string(api.StorageCluster), Object: "example"}) {
		t.Fatalf("the pool block requires %+v", pool.Requires)
	}
	for _, kind := range []string{"Machine", "MachineInstallProfile"} {
		for _, name := range []string{"node-1", "node-2", "node-3", "rhel"} {
			all = append(all, reconciliation.BlockDefinition{ID: strings.ToLower(kind) + "-" + name, Description: "synthetic " + name, Stage: reconciliation.StageMachines, Kind: kind, Object: name, Implementation: "synthetic", ContentDigest: strings.Repeat("f", 64), Request: json.RawMessage(`{}`)})
		}
	}
	plan, err := reconciliation.NewPlan(reconciliation.Apply, all)
	if err != nil {
		t.Fatal(err)
	}
	found := 0
	for _, block := range plan.Blocks {
		if block.ID == "ceph-pool-volumes" && slices.Contains(block.Dependencies, "ceph-policy-fast") {
			found++
		}
		if block.ID == "ceph-policy-fast" && slices.Equal(block.Dependencies, []string{"ceph-cluster-example"}) {
			found++
		}
	}
	if found != 2 {
		t.Fatal("the planned pool does not depend on its policy block, or the policy block depends on more than its cluster block")
	}
}
