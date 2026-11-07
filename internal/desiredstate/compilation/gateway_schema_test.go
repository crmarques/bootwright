package compilation_test

import (
	"slices"
	"testing"
)

func TestGatewayDerivedPoolNameCollidesWithDeclaredPoolOnCompile(t *testing.T) {
	onProbe, _ := strictYAMLCompile(t, "apiVersion: bootwright.io/v1alpha1\nkind: CephPool\nmetadata:\n  name: objects.rgw.meta\nspec:\n  clusterRef: missing\n  role: rgw\n")
	if !slices.Contains(onProbe, "api.value $.metadata.name") {
		t.Fatalf("a CephPool carried a derived zone pool name: %v", onProbe)
	}
}
