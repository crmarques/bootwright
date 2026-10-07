package machine

import (
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

// An OS-ready Machine is told once to remove a network selection, never also
// to select a configuration it may not declare.
func TestAnOSReadyMachineIsToldOnlyToRemoveASelection(t *testing.T) {
	ready := object(api.Machine, "ready", m("os", m("provided", true), "network", m("installAddressRef", "ip")))
	var found []api.Issue
	for _, issue := range Validate(ready, api.NewCatalog([]api.Object{ready})) {
		if issue.Field == "$.spec.network.installAddressRef" {
			found = append(found, issue)
		}
	}
	if len(found) != 1 || found[0].Message != "OS-ready Machines declare contacts only" {
		t.Fatalf("issues = %+v", found)
	}
}
