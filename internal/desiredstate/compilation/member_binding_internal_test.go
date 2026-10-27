package compilation

import (
	"context"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/environment"
)

// One Machine bound by two cluster nodes is refused at the field the owning
// domain's membership names, whatever arm declares the nodes.
func TestAMachineBoundByTwoClusterNodesIsRefusedAtTheMembershipField(t *testing.T) {
	members := func(object api.Object) (environment.Membership, bool) {
		if object.Kind() != api.StorageCluster {
			return environment.Membership{}, false
		}
		return environment.MembersOf("$.spec.array.nodes", object.Spec().Get("array", "nodes")), true
	}
	cluster := func(name string) *objectRecord {
		node := api.MapValue(api.FieldValue{Name: "machineRef", Value: api.StringValue("shared")})
		spec := api.MapValue(api.FieldValue{Name: "array", Value: api.MapValue(api.FieldValue{Name: "nodes", Value: api.ListValue(node)})})
		return &objectRecord{object: api.NewObject(api.StorageCluster, name, api.Value{}, spec)}
	}
	for _, test := range []struct {
		records []*objectRecord
		refused int
	}{{[]*objectRecord{cluster("one"), cluster("two")}, 2}, {[]*objectRecord{cluster("one")}, 0}} {
		sink := newDiagnostics(context.Background())
		validateIdentities(test.records, sink, members)
		if len(sink.items) != test.refused {
			t.Fatalf("refused %d nodes: %+v", len(sink.items), sink.items)
		}
		for _, item := range sink.items {
			if item.Field != "$.spec.array.nodes" || item.Message != "a Machine may be bound by only one selected cluster node" {
				t.Fatalf("refusal = %+v", item)
			}
		}
	}
}
