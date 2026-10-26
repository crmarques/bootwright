package ceph

import (
	"slices"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/reconciliation"
)

func TestEveryStorageChildKindHasAScopeRow(t *testing.T) {
	children := map[api.Kind]bool{}
	for _, kind := range api.Kinds() {
		schema := api.Schema(kind)
		if schema == nil {
			continue
		}
		if field, found := schema.Field("clusterRef"); found && field.Shape != nil && slices.Contains(field.Shape.Reference, api.StorageCluster) {
			children[kind] = true
		}
	}
	if !children[api.StoragePool] || !children[api.StorageNFSExport] {
		t.Fatalf("storage child kinds = %v", children)
	}
	for kind := range children {
		if _, found := scopeRows[kind]; !found {
			t.Errorf("the storage child kind %s has no scope row", kind)
		}
	}
	for kind := range scopeRows {
		if !children[kind] {
			t.Errorf("the scope row %s is not a storage child kind", kind)
		}
	}
}

func frozenScopeBlock(t *testing.T, kind api.Kind, object string, request Request) reconciliation.Block {
	t.Helper()
	canonical, err := request.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	return reconciliation.Block{BlockDefinition: reconciliation.BlockDefinition{Kind: string(kind), Object: object, Implementation: Implementation, Request: canonical}}
}

func declaredScopeObject(kind api.Kind, name string, fields ...api.FieldValue) api.Object {
	return api.NewObject(kind, name, api.Value{}, api.MapValue(append(fields, api.FieldValue{Name: "clusterRef", Value: api.StringValue("ceph")})...))
}

func scopeText(name, value string) api.FieldValue {
	return api.FieldValue{Name: name, Value: api.StringValue(value)}
}

