package power

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/substrate"
	"github.com/crmarques/bootwright/internal/substrate/baremetal"
)

// A power command, --force included, refuses controller.conflict before any
// power request while another context of the store holds its Machine's bmc:
// key, a bmc-mac: key of a MAC it declares or the bmc-system: key of the UUID
// its pin records, an emulated BMC's shared claim on an SSH-placed provider
// host included; a shared claim beside another shared one, and this context's
// own claims, let it go (REC-10, REC-09, D252 (2)).
func TestAPowerCommandRefusesAServerAnotherContextReserves(t *testing.T) {
	guest, _, _, err := requestFor(catalog(), "lab", "guest", Start, false, realized())
	if err != nil {
		t.Fatal(err)
	}
	emulated := substrate.ControllerReservationKey(guest.Controller.Endpoint)
	physical := func(context string, keys ...string) prerequisites.HostReservation {
		return prerequisites.HostReservation{Context: context, Kind: "substrate-physical", Service: "server", Keys: keys}
	}
	shared := func(context string, key string) prerequisites.HostReservation {
		return prerequisites.HostReservation{Context: context, Kind: "substrate-machine-bmc", Service: "node", Keys: []string{key}, Shared: true}
	}
	pinned := machine.HardwareIdentity{UUID: "4C4C4544-0042-3510-8052-B4C04F4D4E31", Serial: "SN1"}
	for name, test := range map[string]struct {
		machine string
		verb    string
		force   bool
		pin     machine.HardwareIdentity
		held    prerequisites.HostReservation
		refused bool
	}{
		"a forced stop of a server another context holds by its endpoint": {machine: "metal", verb: Stop, force: true, held: physical("other", "bmc:bmc.example.test:443/1"), refused: true},
		"a start of a server another spelling holds by its MAC":           {machine: "metal", verb: Start, held: physical("other", "bmc:192.0.2.40:443/1", "bmc-mac:52:54:00:9a:1b:01"), refused: true},
		"a forced restart of a server another context holds by its UUID":  {machine: "metal", verb: Restart, force: true, pin: pinned, held: physical("other", "bmc:192.0.2.40:443/1", "bmc-system:4c4c4544-0042-3510-8052-b4c04f4d4e31"), refused: true},
		"a server whose endpoint another context's emulated BMC serves":   {machine: "metal", verb: Stop, force: true, held: shared("other", "bmc:bmc.example.test:443/1"), refused: true},
		"a server this context holds":                                     {machine: "metal", verb: Stop, force: true, held: physical("lab", "bmc:bmc.example.test:443/1", "bmc-mac:52:54:00:9a:1b:01")},
		"a server another context holds at another endpoint":              {machine: "metal", verb: Stop, force: true, held: physical("other", "bmc:192.0.2.40:443/1", "bmc-mac:52:54:00:9a:1b:02")},
		"an SSH-hosted guest another context drives as a physical server": {machine: "guest", verb: Stop, force: true, held: physical("other", emulated), refused: true},
		"an SSH-hosted guest whose endpoint another emulated BMC shares":  {machine: "guest", verb: Start, held: shared("other", emulated)},
	} {
		t.Run(name, func(t *testing.T) {
			proved := &pins{identity: test.pin, found: test.pin.Present()}
			power := machine.PowerOn
			if test.verb == Stop {
				power = machine.PowerOff
			}
			runtime, runner := &boundary{held: []prerequisites.HostReservation{test.held}}, &adapter{machine: test.machine, power: power}
			request := PowerRequest{ContextName: "lab", Names: []string{test.machine}, Verb: test.verb, Force: test.force, SkipConfirmation: true}
			_, err := New(stateSource{}, evidenceSource{}, proved, runtime, runner, nil, &answer{}, nil, nil).converge(context.Background(), request)
			if !test.refused {
				if err != nil || runner.seen.Implementation == "" {
					t.Fatalf("the run was refused or never ran: %v", err)
				}
				return
			}
			refused := onlyRefusal(t, err)
			if refused.Code != "controller.conflict" || !strings.HasPrefix(refused.Message, "context other holds ") ||
				!strings.HasPrefix(refused.Remediation, "act on the machine through context other, or run bootwright destroy --context other first") ||
				refused.Object == nil || refused.Object.Name != test.machine || strings.Contains(refused.Message, "52:54") || strings.Contains(refused.Message, "4c4c") {
				t.Fatalf("refusal = %+v", refused)
			}
			if runner.seen.Implementation != "" || runtime.entered != 1 {
				t.Fatalf("a refused power command reached the adapter (%q) or the runtime %d times", runner.seen.Implementation, runtime.entered)
			}
		})
	}
}

