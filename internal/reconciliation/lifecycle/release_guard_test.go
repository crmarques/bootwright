package lifecycle

import (
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
)

// A removal never takes a host resource from under another context: it
// refuses before it registers while another context still uses a managed
// bridge this context defines (its bridge-use:<name>), or holds exclusively
// the management controller endpoint this context's emulated BMC serves,
// naming that context and its destroy (B803). Once that context's claim is
// gone the removal proceeds.
func TestARemovalThatWouldStrandAnotherContextsUseRefuses(t *testing.T) {
	for name, claims := range map[string]struct {
		ours, theirs prerequisites.HostReservation
	}{
		"a managed bridge another context's external attachment uses": {
			prerequisites.HostReservation{Context: testContextName, Kind: "substrate-host", Service: "lab", Keys: []string{"bridge:virbr-lab"}},
			prerequisites.HostReservation{Context: "other", Kind: "substrate-host-use", Service: "lab-b", Keys: []string{"bridge-use:virbr-lab"}, Shared: true},
		},
		"an emulated BMC another context drives as a physical machine": {
			prerequisites.HostReservation{Context: testContextName, Kind: "substrate-machine-bmc", Service: "rhel-01", Keys: []string{"bmc:192.0.2.1:8000/1"}, Shared: true},
			prerequisites.HostReservation{Context: "other", Kind: "substrate-physical", Service: "metal-01", Keys: []string{"bmc:192.0.2.1:8000/1"}},
		},
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, "alpha")
			h.capability.reservations = []prerequisites.HostReservation{claims.ours}
			if _, err := apply(h); err != nil {
				t.Fatal(err)
			}
			applied := currentOperation(t, h)
			h.workspace.foreign = []prerequisites.HostReservation{claims.theirs}
			_, err := destroy(h)
			reported := diagnostics.Of(err)
			if len(reported) != 1 || reported[0].Code != "controller.conflict" || !strings.Contains(reported[0].Message, "context other") ||
				reported[0].Remediation != "run bootwright destroy --context other first" {
				t.Fatalf("a stranding removal = %#v", reported)
			}
			if current := currentOperation(t, h); current != applied || len(h.capability.destroys) != 0 {
				t.Fatalf("the refused removal registered %s or removed %v", current, h.capability.destroys)
			}
			h.workspace.foreign = nil
			if _, err := destroy(h); err != nil {
				t.Fatalf("the removal refused once nothing used it: %v", err)
			}
		})
	}
}

// A context that claims the key after the destroy registered is found again
// under the root lock right before the effect on the block holding the
// released key: that block's removal refuses controller.conflict and performs
// nothing, a block holding no such key is still removed, and the same destroy
// continues once the claim is gone (B803).
func TestAClaimMadeAfterTheDestroyRegisteredRefusesTheRemovalsEffect(t *testing.T) {
	for name, claims := range map[string]struct {
		kind, object string
		ours, theirs prerequisites.HostReservation
	}{
		"a bridge use": {"InfraProvider", "lab",
			prerequisites.HostReservation{Context: testContextName, Kind: "substrate-host", Service: "lab", Keys: []string{"bridge:virbr-lab"}},
			prerequisites.HostReservation{Context: "other", Kind: "substrate-host-use", Service: "lab-b", Keys: []string{"bridge-use:virbr-lab"}, Shared: true},
		},
		"an exclusive bmc key": {"Machine", "rhel-01",
			prerequisites.HostReservation{Context: testContextName, Kind: "substrate-machine-bmc", Service: "rhel-01", Keys: []string{"bmc:192.0.2.1:8000/1"}, Shared: true},
			prerequisites.HostReservation{Context: "other", Kind: "substrate-physical", Service: "metal-01", Keys: []string{"bmc:192.0.2.1:8000/1"}},
		},
	} {
		t.Run(name, func(t *testing.T) {
			guarded := definition("guarded")
			guarded.Kind, guarded.Object = claims.kind, claims.object
			definitions := []reconciliation.BlockDefinition{definition("alpha"), guarded}
			h := newPlannedHarness(t, definitions)
			h.service.capabilities = sharedResolver{bound: []BoundCapability{
				{Binding: CapabilityBinding{Kind: "ArtifactServer", Implementation: guarded.Implementation}, Capability: h.capability},
				{Binding: CapabilityBinding{Kind: claims.kind, Implementation: guarded.Implementation}, Capability: h.capability},
			}, plan: CapabilityPlan{Definitions: definitions, Reservations: []prerequisites.HostReservation{claims.ours}}}
			if _, err := apply(h); err != nil {
				t.Fatalf("%#v", diagnostics.Of(err))
			}
			applied := currentOperation(t, h)
			reproofs := 0
			h.workspace.kill = func(point string) error {
				if point == "reprove reservations" {
					reproofs++
					if reproofs == 2 {
						h.workspace.foreign = []prerequisites.HostReservation{claims.theirs}
					}
				}
				return nil
			}
			_, err := destroy(h)
			reported := diagnostics.Of(err)
			if len(reported) == 0 || reported[0].Code != "controller.conflict" || !strings.Contains(reported[0].Message, "context other") ||
				reported[0].Remediation != "run bootwright destroy --context other first" {
				t.Fatalf("a removal stranding a later claim = %#v", reported)
			}
			if currentOperation(t, h) == applied || slices.Contains(h.capability.destroys, "guarded") || !slices.Contains(h.capability.destroys, "alpha") {
				t.Fatalf("the destroy did not register, or removed the guarded block: %v", h.capability.destroys)
			}
			h.workspace.kill, h.workspace.foreign = nil, nil
			if _, err := destroy(h); err != nil || !slices.Contains(h.capability.destroys, "guarded") || !slices.Contains(h.capability.destroys, "alpha") {
				t.Fatalf("the destroy did not continue once nothing used the key: %v (%v)", h.capability.destroys, err)
			}
		})
	}
}
