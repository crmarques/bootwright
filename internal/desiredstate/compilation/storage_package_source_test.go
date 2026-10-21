package compilation_test

import (
	"context"
	"strings"
	"testing"
)

func TestCephPackageSourceRefusesUnusedSubscriptionFields(t *testing.T) {
	for _, tc := range []struct {
		name, ceph, field, code, remedy string
	}{
		{"subscription source", "  ibm:\n    callHome: disabled\n    packages:\n      source: subscription\n", "$.spec.ceph.ibm.packages.source", "api.value", "vendor"},
		{"subscription entitlement", "  osSubscriptionRef: unused\n", "$.spec.ceph.osSubscriptionRef", "api.field", "remove"},
		{"subscription repositories", "  ibm:\n    callHome: disabled\n    packages:\n      source: vendor\n      subscriptionRepos: [unused]\n", "$.spec.ceph.ibm.packages.subscriptionRepos", "api.field", "remove"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			probe := "apiVersion: bootwright.io/v1alpha1\nkind: StorageCluster\nmetadata:\n  name: probe\nspec:\n  type: ceph\n  ceph:\n  " + strings.ReplaceAll(strings.TrimSuffix(tc.ceph, "\n"), "\n", "\n  ") + "\n"
			state, report, err := regressionCompiler().Compile(context.Background(), sources(environmentYAML+"---\n"+probe))
			found := false
			for _, d := range requireCompilationFailure(t, state, report, err) {
				if d.Field != tc.field {
					continue
				}
				if d.Code != tc.code || d.Object == nil || d.Object.Kind != "StorageCluster" || d.Object.Name != "probe" || d.Remediation == "" || !strings.Contains(d.Message+" "+d.Remediation, tc.remedy) {
					t.Fatalf("subscription declaration has no actionable refusal: %+v", d)
				}
				found = true
			}
			if !found {
				t.Fatalf("unused subscription declaration was admitted at %s", tc.field)
			}
		})
	}
}
