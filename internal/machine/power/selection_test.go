package power

import (
	"errors"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/substrate"
)

func object(kind api.Kind, name string, spec api.Value) api.Object {
	return api.NewObject(kind, name, api.MapValue(), spec)
}

func m(kv ...any) api.Value {
	fields := []api.FieldValue{}
	for i := 0; i < len(kv); i += 2 {
		var value api.Value
		switch typed := kv[i+1].(type) {
		case api.Value:
			value = typed
		case string:
			value = api.StringValue(typed)
		case bool:
			value = api.BoolValue(typed)
		}
		fields = append(fields, api.FieldValue{Name: kv[i].(string), Value: value})
	}
	return api.MapValue(fields...)
}

func list(values ...api.Value) api.Value { return api.ListValue(values...) }

func addresses(value string) api.Value {
	return m("addresses", list(m("name", "ssh", "address", value)))
}

// The fixture is one libvirt provider whose host is reached over SSH, one
// guest it emulates a controller for, and one physical Machine that authors
// its own controller.
func catalog() api.Catalog {
	environment := object(api.Environment, "env", m("controller", m("machineRef", "controller")))
	controller := object(api.Machine, "controller", m("os", m("provided", true), "network", addresses("192.0.2.5/24")))
	host := object(api.Machine, "host", m("os", m("provided", true), "network", addresses("192.0.2.10/24"),
		"access", m("ssh", m("addressRef", "ssh", "user", "root", "knownHostsRef", "host-key",
			"auth", m("privateKeyRef", "host-identity")))))
	provider := object(api.InfraProvider, "lab", m("libvirt", m("machineRef", "host", "uri", "qemu:///system",
		"bmcEmulationDefaults", m("bindAddress", "192.0.2.10", "port", api.IntegerValue("8000"),
			"auth", m("credentialsRef", "bmc")))))
	guests := object(api.NetworkConfig, "guests", m("nmstate", m("interfaces",
		list(m("name", "enp1s0", "type", "ethernet")))))
	guest := object(api.Machine, "guest", m("substrate", m("providerRef", "lab"),
		"os", m("provided", false, "installProfileRef", "rhel"),
		"network", addresses("192.0.2.20/24").With("configRef", api.StringValue("guests"))))
	physical := object(api.Machine, "metal", m("os", m("provided", true), "network", addresses("192.0.2.30/24"),
		"hardware", m("management", m("bmc", m("address", "https://bmc.example.test/redfish/v1/Systems/1",
			"credentialsRef", "metal-bmc")))))
	return api.NewCatalog([]api.Object{environment, controller, host, provider, guests, guest, physical})
}

// realizedAs answers as a current plan whose machine block of each named
// Machine reached the given state; a Machine it does not name has no block.
func realizedAs(states map[string]machine.OwnershipState) realizations {
	return func(name string) (machine.OwnershipState, bool, error) {
		state, found := states[name]
		return state, found, nil
	}
}

func realized() realizations {
	return realizedAs(map[string]machine.OwnershipState{"guest": {Verb: machine.VerbApply, State: machine.BlockDone}})
}

func code(t *testing.T, err error) string {
	t.Helper()
	reported := diagnostics.Of(err)
	if len(reported) == 0 {
		t.Fatalf("error carries no diagnostic: %v", err)
	}
	return reported[0].Code
}

// An emulated controller is reached at its own allocated port, from the host
// that runs it, and the request names the credential rather than any material.
func TestAnEmulatedControllerIsReachedFromItsProviderHost(t *testing.T) {
	request, physical, err := requestFor(catalog(), "lab", "guest", Stop, false, realized())
	if err != nil {
		t.Fatal(err)
	}
	if physical {
		t.Fatal("a Machine whose controller its provider emulates was resolved as physical")
	}
	want := "http://192.0.2.10:8000/redfish/v1/Systems/" + substrate.DomainUUID("lab", "guest")
	if request.Controller.Endpoint != want {
		t.Fatalf("endpoint = %q, want %q", request.Controller.Endpoint, want)
	}
	if request.Controller.CredentialsRef != "bmc" {
		t.Fatalf("credential = %q", request.Controller.CredentialsRef)
	}
	if request.Placement.Machine != "host" || request.Placement.Local() {
		t.Fatalf("placement = %+v", request.Placement)
	}
	if request.Verb != Stop || request.Force || request.Version != Implementation {
		t.Fatalf("request = %+v", request)
	}
	if canonical, err := request.Canonical(); err != nil || strings.Contains(string(canonical), "bmc-password") {
		t.Fatalf("canonical request: %v %s", err, canonical)
	}
}

