package agentinstall

import (
	"bytes"
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"go.yaml.in/yaml/v3"

	"github.com/crmarques/bootwright/ansible"
)

// roleSpecifications are the argument specifications each block's role checks
// every frozen request against before its first task runs.
var roleSpecifications = map[string]string{
	"media":   "collections/ansible_collections/bootwright/core/roles/containercluster_media_agent/meta/argument_specs.yml",
	"install": "collections/ansible_collections/bootwright/core/roles/containercluster_install_agent/meta/argument_specs.yml",
}

// roleOption is one option of a role argument specification, reduced to what
// these roles' specifications use.
type roleOption struct {
	Type     string                `yaml:"type"`
	Required bool                  `yaml:"required"`
	Choices  []string              `yaml:"choices"`
	Options  map[string]roleOption `yaml:"options"`
}

// A role refuses, before its first task, a request whose version it does not
// list, that lacks a required option, or that carries a key a dict declaring
// options does not declare. Moving a budget between the two requests changes
// both, so a role left at the earlier version, or a budget declared on one
// side only, would fail every apply, observe and destroy on a real host while
// the goldens and the role's own tests stay green. Each request golden is
// therefore checked against every entry point its capability runs: its keys,
// its version, and its budgets, each an integer the role requires.
func TestTheRolesAdmitEveryRequestThisBuildFreezes(t *testing.T) {
	for block, path := range roleSpecifications {
		data, ok := ansible.Assets()[path]
		if !ok {
			t.Fatalf("the embedded collection carries no %s", path)
		}
		var document struct {
			ArgumentSpecs map[string]struct {
				Options map[string]roleOption `yaml:"options"`
			} `yaml:"argument_specs"`
		}
		if err := yaml.Unmarshal(data, &document); err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		goldens, err := filepath.Glob(filepath.Join("testdata", block+"-request-*.golden"))
		if err != nil || len(goldens) == 0 {
			t.Fatalf("no %s request goldens to check (%v)", block, err)
		}
		variable := "bootwright_cluster_" + block + "_request"
		for _, operation := range operations {
			request, ok := document.ArgumentSpecs[operation].Options[variable]
			if !ok || request.Type != "dict" || !request.Required {
				t.Fatalf("the %s role's %s entry point does not require the dict %s", block, operation, variable)
			}
			for _, golden := range goldens {
				for _, problem := range refusedBy(t, request, golden) {
					t.Errorf("the %s role's %s entry point refuses %s: %s", block, operation, filepath.Base(golden), problem)
				}
			}
		}
	}
}

// refusedBy lists why the role's validation would refuse one golden request.
func refusedBy(t *testing.T, request roleOption, golden string) []string {
	t.Helper()
	raw, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var fields map[string]any
	if err := decoder.Decode(&fields); err != nil {
		t.Fatalf("%s: %v", golden, err)
	}
	var found []string
	for _, name := range slices.Sorted(maps.Keys(fields)) {
		if _, declared := request.Options[name]; !declared {
			found = append(found, name+" is not an option the role declares")
		}
	}
	for _, name := range slices.Sorted(maps.Keys(request.Options)) {
		if _, present := fields[name]; !present && request.Options[name].Required {
			found = append(found, name+" is required and absent")
		}
	}
	if version, _ := fields["version"].(string); !slices.Contains(request.Options["version"].Choices, version) {
		found = append(found, "version "+version+" is not one the role admits")
	}
	budgets, _ := fields["budgets"].(map[string]any)
	declared := request.Options["budgets"]
	if declared.Type != "dict" || !declared.Required {
		found = append(found, "budgets is not a dict the role requires")
	}
	if !slices.Equal(slices.Sorted(maps.Keys(budgets)), slices.Sorted(maps.Keys(declared.Options))) {
		found = append(found, "budgets are not exactly the ones the role declares")
	}
	for name, option := range declared.Options {
		number, isNumber := budgets[name].(json.Number)
		if _, err := number.Int64(); !isNumber || err != nil || option.Type != "int" || !option.Required {
			found = append(found, "budgets."+name+" is not an integer the role requires")
		}
	}
	return found
}
