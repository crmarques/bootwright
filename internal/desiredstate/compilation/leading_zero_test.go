package compilation_test

import (
	"context"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
)

func TestLeadingZeroIntegerRefusalSaysWhyAndWhatToWrite(t *testing.T) {
	for _, probe := range []struct{ name, probe, field string }{
		{"integer field", strictYAMLBytes("0100"), "$.spec.source.generated.bytes"},
		{"number field", strictYAMLRatio("0100"), "$.spec.autoscale.targetSizeRatio"},
		{"string field", strictYAMLSecret("  type: 0100\n"), "$.spec.type"},
		{"native map", strictYAMLPlaybook("{mode: 0644}"), "$.spec.extraVars"},
		{"label", "apiVersion: bootwright.io/v1alpha1\nkind: Secret\nmetadata:\n  name: probe\n  labels: {mode: 0644}\nspec:\n  type: opaque\n", "$.metadata.labels"},
		{"field with alternatives", "apiVersion: bootwright.io/v1alpha1\nkind: StorageFilesystem\nmetadata:\n  name: probe\nspec:\n  clusterRef: missing\n  dataPoolRefs: [0100]\n", "$.spec.dataPoolRefs[0]"},
	} {
		t.Run(probe.name, func(t *testing.T) {
			_, _, err := compiler().Compile(context.Background(), refusalSources(refusalRow{environment: environmentYAML, probe: probe.probe}))
			found := diagnostics.Of(err)
			if len(found) != 1 || found[0].Code != "api.type" || found[0].Field != probe.field ||
				found[0].Message != "a multi-digit integer must not start with 0; YAML 1.1 readers read it as octal" ||
				found[0].Remediation != "remove the leading zero, or quote the value where the field takes a string" {
				t.Fatalf("diagnostics = %+v, want one leading-zero refusal at %s", found, probe.field)
			}
		})
	}
}