// An authored controller belongs to the machine itself, so it is reached from
// the controller host and needs no realization of its own.
func TestAnAuthoredControllerIsReachedFromTheControllerHost(t *testing.T) {
	request, physical, err := requestFor(catalog(), "lab", "metal", Start, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !physical {
		t.Fatal("a Machine that authors its own controller was not resolved as physical")
	}
	if request.Controller.Endpoint != "https://bmc.example.test/redfish/v1/Systems/1" {
		t.Fatalf("endpoint = %q", request.Controller.Endpoint)
	}
	if request.Controller.CredentialsRef != "metal-bmc" || !request.Placement.Local() {
		t.Fatalf("request = %+v", request)
	}
}

// An emulated controller is an effect of the block that realizes its Machine
// on the provider, so power follows that block alone: the Machine's
// installation may have failed, be unproved or still run, and the controller
// still answers. It exists from the apply that completed the block until a
// removal proves the block gone, so a guest a removal refuses can be stopped.
func TestPowerFollowsTheMachineBlockNotTheInstallation(t *testing.T) {
	for _, test := range []struct {
		name     string
		state    machine.OwnershipState
		found    bool
		admitted bool
	}{
		{"applied", machine.OwnershipState{Verb: machine.VerbApply, State: machine.BlockDone}, true, true},
		{"removal pending", machine.OwnershipState{Verb: machine.VerbDestroy, State: machine.BlockPending}, true, true},
		{"removal failed", machine.OwnershipState{Verb: machine.VerbDestroy, State: machine.BlockFailed}, true, true},
		{"removal unproved", machine.OwnershipState{Verb: machine.VerbDestroy, State: machine.BlockUnknown}, true, true},
		{"removal running", machine.OwnershipState{Verb: machine.VerbDestroy, State: machine.BlockRunning}, true, true},
		{"apply pending", machine.OwnershipState{Verb: machine.VerbApply, State: machine.BlockPending}, true, false},
		{"apply running", machine.OwnershipState{Verb: machine.VerbApply, State: machine.BlockRunning}, true, false},
		{"apply failed", machine.OwnershipState{Verb: machine.VerbApply, State: machine.BlockFailed}, true, false},
		{"apply unproved", machine.OwnershipState{Verb: machine.VerbApply, State: machine.BlockUnknown}, true, false},
		{"removed", machine.OwnershipState{Verb: machine.VerbDestroy, State: machine.BlockDone}, true, false},
		{"not planned", machine.OwnershipState{}, false, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			var asked []string
			realized := func(name string) (machine.OwnershipState, bool, error) {
				asked = append(asked, name)
				return test.state, test.found, nil
			}
			_, _, err := requestFor(catalog(), "lab", "guest", Stop, false, realized)
			if strings.Join(asked, ",") != "guest" {
				t.Fatalf("the realization was asked about %v", asked)
			}
			if test.admitted {
				if err != nil {
					t.Fatalf("a standing realization was refused: %v", err)
				}
				return
			}
			reported := diagnostics.Of(err)
			if len(reported) != 1 || reported[0].Code != "access.unavailable" ||
				reported[0].Message != "this Machine's emulated management controller is not realized" ||
				reported[0].Remediation != "bootwright apply --context lab realizes Machine/guest and its controller" {
				t.Fatalf("refusal = %+v", reported)
			}
		})
	}
	unread := errors.New("the current operation cannot be read")
	if _, _, err := requestFor(catalog(), "lab", "guest", Stop, false, func(string) (machine.OwnershipState, bool, error) {
		return machine.OwnershipState{}, false, unread
	}); !errors.Is(err, unread) {
		t.Fatalf("an unreadable realization reported %v", err)
	}
	if _, _, err := requestFor(catalog(), "lab", "metal", Stop, false, func(name string) (machine.OwnershipState, bool, error) {
		t.Fatalf("a physical Machine asked about the realization of %s", name)
		return machine.OwnershipState{}, false, nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestAnUnresolvableTargetOrVerbRefuses(t *testing.T) {
	_, _, err := requestFor(catalog(), "lab", "absent", Start, false, realized())
	if code(t, err) != "access.target" {
		t.Fatalf("unknown Machine: %v", err)
	}
	if remedy := diagnostics.Of(err)[0].Remediation; remedy != "list the Machines this context selects with bootwright machine list --context lab" {
		t.Fatalf("unknown Machine remedy = %q", remedy)
	}
	if _, _, err := requestFor(catalog(), "lab", "controller", Start, false, realized()); code(t, err) != "access.unavailable" {
		t.Fatalf("a Machine with no controller at all: %v", err)
	}
	if _, _, err := requestFor(catalog(), "lab", "guest", "suspend", false, realized()); code(t, err) != "lifecycle.state" {
		t.Fatalf("unsupported verb: %v", err)
	}
}
