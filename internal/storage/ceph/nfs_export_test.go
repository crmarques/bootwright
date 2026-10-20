package ceph

import (
	"context"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

func exportNative(pairs ...any) api.Value {
	fsal := requestMap("name", "CEPH", "fs_name", "files", "cmount_path", "/")
	native := requestMap("pseudo", "/docs", "access_type", "RW", "squash", "root_squash", "security_label", true,
		"protocols", api.ListValue(api.IntegerValue("4")), "transports", api.StringList("TCP"), "fsal", fsal, "clients", api.ListValue())
	for i := 0; i < len(pairs); i += 2 {
		name := pairs[i].(string)
		native = native.With(name, requestMap(name, pairs[i+1]).Get(name))
	}
	return native
}

func subvolumeExport(native api.Value) api.Object {
	return childOf(api.StorageNFSExport, "docs", "serviceID", "shares", "subvolumeRef", "docs", "native", native)
}

func filesystemExport(native api.Value) api.Object {
	return childOf(api.StorageNFSExport, "docs", "serviceID", "shares", "filesystemRef", "files", "native", native)
}

func withShares(spec api.Value) api.Value {
	spec = withMDS(spec)
	shares := requestMap("service_type", "nfs", "service_id", "shares", "placement", requestMap("label", "mon", "count", 1))
	return spec.WithPath(api.ListValue(append(spec.Get("ceph", "services").Items(), shares)...), "ceph", "services")
}

func exportCatalog(hosts int, change func(api.Value) api.Value, export api.Object) api.Catalog {
	fs := filesystemOf("subvolumeGroups", api.ListValue(requestMap("name", "csi")))
	return planCatalog(hosts, hosts, change, cephfsPool("fs-meta", "cephfs-metadata"), cephfsPool("fs-data", "cephfs-data"), fs, subvolumeOf("group", "csi"), export)
}

func plannedExport(t *testing.T, catalog api.Catalog) Request {
	t.Helper()
	r, err := planned(t, catalog, api.StorageNFSExport, "docs")
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	return r
}

func exportPlan(t *testing.T, catalog api.Catalog) reconciliation.BlockDefinition {
	t.Helper()
	input := lifecycle.PlanInput{Verb: reconciliation.Apply, Context: lifecycle.ContextIdentity{Name: "synthetic"}, State: compilation.NewState(catalog, catalog, nil), Controller: "controller"}
	plan, err := New(api.StorageNFSExport, nil).Plan(context.Background(), input)
	if err != nil || len(plan.Definitions) != 1 {
		t.Fatalf("the export planned %+v (%v)", plan.Definitions, diagnostics.Of(err))
	}
	return plan.Definitions[0]
}

func TestNFSExportBlockDependsOnUnitAndBacking(t *testing.T) {
	for _, tt := range []struct {
		export  api.Object
		backing reconciliation.ObjectRef
	}{
		{subvolumeExport(exportNative()), reconciliation.ObjectRef{Kind: string(api.StorageSubvolume), Object: "docs"}},
		{filesystemExport(exportNative("path", "/")), reconciliation.ObjectRef{Kind: string(api.StorageFilesystem), Object: "files"}},
	} {
		block := exportPlan(t, exportCatalog(3, withShares, tt.export))
		if block.ID != "ceph-nfs-export-docs" || block.ID != NFSExportBlockID("docs") || block.Kind != string(api.StorageNFSExport) || block.Object != "docs" || block.Stage != reconciliation.StageClusters ||
			!slices.Equal(block.Exclusive, []string{"ceph/example"}) || block.Description != "realize Ceph NFS export docs" {
			t.Fatalf("the export block is %+v", block)
		}
		if !slices.Equal(block.Dependencies, []string{"ceph-cluster-example", "ceph-image-example", "ceph-service-example-nfs-shares"}) || !slices.Equal(block.Requires, []reconciliation.ObjectRef{tt.backing}) {
			t.Fatalf("the export block depends on %v and requires %v", block.Dependencies, block.Requires)
		}
		removal, err := New(api.StorageNFSExport, nil).Removal(context.Background(), reconciliation.Block{BlockDefinition: block})
		if err != nil || removal.Description != "remove Ceph NFS export docs" {
			t.Fatalf("the export removal is %+v (%v)", removal, err)
		}
	}
}

func TestNFSExportRequestGolden(t *testing.T) {
	r := plannedExport(t, exportCatalog(3, withShares, subvolumeExport(exportNative())))
	want := NFSExport{Filesystem: "files", Group: "csi", Name: "docs", Service: "shares", Subvolume: "docs"}
	x := r.NFSExport
	if r.Action != "nfs-export" || r.Filesystem != nil || r.Subvolume != nil || r.Pool != nil || x == nil || x.Filesystem != want.Filesystem || x.Group != want.Group || x.Name != want.Name ||
		x.Service != want.Service || x.Subvolume != want.Subvolume || x.SubvolumePath != "" || x.Native.Path != "" || x.Native.Clients == nil {
		t.Fatalf("the subvolume export froze as %+v %+v", r, x)
	}
	native := NFSExportNative{AccessType: "RW", Clients: []NFSExportClient{}, FSAL: NFSExportFSAL{CmountPath: "/", FSName: "files", Name: "CEPH"}, Protocols: []int{4}, Pseudo: "/docs", SecurityLabel: true, Squash: "root_squash", Transports: []string{"TCP"}}
	if !reflect.DeepEqual(x.Native, native) {
		t.Fatalf("the export's native record froze as %+v", x.Native)
	}
	data, err := r.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	matchesGolden(t, "request-nfs-export", data, false)
	bare := exportNative("fsal", requestMap("name", "CEPH", "cmount_path", "/"), "clients", api.ListValue(requestMap("access_type", "RO", "addresses", api.StringList("192.0.2.0/24"), "squash", "all_squash")), "sectype", api.StringList("sys"))
	x = plannedExport(t, exportCatalog(3, withShares, filesystemExport(bare.With("path", api.StringValue("/"))))).NFSExport
	clients := []NFSExportClient{{AccessType: "RO", Addresses: []string{"192.0.2.0/24"}, Squash: "all_squash"}}
	if x.Native.FSAL.FSName != "files" || x.Native.Path != "/" || x.Group != "" || x.Subvolume != "" || x.Filesystem != "files" || !reflect.DeepEqual(x.Native.Clients, clients) || !slices.Equal(x.Native.SecType, []string{"sys"}) {
		t.Fatalf("the filesystem export froze as %+v", x)
	}
}

func TestNFSExportRequestCarriesNoUserID(t *testing.T) {
	refused := []string{"user_id", "export_id", "cluster_id", "cephx_key", "access_key_id", "secret_access_key", "sec_label_xattr"}
	var walk func(reflect.Type)
	walk = func(typ reflect.Type) {
		for typ.Kind() == reflect.Slice || typ.Kind() == reflect.Pointer {
			typ = typ.Elem()
		}
		if typ.Kind() != reflect.Struct {
			return
		}
		for field := range typ.Fields() {
			if name, _, _ := strings.Cut(field.Tag.Get("json"), ","); slices.Contains(refused, name) {
				t.Fatalf("%s carries %s", typ, name)
			}
			walk(field.Type)
		}
	}
	walk(reflect.TypeFor[NFSExport]())
	data, err := plannedExport(t, exportCatalog(3, withShares, subvolumeExport(exportNative()))).Canonical()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range refused {
		if strings.Contains(string(data), `"`+name+`"`) {
			t.Fatalf("the frozen export request carries %s", name)
		}
	}
}

func TestNFSExportDeadline(t *testing.T) {
	for _, hosts := range []int{3, 6} {
		r := plannedExport(t, exportCatalog(hosts, withShares, subvolumeExport(exportNative())))
		if len(r.Hosts) != hosts {
			t.Fatalf("the export froze %d hosts, want %d", len(r.Hosts), hosts)
		}
		for operation, want := range map[string]time.Duration{"apply": 2280 * time.Second, "observe": 720 * time.Second, "destroy": 1380 * time.Second} {
			if got, err := cephDeadline(r, operation); err != nil || got != want {
				t.Fatalf("with %d hosts the export %s deadline is %v (%v), want %v", hosts, operation, got, err, want)
			}
		}
	}
}

func exportExecution(t *testing.T, r Request) lifecycle.Execution {
	t.Helper()
	e := executionFor(t, r)
	e.Block.ID, e.Block.Kind, e.Block.Object = NFSExportBlockID("docs"), string(api.StorageNFSExport), "docs"
	return e
}

func changedExport(r Request, change func(*NFSExport)) Request {
	if r.NFSExport != nil {
		x := *r.NFSExport
		change(&x)
		r.NFSExport = &x
	}
	return r
}

func refusedIdentity(t *testing.T, kind api.Kind, e lifecycle.Execution, name string) {
	t.Helper()
	if _, err := New(kind, nil).identity(context.Background(), e); len(diagnostics.Of(err)) != 1 || diagnostics.Of(err)[0].Message != "Ceph request does not match its frozen resource identity" {
		t.Fatalf("%s: %s identity accepted or refused otherwise: %v", name, kind, diagnostics.Of(err))
	}
}

func TestNFSExportIdentity(t *testing.T) {
	r := plannedExport(t, exportCatalog(3, withShares, subvolumeExport(exportNative())))
	if got, err := New(api.StorageNFSExport, nil).identity(context.Background(), exportExecution(t, r)); err != nil || got.NFSExport == nil || got.Owner != "apply-original" {
		t.Fatalf("the planned export block was refused: %+v (%v)", got.NFSExport, diagnostics.Of(err))
	}
	other := exportExecution(t, r)
	other.Block.Object = "other"
	refusedIdentity(t, api.StorageNFSExport, other, "another object")
	without := r
	without.NFSExport = nil
	refusedIdentity(t, api.StorageNFSExport, exportExecution(t, without), "no member")
	subvolume := r
	subvolume.Action, subvolume.Subvolume = "subvolume", &Subvolume{Filesystem: "files", Group: "csi", Mode: "755", Name: "docs", Pool: "fs-data"}
	e := exportExecution(t, subvolume)
	e.Block.ID, e.Block.Kind = "ceph-subvolume-docs", string(api.StorageSubvolume)
	refusedIdentity(t, api.StorageSubvolume, e, "subvolume carrying an export")
	refusedIdentity(t, api.StorageNFSExport, exportExecution(t, changedExport(r, func(x *NFSExport) { x.Name = "other" })), "another export name")
	refusedIdentity(t, api.StorageNFSExport, exportExecution(t, changedExport(r, func(x *NFSExport) { x.SubvolumePath = "/volumes/csi/docs/" + subvolumeID })), "frozen subvolume path")
	refusedIdentity(t, api.StorageSubvolume, exportExecution(t, r), "subvolume capability")
}

func subvolumeProofOn(t *testing.T, filesystem string, proof lifecycle.BlockEvidence) lifecycle.BlockEvidence {
	t.Helper()
	r, err := DecodeRequest(proof.Block.Request)
	if err != nil {
		t.Fatal(err)
	}
	subvolume := *r.Subvolume
	subvolume.Filesystem, r.Subvolume = filesystem, &subvolume
	if proof.Block.Request, err = r.Canonical(); err != nil {
		t.Fatal(err)
	}
	return proof
}

func TestNFSExportPrepareBindsTheProvedSubvolumePath(t *testing.T) {
	r := plannedExport(t, exportCatalog(3, withShares, subvolumeExport(exportNative())))
	exact := "/volumes/csi/docs/" + subvolumeID
	e := exportExecution(t, r)
	e.ApplyProved = append(e.ApplyProved, subvolumeProof(t, "docs", "csi", exact))
	prepared, err := New(api.StorageNFSExport, nil).prepare(context.Background(), e)
	if err != nil || prepared.NFSExport.SubvolumePath != exact || r.NFSExport.SubvolumePath != "" {
		t.Fatalf("the export bound %+v (%v)", prepared.NFSExport, diagnostics.Of(err))
	}
	e = exportExecution(t, r)
	e.ApplyProved = append(e.ApplyProved, subvolumeProof(t, "docs", "other", "/volumes/other/docs/"+subvolumeID))
	_, err = New(api.StorageNFSExport, nil).prepare(context.Background(), e)
	refusedWith(t, err, "lifecycle.state", "the creation proof of StorageSubvolume docs names another filesystem or group than StorageNFSExport docs froze", "restore the original apply evidence")
	e = exportExecution(t, r)
	e.ApplyProved = append(e.ApplyProved, subvolumeProofOn(t, "other", subvolumeProof(t, "docs", "csi", exact)))
	if _, filesystem, group, err := ProvedSubvolume(e.ApplyProved, "docs"); err != nil || filesystem != "other" || group != "csi" {
		t.Fatalf("the foreign-filesystem proof names %q %q (%v)", filesystem, group, diagnostics.Of(err))
	}
	_, err = New(api.StorageNFSExport, nil).prepare(context.Background(), e)
	refusedWith(t, err, "lifecycle.state", "the creation proof of StorageSubvolume docs names another filesystem or group than StorageNFSExport docs froze", "restore the original apply evidence")
	_, err = New(api.StorageNFSExport, nil).prepare(context.Background(), exportExecution(t, r))
	refusedWith(t, err, "lifecycle.state", "StorageSubvolume docs lacks its applied creation proof", "apply StorageSubvolume docs first; a consumer binds the path its creation reported and never constructs one")
	fs := plannedExport(t, exportCatalog(3, withShares, filesystemExport(exportNative("path", "/"))))
	if prepared, err := New(api.StorageNFSExport, nil).prepare(context.Background(), exportExecution(t, fs)); err != nil || prepared.NFSExport.SubvolumePath != "" {
		t.Fatalf("the filesystem export prepared %+v (%v)", prepared.NFSExport, diagnostics.Of(err))
	}
}

func TestNFSExportEvidence(t *testing.T) {
	r := runtimeRequest()
	r.Action, r.NFSExport = "nfs-export", &NFSExport{Filesystem: "files", Name: "docs", Service: "shares", Native: NFSExportNative{Pseudo: "/docs"}}
	evaluate := func(e Evidence) (reconciliation.EffectState, error) {
		return EvaluateEvidence(evidenceBytes(t, e), r, "digest", false)
	}
	ready := readyEvidence(r)
	ready.Resource = ResourceEvidence{ID: "3", Path: "/docs"}
	if effect, err := evaluate(ready); err != nil || effect != reconciliation.EffectCompleted {
		t.Fatalf("ready export evidence is %s (%v)", effect, err)
	}
	for name, mutate := range map[string]func(*Evidence){
		"ready without resource": func(e *Evidence) { e.Resource = ResourceEvidence{} },
		"other path":             func(e *Evidence) { e.Resource.Path = "/other" },
		"id zero":                func(e *Evidence) { e.Resource.ID = "0" },
		"id text":                func(e *Evidence) { e.Resource.ID = "abc" },
		"daemon": func(e *Evidence) {
			e.Daemons = []UnitDaemon{{Host: "node-1", Name: "nfs.shares.0", Service: "nfs.shares"}}
		},
		"endpoint": func(e *Evidence) { e.Endpoint = EndpointEvidence{Reachable: true} },
		"client":   func(e *Evidence) { e.Client = "client.docs" },
		"absent with a resource": func(e *Evidence) {
			e.Exists, e.Ready, e.Owned = false, false, false
		},
	} {
		e := ready
		mutate(&e)
		if effect, err := evaluate(e); err == nil || effect != reconciliation.EffectUnknown {
			t.Errorf("%s: export evidence is %s (%v)", name, effect, err)
		}
	}
	partial := readyEvidence(r)
	partial.Ready, partial.Partial = false, true
	if effect, err := evaluate(partial); err != nil || effect != reconciliation.EffectPartial {
		t.Fatalf("partial export evidence without a resource is %s (%v)", effect, err)
	}
	partial.Resource = ResourceEvidence{ID: "3", Path: "/other"}
	if effect, err := evaluate(partial); err == nil || effect != reconciliation.EffectUnknown {
		t.Fatalf("partial export evidence at another pseudo path is %s (%v)", effect, err)
	}
}

func TestNFSExportPlanPropagatesBackingRefusals(t *testing.T) {
	withoutMDS := func(spec api.Value) api.Value {
		shares := requestMap("service_type", "nfs", "service_id", "shares", "placement", requestMap("label", "mon", "count", 1))
		return withCorePair(spec).WithPath(api.ListValue(append(spec.Get("ceph", "services").Items(), shares)...), "ceph", "services")
	}
	_, err := planned(t, exportCatalog(3, withoutMDS, subvolumeExport(exportNative())), api.StorageNFSExport, "docs")
	refusedWith(t, err, "api.invariant", "StorageCluster example projects no MDS service mds.files for StorageFilesystem files", "declare the mds service entry with service_id files in spec.ceph.services")
	_, err = planned(t, exportCatalog(3, withMDS, subvolumeExport(exportNative())), api.StorageNFSExport, "docs")
	refusedWith(t, err, "api.invariant", "StorageNFSExport docs's spec.serviceID resolves to no declared nfs service of StorageCluster example",
		"add a spec.ceph.services entry with service_type nfs and this service_id to StorageCluster example, or set spec.serviceID to the service_id of a declared nfs service")
	long := strings.Repeat("d", 48)
	export := childOf(api.StorageNFSExport, long, "serviceID", "shares", "subvolumeRef", "docs", "native", exportNative())
	_, err = planned(t, exportCatalog(3, withShares, export), api.StorageNFSExport, long)
	refusedWith(t, err, "api.invariant", "StorageNFSExport "+long+" has a name too long to identify its block, whose identity holds at most 63 characters", "rename the StorageNFSExport to at most 47 characters")
	if _, err := planned(t, exportCatalog(3, withShares, childOf(api.StorageNFSExport, long[:47], "serviceID", "shares", "subvolumeRef", "docs", "native", exportNative())), api.StorageNFSExport, long[:47]); err != nil {
		t.Fatalf("a 47-character export name was refused: %v", diagnostics.Of(err))
	}
}
