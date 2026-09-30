package ansiblelocal

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"

	"github.com/crmarques/bootwright/ansible"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
)

const roleSpecification = "collections/ansible_collections/bootwright/core/roles/controller_prerequisites/meta/argument_specs.yml"

// specOption is one option of the role's argument specification, reduced to
// what that specification uses.
type specOption struct {
	Type     string                `yaml:"type"`
	Required bool                  `yaml:"required"`
	Choices  []string              `yaml:"choices"`
	Options  map[string]specOption `yaml:"options"`
}

// refusals lists why ansible-core, validating the role's main entry point
// before its first task, would refuse one request: an option it does not
// declare, a required one it lacks, a choice it does not admit, or a value of
// another type. None of the declared options declares options of its own, so
// the request's own keys are the whole shape.
func refusals(declared map[string]specOption, request map[string]any) []string {
	var found []string
	for _, name := range slices.Sorted(maps.Keys(request)) {
		if _, ok := declared[name]; !ok {
			found = append(found, name+" is not an option the role declares")
		}
	}
	for _, name := range slices.Sorted(maps.Keys(declared)) {
		option, value := declared[name], request[name]
		if _, present := request[name]; !present {
			if option.Required {
				found = append(found, name+" is required and absent")
			}
			continue
		}
		if len(option.Options) > 0 {
			found = append(found, name+" declares options this check does not read")
		}
		text, isText := value.(string)
		_, isDict := value.(map[string]any)
		_, isList := value.([]any)
		switch {
		case option.Type == "raw":
		case option.Type == "str" && !isText, option.Type == "dict" && !isDict, option.Type == "list" && !isList:
			found = append(found, fmt.Sprintf("%s is %T, not a %s", name, value, option.Type))
		case option.Type == "str" && len(option.Choices) > 0 && !slices.Contains(option.Choices, text):
			found = append(found, fmt.Sprintf("%s is %q, and the role admits only %q", name, text, option.Choices))
		case !slices.Contains([]string{"str", "dict", "list"}, option.Type):
			found = append(found, name+" declares type "+option.Type+", which this check does not know")
		}
	}
	return found
}

// The setup playbook imports this role by its main entry point, so ansible-core
// refuses, before the role's first task, a request its specification does not
// admit. A request version moved here and not there, or a field added on one
// side only, would therefore pass every gate and refuse every setup on a real
// host. Each request this build sends, with and without tools and for recovery,
// is checked against that specification.
func TestTheRoleAdmitsEveryRequestThisBuildSends(t *testing.T) {
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
		t.Fatal(err)
	}
	declared, ok := document.ArgumentSpecs["main"].Options["bootwright_controller_request"]
	if !ok || declared.Type != "dict" || !declared.Required {
		t.Fatal("the role's main entry point does not require the dict bootwright_controller_request")
	}
	if !slices.Equal(declared.Options["version"].Choices, []string{requestVersion}) {
		t.Fatalf("the role admits request versions %q, and this build sends %s", declared.Options["version"].Choices, requestVersion)
	}
	area := authorityArea{location: prerequisites.BundleLocation{Path: "/bundle", Writable: true}}
	tools := []prerequisites.ToolDefinition{{Kind: "openshift-clients", Source: prerequisites.DependencySource{ID: "tool-openshift-clients", Bytes: 1}}}
	for _, operation := range []string{"setup", "recover"} {
		for _, definition := range []prerequisites.Definition{
			{CatalogDigest: strings.Repeat("a", 64)},
			{CatalogDigest: strings.Repeat("a", 64), Tools: tools},
		} {
			request, err := New(unusedExecution{t}).request(context.Background(), area, nil, operation, prerequisites.Platform{}, definition, prerequisites.SetupEgress{}, nil)
			if err != nil {
				t.Fatal(err)
			}
			encoded, err := json.Marshal(request)
			if err != nil {
				t.Fatal(err)
			}
			var fields map[string]any
			if err := json.Unmarshal(encoded, &fields); err != nil {
				t.Fatal(err)
			}
			for _, problem := range refusals(declared.Options, fields) {
				t.Errorf("%s with %d tools: the role refuses bootwright_controller_request: %s", operation, len(definition.Tools), problem)
			}
		}
	}
}

func TestTheRequestCheckRefusesWhatTheRoleWould(t *testing.T) {
	declared := map[string]specOption{
		"version":   {Type: "str", Required: true, Choices: []string{"v2"}},
		"bundle":    {Type: "dict", Required: true},
		"tools":     {Type: "list", Required: true},
		"native":    {Type: "raw"},
		"structure": {Type: "dict", Options: map[string]specOption{"path": {Type: "str"}}},
	}
	for request, want := range map[string]string{
		`{"version":"v2","bundle":{},"tools":[],"native":null}`:  "",
		`{"version":"v1","bundle":{},"tools":[]}`:                `version is "v1", and the role admits only ["v2"]`,
		`{"version":"v2","bundle":{},"tools":[],"extra":1}`:      "extra is not an option the role declares",
		`{"version":"v2","tools":[]}`:                            "bundle is required and absent",
		`{"version":"v2","bundle":[],"tools":[]}`:                "bundle is []interface {}, not a dict",
		`{"version":"v2","bundle":{},"tools":{}}`:                "tools is map[string]interface {}, not a list",
		`{"version":"v2","bundle":{},"tools":[],"structure":{}}`: "structure declares options this check does not read",
	} {
		var fields map[string]any
		if err := json.Unmarshal([]byte(request), &fields); err != nil {
			t.Fatal(err)
		}
		found := strings.Join(refusals(declared, fields), "; ")
		if want == "" && found != "" || want != "" && !strings.Contains(found, want) {
			t.Errorf("%s: refusals %q, want %q", request, found, want)
		}
	}
}
