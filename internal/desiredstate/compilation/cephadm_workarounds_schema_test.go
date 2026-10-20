package compilation_test

import (
	"context"
	"testing"
)

func TestCephadmWorkaroundsRetiredOnDecode(t *testing.T) {
	const message = "the cephadm workaround list was retired; Bootwright realizes no cephadm workaround"
	for _, row := range []struct{ name, input, field, remedy string }{
		{"cluster", environmentYAML + "---\napiVersion: bootwright.io/v1alpha1\nkind: StorageCluster\nmetadata: {name: storage}\nspec:\n  type: ceph\n  ceph:\n    cephadm:\n      workarounds: [mgmt-gateway-spec-dependency-recording]\n", "$.spec.ceph.cephadm.workarounds", "remove workarounds from $.spec.ceph.cephadm"},
		{"default", environmentYAML + "  defaults:\n    StorageCluster:\n      ceph:\n        cephadm:\n          workarounds: []\n", "$.spec.defaults.StorageCluster.ceph.cephadm.workarounds", "remove workarounds from $.spec.defaults.StorageCluster.ceph.cephadm"},
	} {
		t.Run(row.name, func(t *testing.T) {
			state, report, err := regressionCompiler().Compile(context.Background(), sources(row.input))
			sink := requireCompilationFailure(t, state, report, err)
			if len(sink) != 1 || sink[0].Code != "api.field" || sink[0].Field != row.field || sink[0].Message != message || sink[0].Remediation != row.remedy {
				t.Fatalf("retired workarounds lost their reason: %+v", sink)
			}
		})
	}
}
