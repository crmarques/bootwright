package power

import (
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
	guest := object(api.Machine, "guest", m("substrate", m("providerRef", "lab"),
		"os", m("provided", false, "installProfileRef", "rhel"), "network", addresses("192.0.2.20/24")))
	physical := object(api.Machine, "metal", m("os", m("provided", true), "network", addresses("192.0.2.30/24"),
		"hardware", m("management", m("bmc", m("address", "https://bmc.example.test/redfish/v1/Systems/1",
			"credentialsRef", "metal-bmc")))))
	return api.NewCatalog([]api.Object{environment, controller, host, provider, guest, physical})
}

func realized() map[string]machine.OwnershipState {
	return map[string]machine.OwnershipState{"Machine/guest": {Verb: "apply", State: "done"}}
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
	request, err := requestFor(catalog(), "lab", "guest", Stop, false, realized())
	if err != nil {
		t.Fatal(err)
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
	request, err := requestFor(catalog(), "lab", "metal", Start, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if request.Controller.Endpoint != "https://bmc.example.test/redfish/v1/Systems/1" {
		t.Fatalf("endpoint = %q", request.Controller.Endpoint)
	}
	if request.Controller.CredentialsRef != "metal-bmc" || !request.Placement.Local() {
		t.Fatalf("request = %+v", request)
	}
}

// An emulated controller exists only while the Machine that owns it does, so a
// power operation asks the evidence before it acts.
func TestAnEmulatedControllerThatIsNotRealizedRefusesBeforeActing(t *testing.T) {
	for _, test := range []struct {
		name  string
		owned map[string]machine.OwnershipState
	}{
		{"never applied", nil},
		{"applying", map[string]machine.OwnershipState{"Machine/guest": {Verb: "apply", State: "pending"}}},
		{"unproved", map[string]machine.OwnershipState{"Machine/guest": {Verb: "apply", State: "unknown"}}},
		{"removed", map[string]machine.OwnershipState{"Machine/guest": {Verb: "destroy", State: "done"}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := requestFor(catalog(), "lab", "guest", Start, false, test.owned); code(t, err) != "access.unavailable" {
				t.Fatalf("unrealized Machine accepted a power request: %v", err)
			}
		})
	}
}

func TestAnUnresolvableTargetOrVerbRefuses(t *testing.T) {
	if _, err := requestFor(catalog(), "lab", "absent", Start, false, realized()); code(t, err) != "access.target" {
		t.Fatalf("unknown Machine: %v", err)
	}
	if _, err := requestFor(catalog(), "lab", "controller", Start, false, realized()); code(t, err) != "access.unavailable" {
		t.Fatalf("a Machine with no controller at all: %v", err)
	}
	if _, err := requestFor(catalog(), "lab", "guest", "suspend", false, realized()); code(t, err) != "lifecycle.state" {
		t.Fatalf("unsupported verb: %v", err)
	}
}
