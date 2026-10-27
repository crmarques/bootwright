package compilation_test

import (
	"context"
	"slices"
	"testing"
)

func TestGatewayDerivedPoolNameCollidesWithDeclaredPoolOnCompile(t *testing.T) {
	onProbe, _ := strictYAMLCompile(t, "apiVersion: bootwright.io/v1alpha1\nkind: CephPool\nmetadata:\n  name: objects.rgw.meta\nspec:\n  clusterRef: missing\n  role: rgw\n")
	if !slices.Contains(onProbe, "api.value $.metadata.name") {
		t.Fatalf("a CephPool carried a derived zone pool name: %v", onProbe)
	}
}

func TestGatewayRetiredFieldsNameReplacementsOnDecode(t *testing.T) {
	const field = "$.spec.serviceID"
	probe := "apiVersion: bootwright.io/v1alpha1\nkind: CephObjectGateway\nmetadata:\n  name: probe\nspec:\n  clusterRef: missing\n  serviceRef: rgw.probe\n  realm: probe\n  zoneGroup: probe\n  zone: probe\n  serviceID: probe\n"
	state, report, err := regressionCompiler().Compile(context.Background(), sources(environmentYAML+"---\n"+probe))
	sink := requireCompilationFailure(t, state, report, err)
	if len(sink) != 1 || sink[0].Code != "api.field" || sink[0].Field != field || sink[0].Remediation != "replace "+field+" with spec.serviceRef naming the rgw.<service_id> entry of its CephCluster's spec.services" {
		t.Fatalf("retired gateway serviceID lost its replacement: %+v", sink)
	}
}
