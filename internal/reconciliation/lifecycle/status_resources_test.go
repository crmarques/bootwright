package lifecycle

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/secrets/custody"
	"github.com/crmarques/bootwright/internal/secrets/secretstore"
)

const storageImplementation = "storage-test-v1"

type listingBinder struct {
	SecretBinder
	reads int
}

func (b *listingBinder) Bindings(ctx context.Context, request custody.BindingsRequest) ([]string, error) {
	b.reads++
	return b.SecretBinder.Bindings(ctx, request)
}

func (b *listingBinder) Reopen(ctx context.Context, request custody.BindingRequest) ([]secretstore.BoundMaterial, error) {
	b.reads++
	return b.SecretBinder.Reopen(ctx, request)
}

func (b *listingBinder) ReadCurrent(ctx context.Context, request custody.ReadCurrentRequest) ([]secretstore.BoundMaterial, error) {
	b.reads++
	return b.SecretBinder.ReadCurrent(ctx, request)
}

func (b *listingBinder) Holds(ctx context.Context, selected secretstore.Context, area secretstore.Area, block, name string) (bool, error) {
	b.reads++
	return b.SecretBinder.Holds(ctx, selected, area, block, name)
}

func storageBlock(id string, kind api.Kind, object string) reconciliation.BlockDefinition {
	block := definition(id)
	block.Kind, block.Object, block.Implementation = string(kind), object, storageImplementation
	return block
}

func storageObject(kind api.Kind, name string, fields ...api.FieldValue) api.Object {
	return api.NewObject(kind, name, api.Value{}, api.MapValue(fields...))
}

func clusterRef(name string) api.FieldValue {
	return api.FieldValue{Name: "clusterRef", Value: api.StringValue(name)}
}

type storageRowsHarness struct {
	*harness
	binder   *listingBinder
	declared testCompiler
}

func newStorageRowsHarness(t *testing.T) *storageRowsHarness {
	t.Helper()
	h := newPlannedHarness(t, []reconciliation.BlockDefinition{
		storageBlock("pool-rbd", api.CephPool, "rbd"),
		storageBlock("client-app", api.CephClient, "app"),
		storageBlock("cluster-ceph", api.CephCluster, "ceph"),
	})
	objects := []api.Object{
		api.NewObject(api.Environment, "lab", api.Value{}, api.MapValue(
			api.FieldValue{Name: "controller", Value: api.MapValue(api.FieldValue{Name: "machineRef", Value: api.StringValue("controller")})},
		)),
		storageObject(api.CephCluster, "ceph", api.FieldValue{Name: "management", Value: api.StringValue("managed")}),
		storageObject(api.CephCluster, "archive", api.FieldValue{Name: "management", Value: api.StringValue("external")}),
		storageObject(api.CephPool, "rbd", clusterRef("ceph")),
		storageObject(api.CephPool, "meta", clusterRef("ceph")),
		storageObject(api.CephClient, "app", clusterRef("ceph")),
		storageObject(api.CephPool, "cold", clusterRef("archive")),
	}
	applied := api.NewCatalog(objects)
	declared := api.NewCatalog(append(objects, storageObject(api.CephObjectGateway, "objects", clusterRef("ceph"))))
	h.service.compiler = testCompiler{state: compilation.NewState(applied, applied, nil)}
	h.service.capabilities = boundCapabilities{
		bindings: []CapabilityBinding{
			{Kind: string(api.CephCluster), Implementation: storageImplementation},
			{Kind: string(api.CephPool), Implementation: storageImplementation},
			{Kind: string(api.CephClient), Implementation: storageImplementation},
		},
		capabilities: []Capability{h.capability, plannedCapability{Capability: h.capability}, plannedCapability{Capability: h.capability}},
	}
	binder := &listingBinder{SecretBinder: h.service.binder}
	h.service.binder = binder
	return &storageRowsHarness{harness: h, binder: binder, declared: testCompiler{state: compilation.NewState(declared, declared, nil)}}
}

