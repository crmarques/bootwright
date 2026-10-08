package main

import (
	"encoding/json"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/managedos/installation"
	"github.com/crmarques/bootwright/internal/substrate/baremetal"
	"github.com/crmarques/bootwright/internal/substrate/libvirt"
)

// exampleRequests is every frozen request one example derives through the
// production compiler, per capability. An admission or schema change that
// moves none of them leaves this golden as it is, so a live context's frozen
// plan stays the plan the same input derives.
type exampleRequests struct {
	LibvirtHosts      []libvirt.HostRequest    `json:"libvirtHosts,omitempty"`
	LibvirtMachines   []libvirt.MachineRequest `json:"libvirtMachines,omitempty"`
	Installations     []installation.Request   `json:"installations,omitempty"`
	BaremetalMachines []baremetal.Request      `json:"baremetalMachines,omitempty"`
}

func compiledEffective(t *testing.T, name string) api.Catalog {
	t.Helper()
	state, _ := compileAcceptance(t, exampleDirectory(t, name))
	return state.Effective()
}

// TestExampleRequestsKeepTheirBytes pins the requests lab-rhel, lab-sno and
// lab-baremetal derive with controller Machine controller in context lab,
// lab-baremetal's private installation included.
func TestExampleRequestsKeepTheirBytes(t *testing.T) {
	const controllerMachine, contextName = "controller", "lab"
	derive := func(t *testing.T, name string, err error) {
		t.Helper()
		if err != nil {
			t.Fatalf("deriving %s: %v", name, diagnostics.Of(err))
		}
	}
	cases := map[string]func(t *testing.T) exampleRequests{
		"lab-rhel": func(t *testing.T) exampleRequests {
			catalog := compiledEffective(t, "lab-rhel")
			var found exampleRequests
			var err error
			found.LibvirtHosts, err = libvirt.HostRequests(catalog, controllerMachine, contextName)
			derive(t, "the libvirt host requests", err)
			found.LibvirtMachines, err = libvirt.MachineRequests(catalog, controllerMachine, contextName)
			derive(t, "the libvirt machine requests", err)
			found.Installations = plannedInstallations(t, "lab-rhel", controllerMachine, contextName)
			return found
		},
		"lab-sno": func(t *testing.T) exampleRequests {
			catalog := compiledEffective(t, "lab-sno")
			var found exampleRequests
			var err error
			found.LibvirtHosts, err = libvirt.HostRequests(catalog, controllerMachine, contextName)
			derive(t, "the libvirt host requests", err)
			found.LibvirtMachines, err = libvirt.MachineRequests(catalog, controllerMachine, contextName)
			derive(t, "the libvirt machine requests", err)
			return found
		},
		"lab-baremetal": func(t *testing.T) exampleRequests {
			catalog := compiledEffective(t, "lab-baremetal")
			var found exampleRequests
			var err error
			found.BaremetalMachines, err = baremetal.Requests(catalog, controllerMachine, contextName)
			derive(t, "the bare-metal requests", err)
			found.Installations = plannedInstallations(t, "lab-baremetal", controllerMachine, contextName)
			return found
		},
	}
	for name, requests := range cases {
		t.Run(name, func(t *testing.T) {
			found := requests(t)
			if len(found.LibvirtHosts)+len(found.LibvirtMachines)+len(found.Installations)+len(found.BaremetalMachines) == 0 {
				t.Fatal("the example derived no request")
			}
			data, err := json.MarshalIndent(found, "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			matchesTextGolden(t, "request-"+name, append(data, '\n'))
		})
	}
}
