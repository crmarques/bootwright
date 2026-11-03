//go:build linux && amd64

package main

import (
	"context"
	"io"
	"strings"
	"sync"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/machine"
	machineaccess "github.com/crmarques/bootwright/internal/machine/access"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/storage/preflight"
	"github.com/crmarques/bootwright/internal/substrate/capacity"
	"github.com/crmarques/bootwright/internal/trust"
)

const witnessHostKey = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAICG+RadYRVwHqO7OQsOpyh8SDf1tbTQ8PUmgn7UEmou/"

type fleetOnlyHost struct {
	mu         sync.Mutex
	identities []machine.Identity
	admitted   string
}

func (h *fleetOnlyHost) Run(_ context.Context, session machine.Session, _ io.Reader, out, _ io.Writer) (int, error) {
	h.mu.Lock()
	h.identities = append(h.identities, session.Identity)
	h.mu.Unlock()
	if session.Kind != machine.IdentityKey || session.User != "bootwright" || string(session.PrivateKey) != h.admitted {
		return 255, nil
	}
	answers := map[string]string{"/usr/bin/hostname": "witness.lab.example.test", "/usr/bin/uname": "kernel", "/usr/bin/cat": "ID=rhel",
		"/usr/bin/timedatectl": "yes", "/usr/bin/id": "0", "/usr/bin/getent": "198.51.100.20 STREAM witness.lab.example.test"}
	_, _ = io.WriteString(out, answers[session.Command[0]]+"\n")
	return 0, nil
}

func (h *fleetOnlyHost) IdentityFile(context.Context, string) (string, error) { return "", nil }

func (h *fleetOnlyHost) HostKeyAlgorithms(_ context.Context, session machine.Session) (string, error) {
	return session.HostKey.Algorithms(), nil
}

func (h *fleetOnlyHost) Observe(context.Context, string, int) (trust.HostKey, error) {
	return trust.HostKey{}, nil
}

func TestStoragePreflightReadsAProvidedTiebreakerWithTheFleetKey(t *testing.T) {
	state, _ := compileAcceptance(t, tiebreakerSources(t, witnessAccess))
	catalog := state.Effective()
	witness := requireObject(t, catalog, api.Machine, "witness")
	lender := &labSessionLender{held: map[string]secrets.Material{
		"bootwright-machine-key": secrets.NewMaterial(map[secrets.Part][]byte{secrets.PrivateKeyPart: []byte("FLEET KEY")}),
		"witness-known-hosts":    secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: []byte("198.51.100.20 " + witnessHostKey + "\n")}),
	}}
	fleet := &preflight.FleetAccess{User: "bootwright", KeyRef: "bootwright-machine-key"}
	for _, admitted := range []string{"FLEET KEY", "ANOTHER KEY"} {
		host := &fleetOnlyHost{admitted: admitted}
		reads := preflightReads(labSessionState{effective: catalog}, nil, machineaccess.Options{Lender: lender},
			func() preflightClient { return host })
		rows, err := preflight.GuestChecks{Commands: reads}.CheckGuest(t.Context(), "lab", witness,
			preflight.HostRequirements{Fleet: fleet}, preflight.PreflightRequest{ContextName: "lab", SSH: machine.SSHOptions{User: "operator"}})
		if err != nil || len(host.identities) == 0 {
			t.Fatalf("%+v %v", rows, err)
		}
		for _, identity := range host.identities {
			if identity.Kind != machine.IdentityKey || identity.User != "bootwright" {
				t.Fatalf("the read presented %+v, not the fleet identity", identity)
			}
		}
		if admitted == "FLEET KEY" {
			if len(rows) != 6 {
				t.Fatalf("rows %+v", rows)
			}
			for _, row := range rows {
				if row.Status != capacity.Passed {
					t.Errorf("%+v", row)
				}
			}
			continue
		}
		if len(rows) != 1 || rows[0].ID != "host.access" || !strings.Contains(rows[0].Next, "bootwright secret show --name bootwright-machine-key --part public-key") {
			t.Fatalf("a refused fleet key reported %+v", rows)
		}
	}
}