// A pin-less power command meets the claim another context's proof made of
// a server that reported a UUID, under another spelling of its BMC address,
// through the bmc-mac: key of the MAC both declare, and refuses before any
// power request (D253 (1)).
func TestAPinLessPowerCommandRefusesAServerAnotherSpellingProvedByItsUUID(t *testing.T) {
	frozen, err := baremetal.Request{
		Controller: baremetal.Controller{CredentialsRef: "metal-bmc", Endpoint: "https://192.0.2.40/redfish/v1/Systems/1", TLSVerify: true},
		Hardware:   []baremetal.Interface{{MACAddress: "52-54-00-9A-1B-01", Name: "eno1"}},
		Identity:   baremetal.Identity{Block: baremetal.BlockID("server"), Context: "other", Object: "server"},
		Placement:  machine.Placement{Connection: machine.ConnectionLocal, Machine: "controller"},
		Version:    "machine-baremetal-v2",
	}.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	evidence, err := json.Marshal(baremetal.Evidence{Addresses: []string{"52:54:00:9a:1b:01"}, Postcondition: true, Power: "Off", Request: "digest", Serial: "SN1", UUID: "4C4C4544-0042-3510-8052-B4C04F4D4E31"})
	if err != nil {
		t.Fatal(err)
	}
	held, found, err := baremetal.NewMachine(nil).ProvedClaim("other", reconciliation.Block{BlockDefinition: reconciliation.BlockDefinition{Request: frozen}}, evidence)
	if err != nil || !found {
		t.Fatalf("claim = %v, %v", found, diagnostics.Of(err))
	}
	runtime, runner := &boundary{held: []prerequisites.HostReservation{held}}, &adapter{machine: "metal", power: machine.PowerOff}
	_, err = New(stateSource{}, evidenceSource{}, &pins{}, runtime, runner, nil, &answer{}, nil, nil).
		converge(context.Background(), PowerRequest{ContextName: "lab", Names: []string{"metal"}, Verb: Stop, Force: true, SkipConfirmation: true})
	refused := onlyRefusal(t, err)
	if refused.Code != "controller.conflict" || !strings.HasPrefix(refused.Message, "context other holds ") || refused.Object == nil || refused.Object.Name != "metal" {
		t.Fatalf("refusal = %+v", refused)
	}
	if runner.seen.Implementation != "" {
		t.Fatalf("a refused power command reached the adapter with %q", runner.seen.Implementation)
	}
}

// The physical Machine drives its endpoint, each MAC it declares in lower
// case and, once pinned, its UUID, exclusively; an emulated guest drives its
// endpoint shared.
func TestAPowerRunDeclaresWhatItDrives(t *testing.T) {
	runtime := &boundary{}
	service := New(stateSource{}, evidenceSource{}, &pins{identity: machine.HardwareIdentity{UUID: " ABC-1 "}, found: true}, runtime, &adapter{machine: "metal", power: machine.PowerOn}, nil, &answer{}, nil, nil)
	if _, err := service.Start(context.Background(), PowerRequest{ContextName: "lab", Names: []string{"metal"}}); err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	drives := runtime.drives[0]
	if len(drives) != 1 || drives[0].Kind != "substrate-physical" || drives[0].Object != "metal" || drives[0].Shared ||
		strings.Join(drives[0].Keys, " ") != "bmc:bmc.example.test:443/1 bmc-mac:52:54:00:9a:1b:01 bmc-system:abc-1" {
		t.Fatalf("drives = %+v", drives)
	}
}

// A MAC declared with dashes or in upper case drives the colon, lower-case key
// a proof claims, so a pin-less command meets the holder of that key.
func TestAPowerRunDrivesTheCanonicalMACKey(t *testing.T) {
	frozen := Request{Controller: Access{Endpoint: "https://bmc.example.test/redfish/v1/Systems/1"}, Identity: Identity{Object: "metal"}}
	keys := drives(frozen, true, machine.HardwareIdentity{}, []string{"52-54-00-9A-1B-01"})[0].Keys
	if strings.Join(keys, " ") != "bmc:bmc.example.test:443/1 bmc-mac:52:54:00:9a:1b:01" {
		t.Fatalf("keys = %v", keys)
	}
}

// A batch checks every member in one shared hold before any power request
// and refuses whole, naming each held Machine, so no member is powered while
// another of the same command is refused.
func TestAPowerBatchRefusesWholeWhenAnyMemberIsHeld(t *testing.T) {
	guest, _, _, err := requestFor(catalog(), "lab", "guest", Start, false, realized())
	if err != nil {
		t.Fatal(err)
	}
	held := []prerequisites.HostReservation{
		{Context: "other", Kind: "substrate-physical", Service: "server", Keys: []string{"bmc:bmc.example.test:443/1"}},
		{Context: "edge", Kind: "substrate-physical", Service: "node", Keys: []string{substrate.ControllerReservationKey(guest.Controller.Endpoint)}},
	}
	runtime, runner := &boundary{held: held}, &adapter{power: machine.PowerOff}
	_, err = New(stateSource{}, evidenceSource{}, &pins{}, runtime, runner, nil, &answer{}, nil, nil).
		Stop(context.Background(), PowerRequest{ContextName: "lab", Names: []string{"metal", "guest"}, Force: true, SkipConfirmation: true})
	reported := diagnostics.Of(err)
	if len(reported) != 2 || reported[0].Code != "controller.conflict" || reported[1].Code != "controller.conflict" ||
		reported[0].Object.Name != "guest" || reported[1].Object.Name != "metal" {
		t.Fatalf("a batch with held members = %+v", reported)
	}
	if runtime.entered != 1 || runner.seen.Implementation != "" {
		t.Fatalf("the refused batch entered %d runtimes and reached the adapter with %q", runtime.entered, runner.seen.Implementation)
	}
}
