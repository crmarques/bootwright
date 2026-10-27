package compilation_test

import (
	"context"
	"testing"
)

func TestCephadmWorkaroundsRetiredOnDecode(t *testing.T) {
	const message = "the cephadm workaround list was retired; Bootwright realizes no cephadm workaround"
	for _, row := range []struct{ name, input, field, remedy string }{
		{"cluster", environmentYAML + "---\napiVersion: bootwright.io/v1alpha1\nkind: CephCluster\nmetadata: {name: storage}\nspec:\n  cephadm:\n    workarounds: [mgmt-gateway-spec-dependency-recording]\n", "$.spec.cephadm.workarounds", "remove workarounds from $.spec.cephadm"},
		{"default", environmentYAML + "  defaults:\n    CephCluster:\n      cephadm:\n        workarounds: []\n", "$.spec.defaults.CephCluster.cephadm.workarounds", "remove workarounds from $.spec.defaults.CephCluster.cephadm"},
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
