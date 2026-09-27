package installation

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"go.yaml.in/yaml/v3"

	"github.com/crmarques/bootwright/ansible"
)

// roleSpecification is the argument specification the installation role
// checks every frozen request against before its first task runs.
const roleSpecification = "collections/ansible_collections/bootwright/core/roles/managedos_install_anaconda/meta/argument_specs.yml"

// specOption is one option of a role argument specification, reduced to what
// the installation role's specification uses.
type specOption struct {
	Type     string                `yaml:"type"`
	Required bool                  `yaml:"required"`
	Choices  []string              `yaml:"choices"`
	Options  map[string]specOption `yaml:"options"`
}

// The role refuses, before its first task, a request its argument
// specification does not admit: a version it does not list, a required option
// the request lacks, or a key that a dict declaring options does not declare.
// The role's own tests prove only that it agrees with itself, and this
// package's tests prove only what it freezes, so a role left at an earlier
// request version, or a budget field renamed on one side only, would fail
// every apply, observe and destroy on a real host while both sides stay green.
// Each frozen request golden is therefore checked against every entry point
// the capability runs.
func TestTheRoleAdmitsEveryRequestThisBuildFreezes(t *testing.T) {
	data, ok := ansible.Assets()[roleSpecification]
	if !ok {
		t.Fatalf("the embedded collection carries no %s", roleSpecification)
	}
	var document struct {
		ArgumentSpecs map[string]struct {
			Options map[string]specOption `yaml:"options"`
		} `yaml:"argument_specs"`
	}
	if err := yaml.Unmarshal(data, &document); err != nil {
		t.Fatalf("%s: %v", roleSpecification, err)
	}
	goldens, err := filepath.Glob(filepath.Join("testdata", "request-*.golden"))
	if err != nil || len(goldens) == 0 {
		t.Fatalf("no request goldens to check (%v)", err)
	}
	variable := variablePrefix + "_request"
	for _, operation := range []string{"apply", "observe", "destroy"} {
		request, ok := document.ArgumentSpecs[operation].Options[variable]
		if !ok {
			t.Fatalf("the role's %s entry point declares no %s", operation, variable)
		}
		for _, golden := range goldens {
			raw, err := os.ReadFile(golden)
			if err != nil {
				t.Fatal(err)
			}
			decoder := json.NewDecoder(bytes.NewReader(raw))
			decoder.UseNumber()
			var value any
			if err := decoder.Decode(&value); err != nil {
				t.Fatalf("%s: %v", golden, err)
			}
			for _, problem := range request.refusals(variable, value) {
				t.Errorf("the role's %s entry point refuses %s: %s", operation, filepath.Base(golden), problem)
			}
		}
	}
}

// refusals lists why Ansible's argument validation would refuse value for
// this option. It applies the checks the role's specification relies on, each
// at least as strictly as Ansible: the type, every required option, the
// admitted choices, and no key a dict with declared options does not declare.
func (o specOption) refusals(path string, value any) []string {
	var found []string
	switch o.Type {
	case "dict":
		fields, ok := value.(map[string]any)
		if !ok {
			return []string{fmt.Sprintf("%s is %T, not a dict", path, value)}
		}
		if o.Options == nil {
			return nil
		}
		for _, name := range slices.Sorted(maps.Keys(fields)) {
			if _, declared := o.Options[name]; !declared {
				found = append(found, path+"."+name+" is not an option the role declares")
			}
		}
		for _, name := range slices.Sorted(maps.Keys(o.Options)) {
			child := o.Options[name]
			field, present := fields[name]
			if !present {
				if child.Required {
					found = append(found, path+"."+name+" is required and absent")
				}
				continue
			}
			found = append(found, child.refusals(path+"."+name, field)...)
		}
		return found
	case "str":
		text, ok := value.(string)
		if !ok {
			return []string{fmt.Sprintf("%s is %T, not a string", path, value)}
		}
		if len(o.Choices) > 0 && !slices.Contains(o.Choices, text) {
			return []string{fmt.Sprintf("%s is %q, and the role admits only %q", path, text, o.Choices)}
		}
		return nil
	case "int":
		if number, ok := value.(json.Number); !ok {
			found = append(found, fmt.Sprintf("%s is %T, not an integer", path, value))
		} else if _, err := number.Int64(); err != nil {
			found = append(found, fmt.Sprintf("%s is %s, not an integer", path, number))
		}
	case "bool":
		if _, ok := value.(bool); !ok {
			found = append(found, fmt.Sprintf("%s is %T, not a boolean", path, value))
		}
	default:
		return []string{path + " declares type " + o.Type + ", which this check does not know"}
	}
	if len(o.Choices) > 0 {
		found = append(found, path+" declares choices on type "+o.Type+", which this check does not know")
	}
	return found
}
