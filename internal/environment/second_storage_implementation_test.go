package environment_test

import (
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/environment"
)

// fakeArmMembers is everything a second StorageCluster implementation adds for
// selection and admission: its own membership, read from its own arm.
func fakeArmMembers(object api.Object) (environment.Membership, bool) {
	if object.Kind() != api.StorageCluster {
		return environment.Membership{}, false
	}
	return environment.MembersOf("$.spec.array.nodes", object.Spec().Get("array", "nodes")), true
}

func fakeArmStorage(name string, machines ...string) api.Object {
	nodes := []api.Value{}
	for _, machine := range machines {
		nodes = append(nodes, selectionValue("name", machine, "machineRef", machine))
	}
	return selectionObject(api.StorageCluster, name, "array", selectionValue("nodes", api.ListValue(nodes...)))
}

// A second StorageCluster implementation, whose nodes live under its own arm,
// is selected and admitted by its published membership alone: selection keeps
// its node Machines and admission keeps the controller out of them, with no
// edit to the environment package.
func TestASecondStorageArmPassesSelectionAndAdmissionThroughItsMembership(t *testing.T) {
	objects := selectionFixture(selectionEnvironment("env", "storageClusters", []string{"array-1"}),
		fakeArmStorage("array-1", "array-node"), selectionObject(api.Machine, "array-node"))
	kept := selectionIdentities(environment.Select(api.NewCatalog(objects), nil, fakeArmMembers).Catalog.Objects())
	if !slices.Contains(kept, "StorageCluster/array-1") || !slices.Contains(kept, "Machine/array-node") {
		t.Fatalf("the second arm's node Machine was dropped: %v", kept)
	}
	if kept := selectionIdentities(environment.Select(api.NewCatalog(objects), nil, testClusterMembers).Catalog.Objects()); slices.Contains(kept, "Machine/array-node") {
		t.Fatalf("membership read from another arm kept the node: %v", kept)
	}
	env, controller := controllerEnvironment(), controllerMachine(true, true, true)
	catalog := api.NewCatalog([]api.Object{env, controller, fakeArmStorage("array-1", "ctl")})
	issues := environment.Validate(env, catalog, fakeArmMembers)
	if len(issues) != 1 || !strings.Contains(issues[0].Message, "controller must remain outside cluster node membership; it is a node of StorageCluster/array-1") {
		t.Fatalf("admission = %+v", issues)
	}
}
