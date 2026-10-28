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
// only after the destroy that takes the context back. A remedy that continues
// the refusing apply names its continuation with every token its frozen plan
// consumes, while the apply after a destroy plans anew and names its own.
func TestEachPreBootRefusalNamesTheMachineWhatWasRefusedAndTheRemedy(t *testing.T) {
	for _, continuation := range []string{"bootwright apply --context lab-b --authorize data-loss", ""} {
		t.Run("continuation="+continuation, func(t *testing.T) {
			eachPreBootRefusal(t, continuation)
		})
	}
}

func eachPreBootRefusal(t *testing.T, continuation string) {
	object := &diagnostics.ObjectIdentity{APIVersion: api.APIVersion, Kind: string(api.Machine), Name: "metal-01"}
	refusal := func(message, remediation string) []diagnostics.Diagnostic {
		return []diagnostics.Diagnostic{{Severity: "error", Code: "lifecycle.state", Message: message, Remediation: remediation, Object: object}}
	}
	next := continuation
	if next == "" {
		next = "bootwright apply --context lab-b"
	}
	running := refusal("Machine/metal-01 is running, and its installation erases its disk, so no media was inserted into it and it was not booted",
		"stop it with bootwright machine stop --context lab-b --name metal-01, then run "+next)
	for arm, want := range map[string]map[string][]diagnostics.Diagnostic{
		ArmLibvirt: {RefusalMachineRunning: running},
		ArmBaremetal: {
			RefusalHardwareMismatch: refusal("the machine answering at "+preBootEndpoint+" does not report exactly the hardware addresses Machine/metal-01 declares, "+
				"or its inventory could not be read in full, so no media was inserted into it and it was not booted",
				"once the machine reports the hardware Machine/metal-01 declares in full, run "+next+"; to correct that declaration instead, "+
					"take this context back with bootwright destroy --context lab-b, correct spec.substrate.baremetal.bmc.address or spec.substrate.baremetal.nics on Machine/metal-01 "+
					"and import that input with bootwright context update --name lab-b --input-dir <directory>, then run bootwright apply --context lab-b"),
			RefusalIdentityMismatch: refusal("the management controller at "+preBootEndpoint+" answers as another system than the one this operation proved "+
				"for Machine/metal-01, so no media was inserted into it and it was not booted",
				"take this context back with bootwright destroy --context lab-b, correct spec.substrate.baremetal.bmc.address on Machine/metal-01 "+
					"if it names another BMC and import that input with bootwright context update --name lab-b --input-dir <directory>, "+
					"then run bootwright apply --context lab-b, so the machine is proved again"),
			RefusalMachineRunning: running,
			RefusalTPMAbsent:      refusal("Machine/metal-01 does not positively report an enabled TPM 2.0, so no encrypted installer was booted", "enable a supported TPM 2.0 on Machine/metal-01, then run "+next),
			RefusalFirmwareMismatch: refusal("the machine answering at "+preBootEndpoint+" does not report the firmware settings Machine/metal-01 declares "+
				"under spec.substrate.baremetal.firmware.mode prove, so no media was inserted into it and it was not booted",
				"set the declared values on Machine/metal-01 through its BMC, then run "+next+"; to have Bootwright set them instead, "+
					"take this context back with bootwright destroy --context lab-b, set spec.substrate.baremetal.firmware.mode to enforce on Machine/metal-01 "+
					"and import that input with bootwright context update --name lab-b --input-dir <directory>, then run bootwright apply --context lab-b"),
			RefusalFirmwareNotApplied: refusal("the management controller at "+preBootEndpoint+" refused or did not apply the firmware settings Machine/metal-01 declares, "+
				"so no media was inserted into it and no installer was booted",
				"when the reasons say the machine was not proved off, first stop it with bootwright machine stop --context lab-b --name metal-01 --force; "+
					"read the BMC's message in the attempt's retained output and correct the cause through the BMC, then run "+next+
					"; any one-time setup boot and settings this step staged stay on the machine and apply at its next boot; to correct the declaration instead, "+
					"take this context back with bootwright destroy --context lab-b, correct spec.substrate.baremetal.firmware on Machine/metal-01 "+
					"and import that input with bootwright context update --name lab-b --input-dir <directory>, then run bootwright apply --context lab-b"),
		},
		ArmVSphere: nil,
		"":         nil,
	} {
		t.Run(arm, func(t *testing.T) {
			found := PreBootRefusals(arm, "lab-b", continuation, "metal-01", preBootEndpoint)
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
// checks, itself or in the task files it includes, and that entry point clears
// what it named once the proof holds, so the runner is never told of a reason
// it was given no diagnostic for.
func TestEachArmsPreBootEntryPointNamesExactlyTheseRefusals(t *testing.T) {
	for _, arm := range Realized() {
		role := "substrate_" + arm + "_machine"
		tasks := "collections/ansible_collections/bootwright/core/roles/" + role + "/tasks/"
		named := preBootRefusalsNamed(t, tasks, "pre_boot.yml", role+"_refusal", map[string]bool{})
		if len(named) < 2 || named[0] != "" || named[len(named)-1] != "" {
			t.Fatalf("%spre_boot.yml names %q, which does not clear the refusal before and after its checks", tasks, named)
		}
		reasons := slices.Compact(slices.Sorted(slices.Values(slices.DeleteFunc(slices.Clone(named), func(reason string) bool { return reason == "" }))))
		if want := slices.Sorted(maps.Keys(PreBootRefusals(arm, "lab", "", "metal-01", preBootEndpoint))); !reflect.DeepEqual(reasons, want) {
			t.Fatalf("%spre_boot.yml names %q, want each of %q", tasks, reasons, want)
		}
	}
}

func preBootRefusalsNamed(t *testing.T, tasks, file, fact string, visited map[string]bool) []string {
	t.Helper()
	if visited[file] {
		return nil
	}
	visited[file] = true
	data, ok := ansible.Assets()[tasks+file]
	if !ok {
		t.Fatalf("the embedded collection carries no %s%s", tasks, file)
	}
	var list []map[string]any
	if err := yaml.Unmarshal(data, &list); err != nil {
		t.Fatalf("%s%s: %v", tasks, file, err)
	}
	return preBootTaskRefusals(t, tasks, list, fact, visited)
}

func preBootTaskRefusals(t *testing.T, tasks string, list []map[string]any, fact string, visited map[string]bool) []string {
	t.Helper()
	var named []string
	for _, task := range list {
		facts, _ := task["ansible.builtin.set_fact"].(map[string]any)
		if value, ok := facts[fact]; ok {
			named = append(named, value.(string))
		}
		for _, key := range []string{"ansible.builtin.include_tasks", "ansible.builtin.import_tasks"} {
			included := task[key]
			if arguments, ok := included.(map[string]any); ok {
				included = arguments["file"]
			}
			if file, ok := included.(string); ok {
				named = append(named, preBootRefusalsNamed(t, tasks, file, fact, visited)...)
			}
		}
		for _, section := range []string{"block", "rescue", "always"} {
			nested, _ := task[section].([]any)
			var children []map[string]any
			for _, child := range nested {
				if entry, ok := child.(map[string]any); ok {
					children = append(children, entry)
				}
			}
			named = append(named, preBootTaskRefusals(t, tasks, children, fact, visited)...)
		}
	}
	return named
}
