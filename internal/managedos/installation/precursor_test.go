package installation

import (
	"bytes"
	"context"
	"testing"

	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/reconciliation"
)

// An installation through an arm that runs no controller client, such as
// libvirt, and the package tree may precede the controller stage's clients; an
// installation whose arm runs one, as vSphere's govc, or a request this build
// cannot read, may not.
func TestOnlyAnInstallationThatRunsNoControllerClientPrecedesTheStage(t *testing.T) {
	input := planInput(reconciliation.Apply)
	catalog := labCatalog()
	input.State = compilation.NewState(catalog, catalog, nil)
	plan, err := New(nil).WithMedia(labMedia()).Plan(context.Background(), input)
	if err != nil || len(plan.Definitions) != 1 {
		t.Fatalf("plan = %+v (%v)", plan, err)
	}
	definition := plan.Definitions[0]
	if !(Capability{}).PrecedesController(definition) || !(TreeCapability{}).PrecedesController(definition) {
		t.Fatal("a libvirt installation or a package tree does not precede the controller stage's clients")
	}
	vsphere := definition
	vsphere.Request = bytes.Replace(definition.Request, []byte(`"substrate":"libvirt"`), []byte(`"substrate":"vsphere"`), 1)
	if _, err := DecodeRequest(vsphere.Request); err != nil || bytes.Equal(vsphere.Request, definition.Request) {
		t.Fatalf("the request names no libvirt substrate: %s", definition.Request)
	}
	broken := definition
	broken.Request = []byte(`{}`)
	if (Capability{}).PrecedesController(vsphere) || (Capability{}).PrecedesController(broken) {
		t.Fatal("an installation that runs govc, or an unreadable request, precedes the controller stage's clients")
	}
}
