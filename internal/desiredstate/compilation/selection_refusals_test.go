package compilation_test

import (
	"context"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
)

type selectionRefusal struct {
	name, environment, probe string
	field, code, message     string
	remediation              string
	line, total              int
}

func selectionRefusals() []selectionRefusal {
	clusters := "apiVersion: bootwright.io/v1alpha1\nkind: OpenShiftCluster\nmetadata: {name: b}\nspec: {}\n---\napiVersion: bootwright.io/v1alpha1\nkind: OpenShiftCluster\nmetadata: {name: a}\nspec: {}\n"
	// The add-on is retained only through cluster a's binding, so its static
	// target is a cluster-retained reference to the excluded cluster b.
	excludedReferrer := "---\napiVersion: bootwright.io/v1alpha1\nkind: ClusterAddonBinding\nmetadata: {name: a-addons}\nspec: {clusterRef: a, addonRefs: [tune]}\n" +
		"---\napiVersion: bootwright.io/v1alpha1\nkind: ClusterAddon\nmetadata: {name: tune}\nspec:\n  manifestSet: {manifests: [{path: manifests/a.yaml}]}\n  steps:\n  - name: configure\n    gates: apply\n    playbook: configure.yaml\n    target: {static: {clusters: [b]}}\n"
	return []selectionRefusal{
		{"unresolved cluster", environmentYAML + "  containerClusters: [missing]\n", clusters,
			"$.spec.containerClusters[0]", "api.reference", "no OpenShiftCluster named missing is declared", "select one of: a, b", 9, 0},
		{"cluster entry that is not a name", environmentYAML + "  containerClusters: [" + refusalSentinel + ".]\n", "",
			"$.spec.containerClusters[0]", "api.reference", "cluster selection entry is not a cluster name", "declare the OpenShiftCluster or remove the entry", 9, 1},
		{"reference to an excluded root", environmentYAML + "  containerClusters: [a]\n", clusters + excludedReferrer,
			"$.spec.steps[0].target.static.clusters[0]", "api.reference", "OpenShiftCluster/b is excluded by the Environment's cluster selection", "add b to Environment spec.containerClusters, or correct the reference", 25, 0},
		{"empty cluster list", environmentYAML + "  containerClusters: []\n", "",
			"$.spec.containerClusters", "api.value", "list must contain at least one entry", "add at least one entry, or omit the field", 9, 1},
	}
}

func TestSelectionRefusalsPointAtTheEntry(t *testing.T) {
	var golden strings.Builder
	for _, row := range selectionRefusals() {
		t.Run(row.name, func(t *testing.T) {
			_, _, err := refusalCompiler().Compile(context.Background(), refusalSources(refusalRow{environment: row.environment, probe: row.probe}))
			found := diagnostics.Of(err)
			at := []diagnostics.Diagnostic{}
			for _, d := range found {
				golden.WriteString(row.name + ": " + refusalLine(d) + "\n")
				if list, _, _ := strings.Cut(row.field, "["); strings.HasPrefix(d.Field, list) {
					at = append(at, d)
				}
				if strings.Contains(d.Message+"; "+d.Remediation, refusalSentinel) {
					t.Errorf("%s at %s repeats authored text", d.Code, d.Field)
				}
			}
			if row.total != 0 && len(found) != row.total {
				t.Errorf("%d diagnostics, want %d: %+v", len(found), row.total, found)
			}
			if len(at) != 1 {
				t.Fatalf("diagnostics at the selection field = %+v, want one at %s", at, row.field)
			}
			got := at[0]
			if got.Field != row.field || got.Code != row.code || got.Message != row.message || got.Remediation != row.remediation || got.Source == nil || got.Source.Line != row.line {
				t.Fatalf("diagnostic = %s, want %s %s at line %d: %s; next: %s", refusalLine(got), row.code, row.field, row.line, row.message, row.remediation)
			}
		})
	}
	matchesTextGolden(t, "selection-refusals", []byte(golden.String()))
}
