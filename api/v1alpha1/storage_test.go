package v1alpha1

import (
	"slices"
	"testing"
)

func TestStorageWireShapesAreClosedAndVariantsAreExplicit(t *testing.T) {
	for _, kind := range []Kind{StorageCluster, StoragePlacementPolicy, StoragePool, StorageRBDImage, StorageClient, StorageFilesystem, StorageSubvolume, StorageObjectGateway, StorageNFSExport, StorageExport} {
		schema := Schema(kind)
		if schema == nil || schema.Type != Mapping || schema.Open {
			t.Fatalf("%s is not a registered closed object", kind)
		}
		if kind != StorageCluster {
			if field, ok := schema.Field("clusterRef"); !ok || !field.Required || len(field.Shape.Reference) != 1 || field.Shape.Reference[0] != StorageCluster {
				t.Fatalf("%s does not require its owning storage cluster", kind)
			}
		}
		checkStorageShape(t, schema)
	}
	fs, _ := Schema(StorageFilesystem).Field("dataPoolRefs")
	if len(fs.Shape.Element.Alternatives) != 2 {
		t.Fatal("filesystem data pools must accept both scalar and named forms")
	}
	pool := Schema(StoragePool)
	if pool.Discriminator != "type" {
		t.Fatal("pool protection discriminator missing")
	}
	erasure, _ := pool.Field("erasure")
	if erasure.Shape.Open {
		t.Fatal("erasure options must stay closed")
	}
	gateway, _ := Schema(StorageObjectGateway).Field("endpoint")
	ingresses, _ := gateway.Shape.Field("ingresses")
	if _, ok := ingresses.Shape.Element.Field("tls"); ok {
		t.Fatal("RGW transport cannot be overridden on individual ingresses")
	}
	nfs, _ := Schema(StorageNFSExport).Field("ingresses")
	if _, ok := nfs.Shape.Element.Field("tls"); !ok {
		t.Fatal("NFS ingress must preserve its TLS arm")
	}
}

func TestNativeCoreSchemaAndBindingsUseOneClosedAuthoringPath(t *testing.T) {
	ceph, _ := Schema(StorageCluster).Field("ceph")
	for _, retired := range []string{"monitoring", "mgmtGateway"} {
		if _, ok := ceph.Shape.Field(retired); ok {
			t.Fatalf("retired daemon helper %s still admitted", retired)
		}
	}
	services, ok := ceph.Shape.Field("services")
	if !ok || !services.Required || services.Shape.MinLength != 1 || services.Shape.Element.Open {
		t.Fatal("native service declarations must be required and closed")
	}
	for _, native := range []string{"service_type", "service_id", "placement", "networks", "config", "spec", "extra_container_args", "custom_configs"} {
		if _, ok := services.Shape.Element.Field(native); !ok {
			t.Fatalf("missing native envelope field %s", native)
		}
	}
	for _, retired := range []string{"serviceType", "serviceID"} {
		if _, ok := services.Shape.Element.Field(retired); ok {
			t.Fatalf("retired native alias %s still admitted", retired)
		}
	}
	placement, _ := services.Shape.Element.Field("placement")
	for _, native := range []string{"hosts", "label", "host_pattern", "count", "count_per_host"} {
		if _, ok := placement.Shape.Field(native); !ok {
			t.Fatalf("missing native placement %s", native)
		}
	}
	topology, _ := ceph.Shape.Field("topology")
	nodes, _ := topology.Shape.Field("nodes")
	for _, retired := range []string{"roles", "devices", "osd"} {
		if _, ok := nodes.Shape.Element.Field(retired); ok {
			t.Fatalf("retired topology helper %s still admitted", retired)
		}
	}
	bindings, _ := ceph.Shape.Field("serviceBindings")
	secrets, _ := bindings.Shape.Element.Field("secrets")
	ref, _ := secrets.Shape.Element.Field("secretRef")
	if bindings.Shape.NameKey != "serviceRef" || secrets.Shape.NameKey != "path" || len(ref.Shape.Reference) != 1 || ref.Shape.Reference[0] != Secret {
		t.Fatal("native binding must retain a typed descriptor-only reference")
	}
	for _, serviceType := range []string{"mon", "mgr", "osd", "mds", "rgw", "ingress", "nfs"} {
		shape := StorageNativeServiceSpecSchema(serviceType)
		if shape == nil || shape.Open {
			t.Fatalf("native core %s lacks a closed payload", serviceType)
		}
		checkStorageShape(t, shape)
	}
	if mds := StorageNativeServiceSpecSchema("mds"); len(mds.Fields) != 0 {
		t.Fatal("native MDS spec carries no type-specific field")
	}
	for serviceType, refused := range map[string][]string{
		"rgw":     {"rgw_frontend_ssl_certificate", "ssl_cert", "ssl_key"},
		"ingress": {"monitor_password", "keepalived_password", "ssl_cert"},
		"nfs":     {"ssl", "kmip_key", "enable_nfsv3"},
	} {
		for _, name := range refused {
			if _, ok := StorageNativeServiceSpecSchema(serviceType).Field(name); ok {
				t.Fatalf("native %s record admits refused member %s", serviceType, name)
			}
		}
	}
	if StorageNativeServiceSpecSchema("container") != nil {
		t.Fatal("future family schema must be qualified by its owning slice")
	}
	client := Schema(StorageClient)
	pool, _ := client.Field("poolRef")
	access, _ := client.Field("access")
	if !pool.Required || pool.Shape.Reference[0] != StoragePool || !access.Required || len(access.Shape.Enums) != 2 {
		t.Fatal("minimal client must bind one pool and explicit bounded access")
	}
}
func TestRBDImageSchemaIsClosedWithExactBounds(t *testing.T) {
	schema := Schema(StorageRBDImage)
	if schema == nil || schema.Open {
		t.Fatal("StorageRBDImage must be a closed object")
	}
	for _, name := range []string{"clusterRef", "poolRef", "sizeBytes"} {
		if field, ok := schema.Field(name); !ok || !field.Required {
			t.Fatalf("StorageRBDImage must require %s", name)
		}
	}
	size, _ := schema.Field("sizeBytes")
	if size.Shape.Type != Integer || size.Shape.Minimum != "1" || size.Shape.Maximum != "9223372036854775807" {
		t.Fatalf("sizeBytes bounds = %q..%q", size.Shape.Minimum, size.Shape.Maximum)
	}
	features, ok := schema.Field("features")
	if !ok || features.Required || !features.Shape.Unique || features.Shape.Element == nil || !slices.Equal(features.Shape.Element.Enums, []string{"layering", "exclusive-lock", "object-map", "fast-diff", "deep-flatten", "journaling"}) {
		t.Fatal("features must be an optional unique set of the six admitted names in librbd bit order")
	}
	dataPool, ok := schema.Field("dataPoolRef")
	if !ok || dataPool.Required || !slices.Equal(dataPool.Shape.Reference, []Kind{StoragePool}) {
		t.Fatal("dataPoolRef must be an optional StoragePool reference")
	}
	if len(schema.Fields) != 5 {
		t.Fatalf("StorageRBDImage admits %d fields, want 5", len(schema.Fields))
	}
}

