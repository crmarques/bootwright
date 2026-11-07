package access

import (
	"context"
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/machine"
)

type fleetState struct{}

func (fleetState) RenderEffective(context.Context, compilation.EffectiveRequest) (*compilation.EffectiveResult, error) {
	witness := object(api.Machine, "witness", m(
		"os", m("provided", true), "network", m("addresses", list(m("name", "ssh", "address", "192.0.2.10"))),
		"access", m("ssh", m("addressRef", "ssh", "user", "bootwright", "knownHostsRef", "declared-host-key",
			"auth", m("operatorIdentity", m())))))
	objects := append(catalog().Objects(), witness)
	return &compilation.EffectiveResult{Effective: api.NewCatalog(objects)}, nil
}

func fleetHarness(t *testing.T, fleet func(api.Catalog, api.Object) (FleetIdentity, bool)) *harness {
	t.Helper()
	h := newHarness(t, func(options *Options) { options.FleetIdentity = fleet })
	h.service.state = fleetState{}
	return h
}

func everyMachineIsFleet(api.Catalog, api.Object) (FleetIdentity, bool) {
	return FleetIdentity{User: "bootwright", KeyRef: "fleet"}, true
}

func TestFleetIdentityRefusesAnUnpinnedMachine(t *testing.T) {
	h := fleetHarness(t, everyMachineIsFleet)
	_, err := exec(t, h, "host", machine.SSHOptions{}, "true")
	if code(t, err) != "access.unavailable" || !strings.Contains(diagnostics.Of(err)[0].Message, "pins no SSH host key") {
		t.Fatalf("an unpinned Machine was offered the fleet identity: %v", err)
	}
	if len(h.client.sessions) != 0 || h.lender.lends != 0 {
		t.Fatalf("the refusal opened %d sessions and %d lends", len(h.client.sessions), h.lender.lends)
	}
	_, err = h.service.Exec(context.Background(), ExecRequest{Names: []string{"witness", "host"}, Command: []string{"true"}})
	if code(t, err) != "access.unavailable" || len(h.client.sessions) != 0 || slices.Contains(h.lender.requested, "fleet") {
		t.Fatalf("a concurrent read offered the fleet key to an unpinned Machine: %v %v", err, h.lender.requested)
	}
}

func TestFleetIdentityIsNeverOfferedByOperatorSessions(t *testing.T) {
	h := fleetHarness(t, nil)
	if _, err := exec(t, h, "witness", machine.SSHOptions{}, "true"); err != nil {
		t.Fatal(err)
	}
	if session := h.client.sessions[0]; session.Kind != machine.IdentityOperator || h.client.keys[0] != "" || slices.Contains(h.lender.requested, "fleet") {
		t.Fatalf("an operator session presented %+v with %v opened", session.Identity, h.lender.requested)
	}

	h = fleetHarness(t, everyMachineIsFleet)
	options := machine.SSHOptions{User: "operator", IdentityFile: "/home/operator/.ssh/id_ed25519"}
	if _, err := exec(t, h, "witness", options, "true"); err != nil {
		t.Fatal(err)
	}
	session := h.client.sessions[0]
	if session.Kind != machine.IdentityKey || session.User != "bootwright" || session.IdentityFile != "" || h.client.keys[0] != "FLEET KEY" || h.client.proofs != 0 {
		t.Fatalf("the fleet read followed operator options: %+v key %q proofs %d", session.Identity, h.client.keys[0], h.client.proofs)
	}
	if strings.Join(h.lender.requested, ",") != "fleet,declared-host-key" {
		t.Fatalf("opened %v", h.lender.requested)
	}
}
