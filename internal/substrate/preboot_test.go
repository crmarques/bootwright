package substrate

import (
	"maps"
	"reflect"
	"slices"
	"testing"

	"go.yaml.in/yaml/v3"

	"github.com/crmarques/bootwright/ansible"
	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

const preBootEndpoint = "https://metal-bmc.lab.example.test/redfish/v1/Systems/1"

// Each refusal a pre-boot proof names is reported as the installation's own
// diagnostic: the Machine as its object, what was refused, and the remedy,
// which names the Machine too because it is what the operator changes, and
// binds every command it names to the installation's context, so a copied
// remedy never stops or proves another context's machine of the same name.
// The refusing apply stays incomplete and continues its frozen plan, and the
// context's input cannot change under it, so a corrected declaration is named
// only after the destroy that takes the context back.
func TestEachPreBootRefusalNamesTheMachineWhatWasRefusedAndTheRemedy(t *testing.T) {
	object := &diagnostics.ObjectIdentity{APIVersion: api.APIVersion, Kind: string(api.Machine), Name: "metal-01"}
	refusal := func(message, remediation string) []diagnostics.Diagnostic {
		return []diagnostics.Diagnostic{{Severity: "error", Code: "lifecycle.state", Message: message, Remediation: remediation, Object: object}}
	}
	running := refusal("Machine/metal-01 is running, and its installation erases its disk, so no media was inserted into it and it was not booted",
		"stop it with bootwright machine stop --context lab-b --name metal-01, then run bootwright apply --context lab-b")
	for arm, want := range map[string]map[string][]diagnostics.Diagnostic{
		ArmLibvirt: {RefusalMachineRunning: running},
		ArmBaremetal: {
			RefusalHardwareMismatch: refusal("the machine answering at "+preBootEndpoint+" does not report every hardware address Machine/metal-01 declares, "+
				"or its inventory could not be read in full, so no media was inserted into it and it was not booted",
				"once the machine reports the hardware Machine/metal-01 declares in full, run bootwright apply --context lab-b; to correct that declaration instead, "+
					"take this context back with bootwright destroy --context lab-b, correct spec.hardware.management.bmc.address or spec.hardware.nics on Machine/metal-01 "+
					"and import that input with bootwright context update --name lab-b --input-dir <directory>, then run bootwright apply --context lab-b"),
			RefusalIdentityMismatch: refusal("the management controller at "+preBootEndpoint+" answers as another system than the one this operation proved "+
				"for Machine/metal-01, so no media was inserted into it and it was not booted",
				"take this context back with bootwright destroy --context lab-b, correct spec.hardware.management.bmc.address on Machine/metal-01 "+
					"if it names another controller and import that input with bootwright context update --name lab-b --input-dir <directory>, "+
					"then run bootwright apply --context lab-b, so the machine is proved again"),
			RefusalMachineRunning: running,
		},
		ArmVSphere: nil,
		"":         nil,
	} {
		t.Run(arm, func(t *testing.T) {
			found := PreBootRefusals(arm, "lab-b", "metal-01", preBootEndpoint)
			got := map[string][]diagnostics.Diagnostic{}
			for reason, err := range found {
				got[reason] = diagnostics.Of(err)
			}
			if len(want) == 0 && len(found) != 0 || len(want) != 0 && !reflect.DeepEqual(got, want) {
				t.Fatalf("refusals = %#v, want %#v", got, want)
			}
		})
	}
}

// The reasons are the ones each arm's pre_boot entry point names before its
// checks, and that entry point clears what it named once the proof holds, so
// the runner is never told of a reason it was given no diagnostic for.
func TestEachArmsPreBootEntryPointNamesExactlyTheseRefusals(t *testing.T) {
	for _, arm := range Realized() {
		role := "substrate_" + arm + "_machine"
		path := "collections/ansible_collections/bootwright/core/roles/" + role + "/tasks/pre_boot.yml"
		data, ok := ansible.Assets()[path]
		if !ok {
			t.Fatalf("the embedded collection carries no %s", path)
		}
		var tasks []map[string]any
		if err := yaml.Unmarshal(data, &tasks); err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		var named []string
		for _, task := range tasks {
			facts, _ := task["ansible.builtin.set_fact"].(map[string]any)
			if value, ok := facts[role+"_refusal"]; ok {
				named = append(named, value.(string))
			}
		}
		if len(named) < 2 || named[0] != "" || named[len(named)-1] != "" {
			t.Fatalf("%s names %q, which does not clear the refusal before and after its checks", path, named)
		}
		reasons := slices.DeleteFunc(slices.Clone(named), func(reason string) bool { return reason == "" })
		if want := slices.Sorted(maps.Keys(PreBootRefusals(arm, "lab", "metal-01", preBootEndpoint))); !reflect.DeepEqual(slices.Sorted(slices.Values(reasons)), want) {
			t.Fatalf("%s names %q, want each of %q once", path, reasons, want)
		}
	}
}