func checkStorageShape(t *testing.T, shape *Shape) {
	t.Helper()
	seen := map[string]bool{}
	for _, field := range shape.Fields {
		if field.Name == "" || seen[field.Name] {
			t.Fatal("ambiguous storage wire schema")
		}
		seen[field.Name] = true
		checkStorageShape(t, field.Shape)
	}
	if shape.Element != nil {
		checkStorageShape(t, shape.Element)
	}
	for _, alternative := range shape.Alternatives {
		checkStorageShape(t, alternative)
	}
}

func TestFilesystemSchemaBindsItsMDSServiceAndRetiresDaemonHelpers(t *testing.T) {
	schema := Schema(StorageFilesystem)
	names := []string{}
	for _, field := range schema.Fields {
		names = append(names, field.Name)
	}
	if !slices.Equal(names, []string{"clusterRef", "mdsServiceRef", "metadataPoolRef", "dataPoolRefs", "mds", "subvolumeGroups"}) {
		t.Fatalf("StorageFilesystem fields = %v", names)
	}
	if ref, _ := schema.Field("mdsServiceRef"); !ref.Required || ref.Shape.Type != String {
		t.Fatal("mdsServiceRef must be a required string")
	}
	mds, _ := schema.Field("mds")
	bounds := map[string][2]string{}
	for _, field := range mds.Shape.Fields {
		bounds[field.Name] = [2]string{field.Shape.Minimum, field.Shape.Maximum}
	}
	if len(bounds) != 3 || bounds["activeCount"] != [2]string{"1", "256"} || bounds["standbyCountWanted"] != [2]string{"0", "2147483647"} {
		t.Fatalf("mds fields = %v", bounds)
	}
	for _, retired := range []string{"placement", "serviceSpec"} {
		if _, ok := mds.Shape.Field(retired); ok {
			t.Fatalf("mds.%s must be retired", retired)
		}
	}
	groups, _ := schema.Field("subvolumeGroups")
	size, _ := groups.Shape.Element.Field("sizeBytes")
	uid, _ := groups.Shape.Element.Field("uid")
	if size.Shape.Minimum != "1" || size.Shape.Maximum != "9223372036854775807" || uid.Shape.Minimum != "0" || uid.Shape.Maximum != "2147483647" {
		t.Fatal("group sizeBytes must be positive and uid an int32")
	}
}

func TestSubvolumeSchemaIsClosedWithExactBounds(t *testing.T) {
	schema := Schema(StorageSubvolume)
	if schema == nil || schema.Open {
		t.Fatal("StorageSubvolume must be a closed object")
	}
	names := []string{}
	for _, field := range schema.Fields {
		names = append(names, field.Name)
	}
	if !slices.Equal(names, []string{"clusterRef", "filesystemRef", "group", "sizeBytes", "poolLayoutRef", "uid", "gid", "mode"}) {
		t.Fatalf("StorageSubvolume fields = %v", names)
	}
	for name, required := range map[string]bool{"clusterRef": true, "filesystemRef": true, "group": false, "sizeBytes": false, "poolLayoutRef": false, "uid": false, "gid": false, "mode": false} {
		if field, _ := schema.Field(name); field.Required != required {
			t.Fatalf("StorageSubvolume %s required = %t", name, field.Required)
		}
	}
	for name, bounds := range map[string][2]string{"sizeBytes": {"1", "9223372036854775807"}, "uid": {"0", "2147483647"}, "gid": {"0", "2147483647"}} {
		if field, _ := schema.Field(name); field.Shape.Type != Integer || field.Shape.Minimum != bounds[0] || field.Shape.Maximum != bounds[1] {
			t.Fatalf("%s bounds = %q..%q", name, field.Shape.Minimum, field.Shape.Maximum)
		}
	}
	for name, kind := range map[string]Kind{"clusterRef": StorageCluster, "filesystemRef": StorageFilesystem, "poolLayoutRef": StoragePool} {
		if field, _ := schema.Field(name); !slices.Equal(field.Shape.Reference, []Kind{kind}) {
			t.Fatalf("%s must reference %s", name, kind)
		}
	}
	for _, name := range []string{"group", "mode"} {
		if field, _ := schema.Field(name); field.Shape.Type != String {
			t.Fatalf("%s must be a string", name)
		}
	}
}
