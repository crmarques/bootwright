package ceph

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/crmarques/bootwright/ansible"
	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/storage"
)

const resourcesModule = "collections/ansible_collections/bootwright/core/plugins/module_utils/ceph_native_resources.py"

func cephDefinitions(t *testing.T, catalog api.Catalog) []reconciliation.BlockDefinition {
	t.Helper()
	input := lifecycle.PlanInput{Verb: reconciliation.Apply, Context: lifecycle.ContextIdentity{Name: "synthetic"}, State: compilation.NewState(catalog, catalog, nil), Controller: "controller"}
	runner := &recordingRunner{}
	var definitions []reconciliation.BlockDefinition
	capabilities := []lifecycle.Capability{NewImage(runner), NewHost(runner)}
	for _, kind := range []api.Kind{api.StorageCluster, api.StoragePlacementPolicy, api.StoragePool, api.StorageClient, api.StorageRBDImage, api.StorageFilesystem, api.StorageSubvolume} {
		capabilities = append(capabilities, New(kind, runner))
	}
	for _, capability := range capabilities {
		plan, err := capability.Plan(context.Background(), input)
		if err != nil {
			t.Fatal(diagnostics.Of(err))
		}
		definitions = append(definitions, plan.Definitions...)
	}
	if runner.calls != 0 {
		t.Fatal("planning reached the runner")
	}
	return definitions
}

func cephPlan(t *testing.T, catalog api.Catalog) reconciliation.Plan {
	t.Helper()
	definitions := cephDefinitions(t, catalog)
	seen := map[reconciliation.ObjectRef]bool{}
	for _, definition := range slices.Clone(definitions) {
		for _, ref := range definition.Requires {
			if (ref.Kind == string(api.Machine) || ref.Kind == "MachineInstallProfile") && !seen[ref] {
				seen[ref] = true
				definitions = append(definitions, reconciliation.BlockDefinition{ID: strings.ToLower(ref.Kind) + "-" + ref.Object, Description: "synthetic " + ref.Object, Stage: reconciliation.StageMachines, Kind: ref.Kind, Object: ref.Object, Implementation: "synthetic", ContentDigest: strings.Repeat("f", 64), Request: json.RawMessage(`{}`)})
			}
		}
	}
	plan, err := reconciliation.NewPlan(reconciliation.Apply, definitions)
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	return plan
}

func cephIDs(plan reconciliation.Plan) []string {
	ids := []string{}
	for _, block := range plan.Blocks {
		if strings.HasPrefix(block.ID, "ceph-") {
			ids = append(ids, block.ID)
		}
	}
	return ids
}

func blockOf(t *testing.T, plan reconciliation.Plan, id string) reconciliation.Block {
	t.Helper()
	for _, block := range plan.Blocks {
		if block.ID == id {
			return block
		}
	}
	t.Fatalf("the plan has no block %s: %v", id, cephIDs(plan))
	return reconciliation.Block{}
}

func settingsCatalog(t *testing.T, services []api.Value, extra ...api.Object) api.Catalog {
	t.Helper()
	return api.NewCatalog(append(pinnedCatalog(t, func(spec api.Value) api.Value {
		return spec.WithPath(api.ListValue(append(spec.Get("ceph", "services").Items(), services...)...), "ceph", "services")
	}).Objects(), extra...))
}

func settingsDefinitions(definitions []reconciliation.BlockDefinition) []reconciliation.BlockDefinition {
	out := []reconciliation.BlockDefinition{}
	for _, definition := range definitions {
		if strings.HasPrefix(definition.ID, "ceph-settings-") {
			out = append(out, definition)
		}
	}
	return out
}

func frozenAuxiliaryReferences() []AuxiliaryReference {
	return []AuxiliaryReference{
		{Name: "haproxy", Image: PinnedRegistry + "/synthetic/haproxy@sha256:" + strings.Repeat("a", 64)},
		{Name: "keepalived", Image: PinnedRegistry + "/synthetic/keepalived@sha256:" + strings.Repeat("b", 64)},
	}
}

