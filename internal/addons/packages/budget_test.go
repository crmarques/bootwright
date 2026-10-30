package packages

import (
	"testing"
	"time"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

func TestPackageTimeoutDefaultsEqualTheirBudgets(t *testing.T) {
	packages, ok := api.Schema(api.ClusterAddonBinding).Field("packages")
	if !ok {
		t.Fatal("the binding schema has no spec.packages")
	}
	registry := Builtin()
	for _, member := range packages.Shape.Fields {
		if _, found := registry.Find(member.Name); !found {
			t.Errorf("schema package %s is not in the registry", member.Name)
		}
	}
	for _, p := range registry.Packages() {
		member, ok := packages.Shape.Field(p.Name)
		if !ok {
			t.Errorf("registry package %s has no spec.packages member", p.Name)
			continue
		}
		timeout, ok := member.Shape.Field("timeout")
		if !ok {
			t.Errorf("package %s has no timeout in its schema", p.Name)
			continue
		}
		parsed, err := time.ParseDuration(timeout.Default.Text())
		if err != nil || parsed != p.Budget {
			t.Errorf("package %s schema timeout default %q is not its budget %s (%v)", p.Name, timeout.Default.Text(), p.Budget, err)
		}
	}
}
