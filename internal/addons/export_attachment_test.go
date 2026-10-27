package addons_test

import (
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/addons"
	"github.com/crmarques/bootwright/internal/addons/packages"
	"github.com/crmarques/bootwright/internal/environment"
)

func exportBinding(name, cluster string) api.Object {
	return api.NewObject(api.ClusterAddonBinding, name, api.Value{}, api.MapValue(
		api.FieldValue{Name: "clusterRef", Value: api.StringValue(cluster)},
		api.FieldValue{Name: "packages", Value: api.MapValue(api.FieldValue{Name: "fdf", Value: api.MapValue(
			api.FieldValue{Name: "storageExportRef", Value: api.StringValue("export")},
			api.FieldValue{Name: "timeout", Value: api.StringValue("1h")},
			api.FieldValue{Name: "subscription", Value: api.MapValue(api.FieldValue{Name: "source", Value: api.StringValue("redhat-operators")})},
		)})},
	))
}

func TestExportConsumerUniquenessUsesCanonicalBindingOrder(t *testing.T) {
	first, second := exportBinding("a", "first"), exportBinding("z", "second")
	for _, objects := range [][]api.Object{{second, first}, {first, second}} {
		c := api.NewCatalog(objects)
		if issues := addons.ValidatePackages(first, c, packages.Builtin(), addons.ClusterFacts{}); len(issues) != 0 {
			t.Fatal(issues)
		}
		issues := addons.ValidatePackages(second, c, packages.Builtin(), addons.ClusterFacts{})
		if len(issues) != 1 || issues[0].Code != "api.duplicate" || issues[0].Field != "$.spec.packages.fdf.storageExportRef" || issues[0].Message != "StorageExport/export is already consumed by package fdf of ClusterAddonBinding/a" {
			t.Fatal(issues)
		}
	}
}

func TestBuiltinExportClosureRetainsBothSidesAndResourceChain(t *testing.T) {
	object := func(kind api.Kind, name string, fields ...api.FieldValue) api.Object {
		return api.NewObject(kind, name, api.Value{}, api.MapValue(fields...))
	}
	clusterRef := api.FieldValue{Name: "clusterRef", Value: api.StringValue("ceph")}
	base := []api.Object{exportBinding("binding", "consumer"), object(api.ContainerCluster, "consumer"), object(api.ContainerCluster, "other-consumer"), object(api.StorageCluster, "ceph"), object(api.StorageCluster, "other-storage"), object(api.StoragePool, "pool", clusterRef), object(api.StorageFilesystem, "filesystem", clusterRef), object(api.StorageExport, "export", clusterRef, api.FieldValue{Name: "dataFoundation", Value: api.MapValue(api.FieldValue{Name: "rbdPoolRef", Value: api.StringValue("pool")}, api.FieldValue{Name: "filesystemRef", Value: api.StringValue("filesystem")})})}
	for _, tc := range []struct{ name, container, storage string }{{"container root", "consumer", "other-storage"}, {"storage root", "other-consumer", "ceph"}} {
		t.Run(tc.name, func(t *testing.T) {
			env := object(api.Environment, "environment", api.FieldValue{Name: "containerClusters", Value: api.StringList(tc.container)}, api.FieldValue{Name: "storageClusters", Value: api.StringList(tc.storage)})
			c := api.NewCatalog(append(append([]api.Object{}, base...), env))
			attachments := []environment.Attachment{}
			for _, a := range addons.StorageAttachments(c) {
				attachments = append(attachments, environment.Attachment{ClusterRef: a.ClusterRef, ExportRef: a.ExportRef})
			}
			selected := environment.Select(c, attachments, testClusterMembers).Catalog
			for _, ref := range []struct {
				kind api.Kind
				name string
			}{{api.ContainerCluster, "consumer"}, {api.StorageCluster, "ceph"}, {api.StorageExport, "export"}, {api.StoragePool, "pool"}, {api.StorageFilesystem, "filesystem"}} {
				if _, found := selected.Find(ref.kind, ref.name); !found {
					t.Fatalf("closure dropped %s/%s", ref.kind, ref.name)
				}
			}
		})
	}
}