func TestSettingsBlockEmittedOnlyWhenNeeded(t *testing.T) {
	if found := settingsDefinitions(cephDefinitions(t, settingsCatalog(t, nil))); len(found) != 0 {
		t.Fatalf("a cluster without a derived module or an auxiliary image plans %+v", found)
	}
	found := settingsDefinitions(cephDefinitions(t, settingsCatalog(t, []api.Value{nfsService("files")})))
	if len(found) != 1 {
		t.Fatalf("a cluster with an nfs service plans %d settings blocks", len(found))
	}
	settings := found[0]
	if settings.ID != "ceph-settings-example" || settings.Kind != string(api.StorageCluster) || settings.Object != "example" || settings.Implementation != Implementation ||
		settings.Stage != reconciliation.StageClusters || !slices.Equal(settings.Exclusive, []string{"ceph/example"}) || len(settings.Requires) != 0 ||
		!slices.Equal(settings.Dependencies, []string{"ceph-cluster-example", "ceph-image-example"}) || settings.ContentDigest != ContentDigest() ||
		!slices.Equal(settings.Impacts, []string{"realize Ceph settings example"}) {
		t.Fatalf("the settings block is %+v", settings)
	}
	r, err := DecodeRequest(settings.Request)
	if err != nil || r.Action != "settings" || r.Settings == nil || !slices.Equal(r.Settings.Modules, []string{"nfs"}) || len(r.Auxiliary) != 0 {
		t.Fatalf("the settings request is %+v (%v)", r, err)
	}
	catalog := delegatedCatalog(nil, nil)
	cluster, _ := catalog.Find(api.StorageCluster, "example")
	base := Request{Version: Version, Action: "cluster", Cluster: "example", Auxiliary: frozenAuxiliaryReferences()}
	r, needed := settingsRequest(base, cluster)
	if !needed || r.Action != "settings" || r.Settings == nil || r.Settings.Modules == nil || len(r.Settings.Modules) != 0 || len(r.Auxiliary) != 2 {
		t.Fatalf("frozen auxiliary images without a derived module make %+v (%v)", r, needed)
	}
	if data, err := json.Marshal(r.Settings); err != nil || string(data) != `{"modules":[]}` {
		t.Fatalf("the settings member encodes %s (%v)", data, err)
	}
	if found := settingsDefinitions(cephDefinitions(t, settingsCatalog(t, []api.Value{rgwService("objects"), ingressService("rgw.objects")}))); len(found) != 0 {
		t.Fatalf("an ingress without a frozen auxiliary image plans %+v", found)
	}
}

func TestSettingsBlockIsTheOnlyWriterOfAuxiliaryImageKeys(t *testing.T) {
	const prefix = "collections/ansible_collections/bootwright/core/"
	holders := []string{}
	for path, data := range ansible.Assets() {
		if !strings.HasPrefix(path, prefix+"plugins/") && !strings.HasPrefix(path, prefix+"roles/") {
			continue
		}
		for range strings.Count(string(data), "mgr/cephadm/container_image_") {
			holders = append(holders, path)
		}
	}
	if !slices.Equal(holders, []string{resourcesModule}) {
		t.Fatalf("the cephadm image setting prefix is spelled in %q, want only the settings worker", holders)
	}
	if writes := strings.Count(string(ansible.Assets()[resourcesModule]), `"config", "set", "mgr"`); writes != 1 {
		t.Fatalf("the settings worker issues %d mgr config writes, want one", writes)
	}
}

func TestDerivedModulesMatchTheWorker(t *testing.T) {
	if got, want := pythonTuple(t, resourcesModule, "DERIVED_MODULES"), storage.DerivableManagerModules(); !slices.Equal(got, want) {
		t.Fatalf("the worker derives %q, Go derives %q", got, want)
	}
}

func settingsRequestFor(r Request) Request {
	r.Action = "settings"
	r.Settings = &Settings{Modules: []string{"nfs"}}
	return r
}

func TestSettingsDeadline(t *testing.T) {
	r := settingsRequestFor(coreRequest())
	apply, err := cephDeadline(r, "apply")
	if err != nil || apply != time.Duration((2*3+3)*nativeCall+60)*time.Second {
		t.Fatalf("the settings apply deadline is %v (%v)", apply, err)
	}
	observe, err := cephDeadline(r, "observe")
	if err != nil || observe != time.Duration((3+2)*nativeCall+60)*time.Second {
		t.Fatalf("the settings observe deadline is %v (%v)", observe, err)
	}
}

func TestSettingsIdentity(t *testing.T) {
	r := settingsRequestFor(coreRequest())
	if _, err := New(api.StorageCluster, nil).identity(context.Background(), executionFor(t, r)); err != nil {
		t.Fatalf("a settings block fails its identity: %+v", diagnostics.Of(err))
	}
	unset := r
	unset.Settings = nil
	carried := coreRequest()
	carried.Settings = &Settings{Modules: []string{}}
	for name, request := range map[string]Request{"settings without its member": unset, "cluster with a settings member": carried} {
		_, err := New(api.StorageCluster, nil).identity(context.Background(), executionFor(t, request))
		if d := diagnostics.Of(err); len(d) != 1 || d[0].Code != "lifecycle.state" || d[0].Message != "Ceph request does not match its frozen resource identity" {
			t.Fatalf("a %s passes its identity: %+v", name, d)
		}
	}
	if _, err := New(api.StoragePool, nil).identity(context.Background(), executionFor(t, r)); err == nil {
		t.Fatal("a pool capability executes a settings request")
	}
}

