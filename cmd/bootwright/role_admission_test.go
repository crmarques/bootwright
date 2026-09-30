//go:build linux && amd64

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"

	"github.com/crmarques/bootwright/ansible"
	"github.com/crmarques/bootwright/internal/machine/power"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

const roleRoot = "collections/ansible_collections/bootwright/core/roles/"

// roleOption is one option of a role argument specification, reduced to the
// keys the collection's specifications use.
type roleOption struct {
	Type     string                `yaml:"type"`
	Required bool                  `yaml:"required"`
	Choices  []string              `yaml:"choices"`
	Elements string                `yaml:"elements"`
	Options  map[string]roleOption `yaml:"options"`
}

// roleEntryPoint is the role and entry point the playbook bound to one
// operation imports, with the options that entry point declares.
func roleEntryPoint(playbook string) (string, map[string]roleOption, error) {
	var plays []struct {
		Tasks []map[string]any `yaml:"tasks"`
	}
	if err := yaml.Unmarshal(ansible.Assets()[playbookRoot+playbook], &plays); err != nil || len(plays) != 1 {
		return "", nil, fmt.Errorf("%s is not one play (%v)", playbook, err)
	}
	for _, task := range plays[0].Tasks {
		imported, ok := task["ansible.builtin.import_role"].(map[string]any)
		if !ok {
			continue
		}
		role := strings.TrimPrefix(fmt.Sprint(imported["name"]), "bootwright.core.")
		entry := strings.TrimSuffix(fmt.Sprint(imported["tasks_from"]), ".yml")
		var document struct {
			ArgumentSpecs map[string]struct {
				Options map[string]roleOption `yaml:"options"`
			} `yaml:"argument_specs"`
		}
		if err := yaml.Unmarshal(ansible.Assets()[roleRoot+role+"/meta/argument_specs.yml"], &document); err != nil {
			return "", nil, fmt.Errorf("%s: %v", role, err)
		}
		declared, ok := document.ArgumentSpecs[entry]
		if !ok {
			return "", nil, fmt.Errorf("%s declares no entry point %s", role, entry)
		}
		return role + "/" + entry, declared.Options, nil
	}
	return "", nil, fmt.Errorf("%s imports no role", playbook)
}

// refusals lists why Ansible's argument validation would refuse value for this
// option: its type, a required option it lacks, a choice it does not admit, a
// key a dict declaring options does not declare, and each element of a list.
// Each check is at least as strict as Ansible's, which converts some values
// between types rather than refusing them.
func (o roleOption) refusals(path string, value any) []string {
	switch o.Type {
	case "raw":
		return nil
	case "dict":
		fields, ok := value.(map[string]any)
		if !ok {
			return []string{fmt.Sprintf("%s is %T, not a dict", path, value)}
		}
		var found []string
		for _, name := range slices.Sorted(maps.Keys(fields)) {
			if o.Options == nil {
				break
			}
			if _, declared := o.Options[name]; !declared {
				found = append(found, path+"."+name+" is not an option the role declares")
			}
		}
		for _, name := range slices.Sorted(maps.Keys(o.Options)) {
			field, present := fields[name]
			if !present {
				if o.Options[name].Required {
					found = append(found, path+"."+name+" is required and absent")
				}
				continue
			}
			found = append(found, o.Options[name].refusals(path+"."+name, field)...)
		}
		return found
	case "list":
		items, ok := value.([]any)
		if !ok {
			return []string{fmt.Sprintf("%s is %T, not a list", path, value)}
		}
		var found []string
		for index, item := range items {
			if o.Elements != "" {
				element := roleOption{Type: o.Elements, Options: o.Options}
				found = append(found, element.refusals(fmt.Sprintf("%s[%d]", path, index), item)...)
			}
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
			return []string{fmt.Sprintf("%s is %T, not an integer", path, value)}
		} else if _, err := number.Int64(); err != nil {
			return []string{fmt.Sprintf("%s is %s, not an integer", path, number)}
		}
		return nil
	case "bool":
		if _, ok := value.(bool); !ok {
			return []string{fmt.Sprintf("%s is %T, not a boolean", path, value)}
		}
		return nil
	}
	return []string{path + " declares type " + o.Type + ", which this check does not know"}
}

// decodedRequest reads a frozen request's canonical bytes with every number
// kept as its text, so an integer is told from a fraction.
func decodedRequest(canonical []byte) (any, error) {
	decoder := json.NewDecoder(bytes.NewReader(canonical))
	decoder.UseNumber()
	var value any
	return value, decoder.Decode(&value)
}

