package v1alpha1

import "testing"

func TestStorageWireShapesAreClosedAndVariantsAreExplicit(t *testing.T) {
	for _, kind := range []Kind{StorageCluster, StoragePlacementPolicy, StoragePool, StorageClient, StorageFilesystem, StorageObjectGateway, StorageNFSExport, StorageExport} {
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
	for _, serviceType := range []string{"mon", "mgr", "osd"} {
		shape := StorageNativeServiceSpecSchema(serviceType)
		if shape == nil || shape.Open {
			t.Fatalf("native core %s lacks a closed payload", serviceType)
		}
		checkStorageShape(t, shape)
	}
	if StorageNativeServiceSpecSchema("rgw") != nil {
		t.Fatal("future family schema must be qualified by its owning slice")
	}
	client := Schema(StorageClient)
	pool, _ := client.Field("poolRef")
	access, _ := client.Field("access")
	if !pool.Required || pool.Shape.Reference[0] != StoragePool || !access.Required || len(access.Shape.Enums) != 2 {
		t.Fatal("minimal client must bind one pool and explicit bounded access")
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
