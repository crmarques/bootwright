package installation

import (
	"context"
	"testing"

	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/substrate"
)

// An installation through an arm that runs no controller client, such as
// libvirt, and the package tree may precede the controller stage's clients; an
// installation whose arm runs one, as KubeVirt's virtctl, or a request this build
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
	kubevirt := definition
	request, err := DecodeRequest(definition.Request)
	if err != nil || request.Target.Substrate != substrate.ArmLibvirt {
		t.Fatalf("expected a libvirt fixture: %v", err)
	}
	request.Target.Substrate = substrate.ArmKubeVirt
	request.Target.PortMACs = []string{"52:54:00:00:00:01"}
	kubevirt.Request, err = request.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeRequest(kubevirt.Request); err != nil {
		t.Fatal(err)
	}
	broken := definition
	broken.Request = []byte(`{}`)
	if (Capability{}).PrecedesController(kubevirt) || (Capability{}).PrecedesController(broken) {
		t.Fatal("an installation that runs virtctl, or an unreadable request, precedes the controller stage's clients")
	}
}
