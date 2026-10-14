package main

import (
	"strings"
	"testing"

	"github.com/crmarques/bootwright/ansible"
	"github.com/crmarques/bootwright/internal/managedos/installation"
	"github.com/crmarques/bootwright/internal/reconciliation/ansiblerunner"
)

const playbookRoot = "collections/ansible_collections/bootwright/core/playbooks/"

// buildCapabilities lists what this executable can realize, in the API's own
// kind order, so a plan's block order never depends on wiring order.
func buildCapabilities(clock systemClock, controller controllerDependencies, records installation.MediaRecords) capabilityResolver {
	return buildCapabilitiesWith(clock, controller, ansiblerunner.New(operationPlaybook(), controller.Stager), records)
}

// Composition owns the entrypoint each implementation runs, so every binding
// must name a playbook the embedded collection actually carries. A renamed or
// removed entrypoint fails here rather than at the first operation that needs it.
func TestEveryBoundPlaybookExistsInTheEmbeddedCollection(t *testing.T) {
	assets := ansible.Assets()
	for key, playbook := range operationPlaybook() {
		if _, ok := assets[playbookRoot+playbook]; !ok {
			t.Errorf("%s binds %q, which the embedded collection does not carry", key, playbook)
		}
	}
}

// Every capability this build offers can be run, and nothing is bound that no
// capability resolves, so the two lists cannot drift apart.
func TestBoundPlaybooksCoverExactlyTheOfferedCapabilities(t *testing.T) {
	bindings := operationPlaybook()
	implementations := map[string]bool{}
	offeredImplementations := map[string]bool{}
	for key := range bindings {
		implementation, _, _ := strings.Cut(key, "/")
		implementations[implementation] = true
	}
	for _, offered := range buildCapabilities(systemClock{}, controllerDependencies{}, nil).Bindings() {
		// The controller stage runs no playbook of its own: it crosses the
		// installer the Controller context owns.
		if offered.Implementation == "controller-prerequisites" {
			continue
		}
		if !implementations[offered.Implementation] {
			t.Errorf("capability %s has no bound entrypoint", offered.Implementation)
		}
		offeredImplementations[offered.Implementation] = true
	}
	for implementation := range implementations {
		if offeredImplementations[implementation] {
			continue
		}
		// Day-2 power and inspection are bound without a lifecycle
		// capability of their own.
		if implementation == "machine-power-redfish-v2" || implementation == "machine-power-read-v2" {
			continue
		}
		t.Errorf("%s is bound but no capability resolves it", implementation)
	}
}
