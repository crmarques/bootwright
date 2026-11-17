package clusterobjects

import (
	"slices"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

// formerAdapterRule is the exact comparison the collection's adapter once
// decided for itself: a Subscription's spec, and an RBAC object's subjects
// and rules. Go now emits the paths and the adapter only applies them.
func formerAdapterRule(kind, member string) bool {
	return kind == "Subscription" && member == "spec" ||
		slices.Contains([]string{"Role", "ClusterRole", "RoleBinding", "ClusterRoleBinding"}, kind) && (member == "subjects" || member == "rules")
}

func TestEachObjectEntryNamesTheMembersTheAdapterComparedExactly(t *testing.T) {
	members := []string{"spec", "subjects", "rules", "roleRef", "data", "aggregationRule"}
	for _, kind := range []string{"Subscription", "Role", "ClusterRole", "RoleBinding", "ClusterRoleBinding", "ConfigMap", "OperatorGroup", "Namespace"} {
		ref := Ref{APIVersion: "example.test/v1", Kind: kind, Namespace: "operators", Name: "sample", Namespaced: true}
		fields := []api.FieldValue{{Name: "apiVersion", Value: api.StringValue(ref.APIVersion)}, {Name: "kind", Value: api.StringValue(kind)},
			{Name: "metadata", Value: api.MapValue(api.FieldValue{Name: "name", Value: api.StringValue(ref.Name)}, api.FieldValue{Name: "namespace", Value: api.StringValue(ref.Namespace)})}}
		for _, member := range members {
			fields = append(fields, api.FieldValue{Name: member, Value: api.MapValue(api.FieldValue{Name: "x", Value: api.StringValue("y")})})
		}
		entry, err := NewObjectEntry(Object{Ref: ref, Body: api.MapValue(fields...)})
		if err != nil {
			t.Fatalf("%s: %v", kind, err)
		}
		var want []string
		for _, member := range members {
			if formerAdapterRule(kind, member) {
				want = append(want, member)
			}
		}
		slices.Sort(want)
		if !slices.Equal(entry.Exact, want) {
			t.Errorf("%s: exact %v, want %v", kind, entry.Exact, want)
		}
		if len(want) > 0 {
			entry.Exact = nil
			if validateObject(entry) == nil {
				t.Errorf("%s: an entry that drops its exact members validated", kind)
			}
		}
	}
}
