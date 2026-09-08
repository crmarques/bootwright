package v1alpha1

import "testing"

func TestStorageWireShapesAreClosedAndVariantsAreExplicit(t *testing.T) {
	for _, kind := range []Kind{StorageCluster, StoragePlacementPolicy, StoragePool, StorageFilesystem, StorageObjectGateway, StorageNFSExport, StorageExport} {
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