// roleRefusals lists why the entry point the playbook bound to one run would
// refuse what the runner hands it: the frozen request under the run's own
// variable, checked against that entry point's argument specification, and a
// material mapping it declares.
func roleRefusals(request lifecycle.RunRequest) []string {
	playbook, bound := operationPlaybook()[request.Implementation+"/"+request.Operation]
	if !bound {
		return []string{request.Implementation + "/" + request.Operation + " is bound to no playbook"}
	}
	entry, options, err := roleEntryPoint(playbook)
	if err != nil {
		return []string{err.Error()}
	}
	declared, ok := options[request.Variable+"_request"]
	if !ok || declared.Type != "dict" || !declared.Required {
		return []string{entry + " does not require the dict " + request.Variable + "_request"}
	}
	if material, ok := options[request.Variable+"_material"]; !ok || material.Type != "dict" {
		return []string{entry + " does not declare the dict " + request.Variable + "_material"}
	}
	value, err := decodedRequest(request.Canonical)
	if err != nil {
		return []string{"the frozen request is not one JSON document: " + err.Error()}
	}
	var found []string
	for _, problem := range declared.refusals(request.Variable+"_request", value) {
		found = append(found, entry+" refuses it: "+problem)
	}
	return found
}

// A role's argument specification and its version assertion are what refuse a
// request before its first task, so a role left at an earlier request version,
// or a field renamed on one side only, passes every other gate and fails only
// when an adapter runs. The capability contract suite holds every request a
// lifecycle capability sends to its entry point through roleRefusals; these
// are the refusals it reports.
func TestRoleRefusalsNameEachRequestAnEntryPointWouldRefuse(t *testing.T) {
	options := map[string]roleOption{"example_request": {Type: "dict", Required: true, Options: map[string]roleOption{
		"version": {Type: "str", Required: true, Choices: []string{"example-v2"}},
		"budget":  {Type: "int", Required: true},
		"verify":  {Type: "bool", Required: false},
		"nodes":   {Type: "list", Elements: "dict", Required: true, Options: map[string]roleOption{"name": {Type: "str", Required: true}}},
		"native":  {Type: "raw"},
	}}}
	sound := `{"budget":30,"native":[1,"x"],"nodes":[{"name":"a"}],"verify":true,"version":"example-v2"}`
	for canonical, want := range map[string]string{
		sound: "",
		`{"budget":30,"nodes":[],"version":"example-v1"}`:                           `version is "example-v1", and the role admits only ["example-v2"]`,
		`{"budget":30,"nodes":[],"version":"example-v2","extra":1}`:                 "extra is not an option the role declares",
		`{"nodes":[],"version":"example-v2"}`:                                       "budget is required and absent",
		`{"budget":1.5,"nodes":[],"version":"example-v2"}`:                          "budget is 1.5, not an integer",
		`{"budget":"30","nodes":[],"version":"example-v2"}`:                         "budget is string, not an integer",
		`{"budget":30,"nodes":[],"verify":"yes","version":"example-v2"}`:            "verify is string, not a boolean",
		`{"budget":30,"nodes":[{"name":"a","role":"x"}],"version":"example-v2"}`:    "nodes[0].role is not an option the role declares",
		`{"budget":30,"nodes":[{}],"version":"example-v2"}`:                         "nodes[0].name is required and absent",
		`{"budget":30,"nodes":{"name":"a"},"version":"example-v2"}`:                 "nodes is map[string]interface {}, not a list",
		`{"budget":30,"nodes":[],"version":"example-v2","verify":true,"native":{}}`: "",
	} {
		value, err := decodedRequest([]byte(canonical))
		if err != nil {
			t.Fatal(err)
		}
		found := strings.Join(options["example_request"].refusals("example_request", value), "; ")
		if want == "" && found != "" || want != "" && !strings.Contains(found, "example_request."+want) {
			t.Errorf("%s: refusals %q, want %q", canonical, found, want)
		}
	}
	if found := (roleOption{Type: "path"}).refusals("example", "x"); len(found) != 1 {
		t.Errorf("an option type the check does not know passed: %v", found)
	}
}

// The day-2 power run and the power reading are bound without a lifecycle
// capability, so the contract suite never sends them. Each entry point is
// checked against the request goldens its package pins as the exact bytes this
// build sends, and each version the goldens carry is the one this build names.
func TestThePowerRolesAdmitTheRequestsThisBuildSends(t *testing.T) {
	for _, bound := range []struct {
		implementation, operation, variable, version string
		goldens                                      []string
	}{
		{power.Implementation, "power", power.Variable, power.Implementation, []string{"request-power", "request-power-bundle"}},
		{power.ReadImplementation, "read", power.ReadVariable, power.ReadImplementation, []string{"survey-read", "survey-read-bundles"}},
	} {
		for _, name := range bound.goldens {
			golden, err := os.ReadFile(filepath.Join("..", "..", "internal", "machine", "power", "testdata", name+".golden"))
			if err != nil {
				t.Fatal(err)
			}
			var compact bytes.Buffer
			if err := json.Compact(&compact, golden); err != nil {
				t.Fatal(err)
			}
			if !bytes.Contains(compact.Bytes(), []byte(`"version":"`+bound.version+`"`)) {
				t.Errorf("%s does not carry the version %s this build names", name, bound.version)
			}
			request := lifecycle.RunRequest{Implementation: bound.implementation, Operation: bound.operation,
				Variable: bound.variable, Canonical: compact.Bytes()}
			for _, problem := range roleRefusals(request) {
				t.Errorf("%s: %s", name, problem)
			}
		}
	}
}
