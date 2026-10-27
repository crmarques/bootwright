package main

import (
	"slices"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

// Composition binds the membership of every cluster kind, each read by the
// domain that owns it, so selection, admission and compilation find every
// node Machine without reading a field path themselves.
func TestCompositionBindsEveryClusterKindsMembership(t *testing.T) {
	node := api.MapValue(api.FieldValue{Name: "name", Value: api.StringValue("node-1")}, api.FieldValue{Name: "machineRef", Value: api.StringValue("machine-1")})
	nodes := api.ListValue(node)
	container := api.NewObject(api.ContainerCluster, "cluster", api.Value{}, api.MapValue(api.FieldValue{Name: "nodes", Value: nodes}))
	storageCluster := api.NewObject(api.StorageCluster, "storage", api.Value{}, api.MapValue(api.FieldValue{Name: "ceph", Value: api.MapValue(
		api.FieldValue{Name: "topology", Value: api.MapValue(api.FieldValue{Name: "nodes", Value: nodes})})}))
	members := clusterMembers()
	for _, row := range []struct {
		object api.Object
		field  string
	}{{container, "$.spec.nodes"}, {storageCluster, "$.spec.ceph.topology.nodes"}} {
		object, field := row.object, row.field
		membership, bound := members(object)
		if !bound || membership.Field != field || len(membership.Nodes) != 1 || membership.Nodes[0].Machine != "machine-1" || membership.Nodes[0].Name != "node-1" {
			t.Errorf("%s membership = %+v (%v)", object.Identity(), membership, bound)
		}
	}
	for _, kind := range api.Kinds() {
		if _, bound := members(api.NewObject(kind, "x", api.Value{}, api.MapValue())); bound && !slices.Contains([]api.Kind{api.ContainerCluster, api.StorageCluster}, kind) {
			t.Errorf("%s answered a membership", kind)
		}
	}
}
