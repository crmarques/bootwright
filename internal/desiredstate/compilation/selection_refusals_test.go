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
	const literal = "resource paths are literal; glob patterns are not expanded"
	const skipped = "resource path is inside a directory discovery skips (dot-prefixed, vendor, node_modules, playbooks, roles, collections, manifests or secrets)"
	const remedy = "list a .yaml or .yml file, or a directory holding them, below the Environment's directory, and include it in the input you pass"
	secret := refusalSecret + "  type: opaque\n"
	clusters := "apiVersion: bootwright.io/v1alpha1\nkind: ContainerCluster\nmetadata: {name: b}\nspec: {}\n---\napiVersion: bootwright.io/v1alpha1\nkind: ContainerCluster\nmetadata: {name: a}\nspec: {}\n"
	return []selectionRefusal{
		{"typo in the second entry", environmentYAML + "  resources:\n  - environment.yaml\n  - envronment.yaml\n", "",
			"$.spec.resources[1]", "api.reference", "resource path selects no discovered desired-state file", remedy, 11, 1},
		{"empty resource list", environmentYAML + "  resources: []\n", secret,
			"$.spec.resources", "api.value", "list must contain at least one entry", "add at least one entry, or omit the field", 9, 1},
		{"identical repeated entry", environmentYAML + "  resources: [environment.yaml, environment.yaml]\n", "",
			"$.spec.resources[1]", "api.duplicate", "entry repeats an earlier entry", "remove or rename the repeated entry", 9, 1},
		{"entry repeated after cleaning", environmentYAML + "  resources: [environment.yaml, ./environment.yaml]\n", "",
			"$.spec.resources[1]", "api.duplicate", "resource path repeats entry [0] after cleaning", "remove the repeated entry", 9, 1},
		{"entry outside the directory", environmentYAML + "  resources: [environment.yaml, ../environment.yaml]\n", "",
			"$.spec.resources[1]", "api.value", "resource path must stay inside the Environment directory", "use a relative path below the Environment's directory", 9, 1},
		{"dot-prefixed directory", environmentYAML + "  resources: [environment.yaml, .hidden/x.yaml]\n", "",
			"$.spec.resources[1]", "api.reference", skipped, remedy, 9, 1},
		{"payload directory", environmentYAML + "  resources: [environment.yaml, secrets/x.yaml]\n", "",
			"$.spec.resources[1]", "api.reference", skipped, remedy, 9, 1},
		{"glob pattern", environmentYAML + "  resources: [environment.yaml, infra/*.yaml]\n", "",
			"$.spec.resources[1]", "api.reference", literal, remedy, 9, 1},
		{"unresolved cluster", environmentYAML + "  containerClusters: [missing]\n", clusters,
			"$.spec.containerClusters[0]", "api.reference", "no ContainerCluster named missing is declared", "select one of: a, b", 9, 0},
		{"cluster entry that is not a name", environmentYAML + "  containerClusters: [" + refusalSentinel + ".]\n", "",
			"$.spec.containerClusters[0]", "api.reference", "cluster selection entry is not a cluster name", "declare the ContainerCluster or remove the entry", 9, 1},
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