func TestSettingsEvidence(t *testing.T) {
	r := runtimeRequest()
	r = settingsRequestFor(r)
	ready := readyEvidence(r)
	if effect, err := EvaluateEvidence(evidenceBytes(t, ready), r, "digest", false); err != nil || effect != reconciliation.EffectCompleted {
		t.Fatalf("ready settings evidence is %s (%v)", effect, err)
	}
	resource := readyEvidence(r)
	resource.Resource = ResourceEvidence{ID: "7"}
	if effect, err := EvaluateEvidence(evidenceBytes(t, resource), r, "digest", false); err == nil || effect != reconciliation.EffectUnknown {
		t.Fatalf("settings evidence with a resource is %s (%v)", effect, err)
	}
}

func TestClusterChildrenDependOnCoreBlockByID(t *testing.T) {
	objects := []api.Object{cephfsPool("fs-meta", "cephfs-metadata"), cephfsPool("fs-data", "cephfs-data"), filesystemOf(), subvolumeOf(),
		childOf(api.StoragePlacementPolicy, "fast", "ruleName", "fast-hosts", "replicated", requestMap("size", 2, "minSize", 2)),
		childOf(api.StorageRBDImage, "disk", "poolRef", "volumes", "sizeBytes", 10737418240)}
	for _, object := range pinnedCatalog(t, withMDS).Objects() {
		if object.Kind() == api.StoragePool {
			object = object.WithSpec(object.Spec().With("placementPolicyRef", api.StringValue("fast")))
		}
		objects = append(objects, object)
	}
	catalog := api.NewCatalog(objects)
	kinds := map[string]bool{}
	for _, definition := range cephDefinitions(t, catalog) {
		kinds[definition.Kind] = true
		if definition.Implementation != Implementation || definition.Kind == string(api.StorageCluster) {
			continue
		}
		if !slices.Contains(definition.Dependencies, ClusterBlockID("example")) {
			t.Errorf("%s depends on %v, not on its cluster block", definition.ID, definition.Dependencies)
		}
		for _, ref := range definition.Requires {
			if ref.Kind == string(api.StorageCluster) {
				t.Errorf("%s requires the StorageCluster %s", definition.ID, ref.Object)
			}
		}
	}
	for _, kind := range []api.Kind{api.StoragePlacementPolicy, api.StoragePool, api.StorageClient, api.StorageRBDImage, api.StorageFilesystem, api.StorageSubvolume} {
		if !kinds[string(kind)] {
			t.Errorf("the catalog plans no %s block", kind)
		}
	}
}

func TestRequiresStorageClusterNoLongerResolvesToImageHostSettingsOrUnitBlocksForChildren(t *testing.T) {
	plan := cephPlan(t, settingsCatalog(t, []api.Value{nfsService("files")}))
	blockOf(t, plan, "ceph-settings-example")
	if pool := blockOf(t, plan, "ceph-pool-volumes"); !slices.Equal(pool.Dependencies, []string{"ceph-cluster-example", "ceph-image-example"}) {
		t.Fatalf("the pool depends on %v", pool.Dependencies)
	}
}

func TestClientStillOrdersAfterItsPool(t *testing.T) {
	plan := cephPlan(t, settingsCatalog(t, []api.Value{nfsService("files")}))
	if client := blockOf(t, plan, "ceph-client-consumer"); !slices.Contains(client.Dependencies, "ceph-pool-volumes") {
		t.Fatalf("the client depends on %v", client.Dependencies)
	}
	removal, err := plan.Inverse()
	if err != nil {
		t.Fatal(err)
	}
	apply, inverse := cephIDs(plan), cephIDs(removal)
	if slices.Index(apply, "ceph-client-consumer") < slices.Index(apply, "ceph-pool-volumes") || slices.Index(inverse, "ceph-client-consumer") > slices.Index(inverse, "ceph-pool-volumes") {
		t.Fatalf("the client is not ordered after its pool: apply %v, removal %v", apply, inverse)
	}
}

func TestSettingsBlockInsertsWithoutReorderingRemoval(t *testing.T) {
	without := cephPlan(t, settingsCatalog(t, nil))
	with := cephPlan(t, settingsCatalog(t, []api.Value{nfsService("files")}))
	withoutRemoval, err := without.Inverse()
	if err != nil {
		t.Fatal(err)
	}
	withRemoval, err := with.Inverse()
	if err != nil {
		t.Fatal(err)
	}
	drop := func(ids []string) []string {
		return slices.DeleteFunc(ids, func(id string) bool { return id == "ceph-settings-example" || id == "ceph-service-example-nfs-files" })
	}
	if got, want := drop(cephIDs(with)), cephIDs(without); !slices.Equal(got, want) {
		t.Fatalf("the settings block reorders the apply: %v, want %v", got, want)
	}
	if got, want := drop(cephIDs(withRemoval)), cephIDs(withoutRemoval); !slices.Equal(got, want) {
		t.Fatalf("the settings block reorders the removal: %v, want %v", got, want)
	}
	for _, block := range withRemoval.Blocks {
		if slices.Contains(block.Dependencies, "ceph-settings-example") && block.ID != ClusterBlockID("example") && block.ID != ImageBlockID("example") {
			t.Fatalf("%s waits on the settings block in removal", block.ID)
		}
	}
}