func (r *storageRowsHarness) status(t *testing.T, operation bool) (*StatusResult, *StorageResourcesResult) {
	t.Helper()
	listed := r.binder.reads
	applied := r.service.compiler
	r.service.compiler = r.declared
	defer func() { r.service.compiler = applied }()
	status, err := r.service.Status(context.Background(), StatusRequest{ContextName: testContextName})
	if err != nil {
		t.Fatal(err)
	}
	if operation && r.binder.reads == listed {
		t.Fatal("status read no keyring, so the listing count proves nothing")
	}
	before := r.binder.reads
	found, err := r.service.StorageResources(context.Background(), testContextName)
	if err != nil {
		t.Fatal(err)
	}
	if r.binder.reads != before {
		t.Fatalf("reading the storage resources read the keyring %d times", r.binder.reads-before)
	}
	if !reflect.DeepEqual(found.Clusters, status.StorageClusters) {
		t.Fatalf("storage resources = %+v, status = %+v", found.Clusters, status.StorageClusters)
	}
	for _, cluster := range status.Clusters {
		if cluster.Resources != nil {
			t.Fatalf("the cluster root %s carries resource rows %+v", cluster.Name, cluster.Resources)
		}
	}
	return status, found
}

func storageRows(t *testing.T, status *StatusResult) map[string][]ResourceSummary {
	t.Helper()
	rows := map[string][]ResourceSummary{}
	for _, cluster := range status.StorageClusters {
		if cluster.Resources == nil {
			t.Fatalf("the CephCluster %s has nil resources", cluster.Name)
		}
		rows[cluster.Name] = cluster.Resources
	}
	return rows
}

func TestStatusStorageResourceRows(t *testing.T) {
	h := newStorageRowsHarness(t)
	row := func(kind api.Kind, name string, status RealizationStatus) ResourceSummary {
		return ResourceSummary{Kind: string(kind), Name: name, Status: status}
	}
	status, found := h.status(t, false)
	want := map[string][]ResourceSummary{
		"archive": {},
		"ceph": {
			row(api.CephPool, "meta", RealizationPending), row(api.CephPool, "rbd", RealizationPending),
			row(api.CephClient, "app", RealizationPending), row(api.CephObjectGateway, "objects", RealizationUnsupported),
		},
	}
	if got := storageRows(t, status); !reflect.DeepEqual(got, want) {
		t.Fatalf("rows without an operation = %+v, want %+v", got, want)
	}
	if found.Frozen == nil || len(found.Frozen) != 0 {
		t.Fatalf("frozen blocks without an operation = %#v", found.Frozen)
	}
	completeApply(t, h.harness)
	status, found = h.status(t, true)
	want["ceph"][1].Status, want["ceph"][2].Status = RealizationDone, RealizationDone
	if got := storageRows(t, status); !reflect.DeepEqual(got, want) {
		t.Fatalf("rows after an apply = %+v, want %+v", got, want)
	}
	if len(found.Frozen) != 3 {
		t.Fatalf("frozen blocks = %+v", found.Frozen)
	}
	for _, block := range found.Frozen {
		if !slices.Contains([]string{"CephPool/rbd", "CephClient/app", "CephCluster/ceph"}, block.Kind+"/"+block.Object) ||
			string(block.Request) != `{"name":"`+block.ID+`"}` {
			t.Fatalf("frozen block = %+v", block)
		}
	}
	h = newStorageRowsHarness(t)
	h.capability.errorFor = map[string]error{"client-app": errors.New("the client was refused")}
	if _, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: testContextName, SkipConfirmation: true}); err == nil {
		t.Fatal("an apply whose client block failed succeeded")
	}
	status, _ = h.status(t, true)
	want["ceph"][1].Status, want["ceph"][2].Status = RealizationPending, RealizationFailed
	if got := storageRows(t, status)["ceph"]; !reflect.DeepEqual(got, want["ceph"]) {
		t.Fatalf("rows after a failed client = %+v, want %+v", got, want["ceph"])
	}
}