func TestOperatorSessionsOfferAProvidedTiebreakerItsOwnIdentity(t *testing.T) {
	state, _ := compileAcceptance(t, tiebreakerSources(t, witnessAccess))
	catalog := state.Effective()
	lender := &labSessionLender{held: map[string]secrets.Material{
		"witness-known-hosts": secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: []byte("198.51.100.20 " + witnessHostKey + "\n")}),
	}}
	host := &fleetOnlyHost{admitted: "FLEET KEY"}
	sessions := machineaccess.New(labSessionState{effective: catalog}, nil, machineaccess.Options{Lender: lender, Launcher: host})
	result, err := sessions.Exec(t.Context(), machineaccess.ExecRequest{ContextName: "lab", Names: []string{"witness"}, Command: []string{"/usr/bin/id", "-u"}})
	if err != nil || result.ExitCode != 255 || host.identities[0].Kind != machine.IdentityOperator {
		t.Fatalf("%+v %v %+v", result, err, host.identities)
	}
	for _, name := range lender.requested {
		if name == "bootwright-machine-key" {
			t.Fatal("an operator session opened the fleet key")
		}
	}
	witness, dataHost := requireObject(t, catalog, api.Machine, "witness"), requireObject(t, catalog, api.Machine, "rhel-01")
	fleet := &preflight.FleetAccess{User: "bootwright", KeyRef: "bootwright-machine-key"}
	for _, offer := range []struct {
		request preflight.CommandRequest
		object  api.Object
	}{
		{preflight.CommandRequest{Machine: "witness"}, witness},
		{preflight.CommandRequest{Machine: "witness", Fleet: fleet}, dataHost},
		{preflight.CommandRequest{Machine: "witness", Fleet: &preflight.FleetAccess{User: "bootwright"}}, witness},
	} {
		if identity, ok := requestFleetIdentity(offer.request)(catalog, offer.object); ok {
			t.Fatalf("%+v received the fleet identity %+v", offer.request, identity)
		}
	}
	if identity, ok := requestFleetIdentity(preflight.CommandRequest{Machine: "witness", Fleet: fleet})(catalog, witness); !ok || identity.User != "bootwright" {
		t.Fatalf("the selected tiebreaker read offered %+v", identity)
	}
}

type unresolvingHost struct{ fleetOnlyHost }

func (h *unresolvingHost) Run(ctx context.Context, session machine.Session, in io.Reader, out, errOut io.Writer) (int, error) {
	if session.Command[0] == "/usr/bin/getent" {
		return 2, nil
	}
	return h.fleetOnlyHost.Run(ctx, session, in, out, errOut)
}

func TestAPrerequisiteCommandsExitStatusIsAFailedCheck(t *testing.T) {
	state, _ := compileAcceptance(t, tiebreakerSources(t, witnessAccess))
	catalog := state.Effective()
	lender := &labSessionLender{held: map[string]secrets.Material{
		"bootwright-machine-key": secrets.NewMaterial(map[secrets.Part][]byte{secrets.PrivateKeyPart: []byte("FLEET KEY")}),
		"witness-known-hosts":    secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: []byte("198.51.100.20 " + witnessHostKey + "\n")}),
	}}
	host := &unresolvingHost{fleetOnlyHost{admitted: "FLEET KEY"}}
	reads := preflightReads(labSessionState{effective: catalog}, nil, machineaccess.Options{Lender: lender}, func() preflightClient { return host })
	rows, err := preflight.GuestChecks{Commands: reads}.CheckGuest(t.Context(), "lab", requireObject(t, catalog, api.Machine, "witness"),
		preflight.HostRequirements{Fleet: &preflight.FleetAccess{User: "bootwright", KeyRef: "bootwright-machine-key"}}, preflight.PreflightRequest{ContextName: "lab"})
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if row.ID == "host.dns" && row.Status != capacity.Failed || row.ID != "host.dns" && row.Status != capacity.Passed {
			t.Fatalf("%+v", rows)
		}
	}
}
