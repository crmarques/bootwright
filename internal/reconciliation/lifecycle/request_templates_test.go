package lifecycle

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
)

// requesting is one block of the harness's kind that realizes object with
// request.
func requesting(id, object, request string) reconciliation.BlockDefinition {
	planned := definition(id)
	planned.Object, planned.Request = object, json.RawMessage(request)
	return planned
}

const (
	delimitersRemoved   = `"{{", "{%" and "{#"`
	reservedKeysRemoved = `every key named "__ansible_type", "__ansible_unsafe" or "__ansible_vault"`
)

// heldRefusal is the diagnostic a block of kind/name refuses with, in the
// harness's context, saying what its request holds and where, and removing
// what removed names. A plan reads the context's imported input, so the remedy
// imports the repaired values before it plans again.
func heldRefusal(kind, name, held, removed string) diagnostics.Diagnostic {
	return diagnostics.Diagnostic{
		Severity: "error", Code: "api.value",
		Message: "the request planned for " + kind + "/" + name + " holds " + held,
		Object:  &diagnostics.ObjectIdentity{APIVersion: api.APIVersion, Kind: kind, Name: name},
		Remediation: "remove " + removed + " from the desired-state values " + kind + "/" + name + " is planned from, import them with bootwright context update --name " +
			testContextName + " --input-dir <dir>, then run bootwright plan --context " + testContextName,
	}
}

// delimiterRefusal is the diagnostic of a block whose request holds a template
// delimiter, saying where.
func delimiterRefusal(kind, name, delimiter, where string) diagnostics.Diagnostic {
	return heldRefusal(kind, name, `the template delimiter "`+delimiter+`" in `+where, delimitersRemoved)
}

// reservedKeyRefusal is the diagnostic of a block whose request holds a key
// ansible-core decodes as a typed value, saying in which object.
func reservedKeyRefusal(kind, name, key, where string) diagnostics.Diagnostic {
	return heldRefusal(kind, name, `the ansible-core reserved key "`+key+`" in `+where, reservedKeysRemoved)
}

