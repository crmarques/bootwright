//go:build linux && amd64

package main

import (
	"context"
	"encoding/json"
	"slices"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/containercluster/agentinstall"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/managedos/installation"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/substrate/kubevirt"
	"github.com/crmarques/bootwright/internal/substrate/libvirt"
)

// exampleNICs is everything one example derives from its Machines' NICs: each
// vNIC's name, MAC and attachment, each provider's MAC claims, each Kickstart
// interface pin and each agent host's interfaces and network configuration. A
// derivation that refuses is pinned by its refusal.
type exampleNICs struct {
	LibvirtMachines  map[string][]libvirt.Interface  `json:"libvirtMachines,omitempty"`
	KubeVirtMachines map[string][]kubevirt.Interface `json:"kubevirtMachines,omitempty"`
	KubeVirtHosts    map[string][]string             `json:"kubevirtHosts,omitempty"`
	Installations    map[string][]string             `json:"installations,omitempty"`
	AgentHosts       map[string]any                  `json:"agentHosts,omitempty"`
	Refusals         map[string]string               `json:"refusals,omitempty"`
}

// TestEveryExampleNICDerivationMatchesItsGolden pins the vNICs, MACs and
// interface pins every tracked example derives, so a change to how NICs are
// declared moves no realized MAC, name or attachment.
func TestEveryExampleNICDerivationMatchesItsGolden(t *testing.T) {
	const contextName = "lab"
	directories := exampleDirectories(t)
	var examples []string
	for name := range directories {
		examples = append(examples, name)
	}
	slices.Sort(examples)
	all := map[string]exampleNICs{}
	for _, name := range examples {
		var state *compilation.State
		if name == refusedExample {
			state, _ = compileAcceptance(t, admissibleExampleSources(t, true))
		} else {
			state, _ = compileAcceptance(t, exampleDirectory(t, name))
		}
		all[name] = deriveExampleNICs(t, state, contextName)
	}
	data, err := json.MarshalIndent(all, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	matchesTextGolden(t, "example-nics", append(data, '\n'))
}

func deriveExampleNICs(t *testing.T, state *compilation.State, contextName string) exampleNICs {
	t.Helper()
	catalog := state.Effective()
	controller := ""
	for _, environment := range catalog.OfKind(api.Environment) {
		controller = environment.Spec().Get("controller", "machineRef").Text()
	}
	found := exampleNICs{Refusals: map[string]string{}}
	refused := func(stage string, err error) bool {
		if err != nil {
			found.Refusals[stage] = refusalText(err)
			return true
		}
		return false
	}
	if machines, err := libvirt.MachineRequests(catalog, controller, contextName); !refused("libvirt", err) {
		for _, machine := range machines {
			if found.LibvirtMachines == nil {
				found.LibvirtMachines = map[string][]libvirt.Interface{}
			}
			found.LibvirtMachines[machine.Identity.Object] = machine.Interfaces
		}
	}
	if machines, err := kubevirt.MachineRequests(catalog, controller, contextName); !refused("kubevirt", err) {
		for _, machine := range machines {
			if found.KubeVirtMachines == nil {
				found.KubeVirtMachines = map[string][]kubevirt.Interface{}
			}
			found.KubeVirtMachines[machine.Identity.Object] = machine.Interfaces
		}
	}
	if hosts, err := kubevirt.HostRequests(catalog, controller, contextName); !refused("kubevirtHosts", err) {
		for _, host := range hosts {
			if found.KubeVirtHosts == nil {
				found.KubeVirtHosts = map[string][]string{}
			}
			found.KubeVirtHosts[host.Identity.Object] = host.MACAddresses
		}
	}
	plan, err := installation.New(nil).WithMedia(exampleMediaRecords{}).Plan(context.Background(), lifecycle.PlanInput{
		Verb: reconciliation.Apply, State: state, Controller: controller,
		Context: lifecycle.ContextIdentity{Name: contextName},
	})
	if !refused("installation", err) {
		for _, definition := range plan.Definitions {
			request, err := installation.DecodeRequest(definition.Request)
			if err != nil {
				t.Fatalf("decoding %s: %v", definition.ID, diagnostics.Of(err))
			}
			if found.Installations == nil {
				found.Installations = map[string][]string{}
			}
			found.Installations[request.Identity.Object] = request.InstallerArguments
		}
	}
	if media, _, _, err := agentinstall.Requests(catalog, controller, contextName); !refused("agent", err) {
		for _, request := range media {
			if found.AgentHosts == nil {
				found.AgentHosts = map[string]any{}
			}
			found.AgentHosts[request.Identity.Cluster] = request.AgentConfig["hosts"]
		}
	}
	if len(found.Refusals) == 0 {
		found.Refusals = nil
	}
	return found
}

func refusalText(err error) string {
	text := ""
	for _, diagnostic := range diagnostics.Of(err) {
		text += diagnostic.Code + ": " + diagnostic.Message + "; "
	}
	return text
}