func TestResourceScopeRendersEachKindFromItsFrozenRequest(t *testing.T) {
	erasure := frozenScopeBlock(t, api.StoragePool, "rbd-ec", Request{Version: Version, Action: "pool", Pool: &Pool{
		Name: "rbd-ec", Application: "rbd", Overwrites: true, Type: "erasure", Erasure: &PoolErasure{DataChunks: 4, CodingChunks: 2}}})
	image := frozenScopeBlock(t, api.StorageRBDImage, "vm-01", Request{Version: Version, Action: "rbd", RBD: &RBDImage{
		Name: "vm-01", Pool: "rbd-meta", DataPool: "rbd-ec", SizeBytes: 1073741824}})
	subvolume := frozenScopeBlock(t, api.StorageSubvolume, "docs", Request{Version: Version, Action: "subvolume", Subvolume: &Subvolume{
		Name: "docs", Filesystem: "files"}})
	declared := api.NewCatalog([]api.Object{
		declaredScopeObject(api.StoragePool, "rbd-ec", scopeText("type", "erasure")),
		declaredScopeObject(api.StorageSubvolume, "docs", scopeText("filesystemRef", "files")),
		declaredScopeObject(api.StorageObjectGateway, "objects", scopeText("realm", "r1"), scopeText("zoneGroup", "zg1"), scopeText("zone", "z2")),
		declaredScopeObject(api.StorageNFSExport, "docs-export", scopeText("serviceID", "other"),
			api.FieldValue{Name: "native", Value: api.MapValue(scopeText("pseudo", "/data"))}),
		declaredScopeObject(api.StorageExport, "foundation", api.FieldValue{Name: "dataFoundation", Value: api.MapValue(
			scopeText("rbdPoolRef", "rbd-meta"), scopeText("filesystemRef", "files"), scopeText("objectGatewayRef", "objects"))}),
		declaredScopeObject(api.StorageExport, "bare"),
		declaredScopeObject(api.StorageNVMeoFSubsystem, "block-a", scopeText("serviceRef", "nvmeof.block")),
	})
	gateway := frozenScopeBlock(t, api.StorageObjectGateway, "objects", Request{Version: Version, Action: "gateway", Gateway: &Gateway{
		Name: "objects", Realm: "r1", ZoneGroup: "zg1", Zone: "z1"}})
	gatewayPool := frozenScopeBlock(t, api.StorageObjectGateway, "objects", Request{Version: Version, Action: "pool", Pool: &Pool{
		Name: "z1.rgw.log", Application: "rgw", Type: "replicated", Size: 3, MinSize: 2}})
	export := frozenScopeBlock(t, api.StorageNFSExport, "docs-export", Request{Version: Version, Action: "nfs-export", NFSExport: &NFSExport{
		Name: "docs-export", Service: "files", Native: NFSExportNative{Pseudo: "/docs"}}})
	frozen := []reconciliation.Block{erasure, image, subvolume, gatewayPool, gateway, gatewayPool, export}
	other := erasure
	other.Implementation = "storage-other-v1"
	stale := frozenScopeBlock(t, api.StoragePool, "rbd-ec", Request{Version: "ceph-core-v9", Action: "pool", Pool: &Pool{Name: "rbd-ec"}})
	mismatched := frozenScopeBlock(t, api.StoragePool, "rbd-ec", Request{Version: Version, Action: "client", Pool: &Pool{Name: "rbd-ec"}})
	for _, tt := range []struct {
		kind   api.Kind
		name   string
		frozen []reconciliation.Block
		want   string
	}{
		{api.StoragePool, "rbd-ec", frozen, "erasure k=4 m=2, application rbd, overwrites"},
		{api.StorageRBDImage, "vm-01", frozen, "image rbd-meta/vm-01, 1073741824 bytes, data pool rbd-ec"},
		{api.StorageSubvolume, "docs", frozen, "subvolume files/_nogroup/docs"},
		{api.StoragePool, "rbd-ec", nil, ""},
		{api.StoragePool, "rbd-ec", []reconciliation.Block{other}, ""},
		{api.StoragePool, "rbd-ec", []reconciliation.Block{stale}, ""},
		{api.StoragePool, "rbd-ec", []reconciliation.Block{mismatched}, ""},
		{api.StorageSubvolume, "docs", nil, ""},
		{api.StorageObjectGateway, "objects", frozen, "zone r1/zg1/z1"},
		{api.StorageNFSExport, "docs-export", frozen, "export /docs on nfs.files"},
		{api.StorageObjectGateway, "objects", nil, ""},
		{api.StorageObjectGateway, "objects", []reconciliation.Block{gatewayPool}, ""},
		{api.StorageNFSExport, "docs-export", nil, ""},
		{api.StorageExport, "foundation", nil, "export of pool rbd-meta, filesystem files, object gateway objects; grants consumer administrators every path of filesystem files; grants consumer administrators every RGW user, bucket and key in the realm of object gateway objects; is not isolated from other exports that share its cluster, filesystem or gateway"},
		{api.StorageExport, "bare", nil, "export"},
		{api.StorageExport, "missing", nil, ""},
		{api.StorageNVMeoFSubsystem, "block-a", nil, ""},
		{api.StorageNVMeoFSubsystem, "missing", nil, ""},
		{api.StorageCluster, "ceph", frozen, ""},
	} {
		if got := ResourceScope(declared, tt.kind, tt.name, tt.frozen); got != tt.want {
			t.Errorf("%s/%s over %d blocks = %q, want %q", tt.kind, tt.name, len(tt.frozen), got, tt.want)
		}
	}
}

func TestResourceScopeRendersSMBFromItsFrozenRequestOnly(t *testing.T) {
	declared := smbCatalog(t, subvolumeOf(), smbUsers(), smbFiles(requestMap("label", "mon", "count", 2)), smbDocs(), smbJoin())
	base := sambaBase(t, declared)
	var frozen []reconciliation.Block
	for _, name := range []string{"smb-files", "smb-docs", "smb-join", "smb-users"} {
		frozen = append(frozen, smbBlock(t, builtSMB(t, declared, base, name)))
	}
	other := frozen[0]
	other.Implementation = Implementation
	for _, tt := range []struct {
		name   string
		frozen []reconciliation.Block
		want   string
	}{
		{"smb-files", frozen, "smb cluster files, user"},
		{"smb-docs", frozen, "share docs on smb cluster files"},
		{"smb-join", frozen, "join auth join"},
		{"smb-users", frozen, "users and groups users"},
		{"smb-files", nil, ""},
		{"smb-docs", frozen[:1], ""},
		{"smb-files", []reconciliation.Block{other}, ""},
		{"smb-files", []reconciliation.Block{frozen[0], frozen[0]}, ""},
	} {
		for _, catalog := range []api.Catalog{declared, api.NewCatalog(nil)} {
			if got := ResourceScope(catalog, api.StorageSMBResource, tt.name, tt.frozen); got != tt.want {
				t.Errorf("%s over %d blocks and %d declared objects = %q, want %q", tt.name, len(tt.frozen), len(catalog.Objects()), got, tt.want)
			}
		}
	}
}