// A runner hands every request string to Ansible as data, and planning keeps a
// value that would be code there, or a key ansible-core reserves that no runner
// can encode, out of a plan before anything is registered, bound or written:
// plan says why as apply does, one diagnostic per block, naming the object, the
// first field holding one, how many more do and the remedy.
func TestAPlanRefusesARequestHoldingATemplateDelimiter(t *testing.T) {
	for _, test := range []struct {
		name        string
		definitions []reconciliation.BlockDefinition
		want        []diagnostics.Diagnostic
	}{
		{"an expression in a value",
			[]reconciliation.BlockDefinition{requesting("alpha", "alpha", `{"name":"alpha","unit":"bootwright-{{ unit }}"}`)},
			[]diagnostics.Diagnostic{delimiterRefusal("ArtifactServer", "alpha", "{{", "unit")}},
		{"a statement in a list item",
			[]reconciliation.BlockDefinition{requesting("alpha", "alpha", `{"names":["alpha","{% if true %}x{% endif %}"]}`)},
			[]diagnostics.Diagnostic{delimiterRefusal("ArtifactServer", "alpha", "{%", "names[1]")}},
		{"a comment in a nested value",
			[]reconciliation.BlockDefinition{requesting("alpha", "alpha", `{"egress":{"proxy":{"comment":"{# x #}"}},"port":8443}`)},
			[]diagnostics.Diagnostic{delimiterRefusal("ArtifactServer", "alpha", "{#", "egress.proxy.comment")}},
		{"a line of a multi-line value",
			[]reconciliation.BlockDefinition{requesting("alpha", "alpha", `{"kickstart":"%packages\n{{ 7*6 }}\n%end\n"}`)},
			[]diagnostics.Diagnostic{delimiterRefusal("ArtifactServer", "alpha", "{{", "line 2 of kickstart")}},
		{"a key of the request",
			[]reconciliation.BlockDefinition{requesting("alpha", "alpha", `{"{{ k }}":"value"}`)},
			[]diagnostics.Diagnostic{delimiterRefusal("ArtifactServer", "alpha", "{{", "a key of the request")}},
		{"a value under a key a dotted path cannot name",
			[]reconciliation.BlockDefinition{requesting("alpha", "alpha", `{"labels":{"a.b":"{%- x -%}"}}`)},
			[]diagnostics.Diagnostic{delimiterRefusal("ArtifactServer", "alpha", "{%", `labels["a.b"]`)}},
		{"two fields of one block",
			[]reconciliation.BlockDefinition{requesting("alpha", "alpha", `{"b":["{% y %}"],"a":"{{ x }}"}`)},
			[]diagnostics.Diagnostic{delimiterRefusal("ArtifactServer", "alpha", "{{", "a, and 1 more of its fields hold one")}},
		{"a key ansible-core reserves",
			[]reconciliation.BlockDefinition{requesting("alpha", "alpha", `{"interfaces":[{"__ansible_vault":"kept","name":"enp1s0"}]}`)},
			[]diagnostics.Diagnostic{reservedKeyRefusal("ArtifactServer", "alpha", "__ansible_vault", "interfaces[0]")}},
		{"a reserved key of the request",
			[]reconciliation.BlockDefinition{requesting("alpha", "alpha", `{"__ansible_type":{"name":"x"}}`)},
			[]diagnostics.Diagnostic{reservedKeyRefusal("ArtifactServer", "alpha", "__ansible_type", "the request")}},
		{"a reserved key beside a delimiter",
			[]reconciliation.BlockDefinition{requesting("alpha", "alpha", `{"a":{"__ansible_unsafe":"x"},"b":"{{ x }}"}`)},
			[]diagnostics.Diagnostic{heldRefusal("ArtifactServer", "alpha",
				`the ansible-core reserved key "__ansible_unsafe" in a, and 1 more of its fields hold one`, delimitersRemoved+" and "+reservedKeysRemoved)}},
		{"two blocks of one object and one of another",
			[]reconciliation.BlockDefinition{
				requesting("os-install-beta", "beta", `{"kickstart":"{# x #}"}`),
				requesting("machine-beta", "beta", `{"interfaces":[{"bridge":"{{ x }}"}]}`),
				requesting("alpha", "alpha", `{"name":"{% raw %}"}`),
			},
			[]diagnostics.Diagnostic{
				delimiterRefusal("ArtifactServer", "alpha", "{%", "name"),
				delimiterRefusal("ArtifactServer", "beta", "{#", "kickstart"),
				delimiterRefusal("ArtifactServer", "beta", "{{", "interfaces[0].bridge"),
			}},
	} {
		t.Run(test.name, func(t *testing.T) {
			h := newPlannedHarness(t, test.definitions)
			_, planned := h.service.Plan(context.Background(), PlanRequest{ContextName: testContextName})
			_, applied := h.service.Apply(context.Background(), ApplyRequest{ContextName: testContextName, SkipConfirmation: true})
			for verb, err := range map[string]error{"plan": planned, "apply": applied} {
				if reported := diagnostics.Of(err); !reflect.DeepEqual(reported, test.want) {
					t.Fatalf("%s reported %+v, want %+v", verb, reported, test.want)
				}
			}
			if len(h.workspace.area.files) != 0 || h.workspace.mutations != 0 || len(h.binder.bound) != 0 {
				t.Fatalf("a refused plan wrote %d records in %d mutations and bound %v", len(h.workspace.area.files), h.workspace.mutations, h.binder.bound)
			}
		})
	}
}

// The controller prerequisites block runs through the controller's own Ansible
// with its request read the same way, so its stage earns it no exemption.
func TestTheControllerPrerequisitesBlockIsScannedLikeEveryOther(t *testing.T) {
	prerequisite := requesting("controller-prerequisites", "lab", `{"egress":{"noProxy":["{{ 7*6 }}"]}}`)
	prerequisite.Stage, prerequisite.Kind = reconciliation.StageController, string(api.Environment)
	h := newPlannedHarness(t, []reconciliation.BlockDefinition{prerequisite, definition("alpha")})
	_, err := h.service.Plan(context.Background(), PlanRequest{ContextName: testContextName})
	want := []diagnostics.Diagnostic{delimiterRefusal("Environment", "lab", "{{", "egress.noProxy[0]")}
	if reported := diagnostics.Of(err); !reflect.DeepEqual(reported, want) {
		t.Fatalf("plan reported %+v, want %+v", reported, want)
	}
}

// Only the three openings Jinja renders refuse: a lone brace, a shell
// expansion and a closing delimiter are ordinary text a request may hold. Only
// the three keys ansible-core decodes as typed values refuse, so a key that
// merely starts as they do, which an open document such as NMState may carry,
// plans.
func TestARequestWithoutATemplateDelimiterPlans(t *testing.T) {
	h := newPlannedHarness(t, []reconciliation.BlockDefinition{
		requesting("alpha", "alpha", `{"nmstate":{"__ANSIBLE_TYPE":"x","__ansible":"x","__ansible_note":"x","__ansible_unsafe_":"x","_ansible_vault":"x"},`+
			`"values":["{","{x}","${HOME}","%}","#}","}}","{ {","{}"],"}}":"%}"}`),
	})
	result, err := h.service.Plan(context.Background(), PlanRequest{ContextName: testContextName})
	if err != nil || result == nil || len(result.Steps) != 1 {
		t.Fatalf("a request holding no template delimiter planned %+v (%v)", result, diagnostics.Of(err))
	}
}
