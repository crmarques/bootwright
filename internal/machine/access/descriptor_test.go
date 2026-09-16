package access

import (
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/machine"
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

func catalog() api.Catalog {
	network := m("addresses", list(m("name", "ssh", "address", "192.0.2.10/24")))
	host := object(api.Machine, "host", m(
		"os", m("provided", true), "network", network,
		"access", m("ssh", m("addressRef", "ssh", "user", "operator", "port", api.IntegerValue("2222"),
			"auth", m("operatorIdentity", m())))))
	guest := object(api.Machine, "guest", m(
		"os", m("provided", false, "installProfileRef", "rhel"), "network", network,
		"access", m("ssh", m("addressRef", "ssh", "user", "bootwright",
			"auth", m("privateKeyRef", "fleet")))))
	local := object(api.Machine, "controller", m("os", m("provided", true), "access", m("local", true)))
	return api.NewCatalog([]api.Object{host, guest, local})
}

func code(t *testing.T, err error) string {
	t.Helper()
	reported := diagnostics.Of(err)
	if len(reported) == 0 {
		t.Fatalf("error carries no diagnostic: %v", err)
	}
	return reported[0].Code
}

func TestDescriptorNamesThePinnedClientTheTargetAndNothingElse(t *testing.T) {
	descriptor, err := describe(catalog(), "lab", "host", machine.SSHOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if descriptor.Client != SSHClient {
		t.Fatalf("client = %q", descriptor.Client)
	}
	if got := strings.Join(descriptor.Arguments, " "); got != "-p 2222 -l operator 192.0.2.10" {
		t.Fatalf("arguments = %q", got)
	}
	if descriptor.Advisory != "" {
		t.Fatalf("an operator identity needed an advisory: %q", descriptor.Advisory)
	}
}

// A private key is confidential material this context holds. The descriptor
// names the export the operator performs themselves and never a key path it
// invented, a value, or a file it wrote.
func TestASecretBackedIdentityIsNamedRatherThanMaterialized(t *testing.T) {
	descriptor, err := describe(catalog(), "lab", "guest", machine.SSHOptions{UserForProvisioned: true})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.Join(descriptor.Arguments, " "), "-i ") {
		t.Fatalf("descriptor invented an identity file: %+v", descriptor.Arguments)
	}
	if !strings.Contains(descriptor.Advisory, "fleet") || !strings.Contains(descriptor.Advisory, "secret show") {
		t.Fatalf("advisory = %q", descriptor.Advisory)
	}
}

func TestABorrowedAccountReplacesTheAuthoredOneOnlyWhereItIsEligible(t *testing.T) {
	borrowed := machine.SSHOptions{User: "admin", IdentityFile: "/keys/admin"}
	descriptor, err := describe(catalog(), "lab", "host", borrowed)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(descriptor.Arguments, " "); got != "-i /keys/admin -o IdentitiesOnly=yes -p 2222 -l admin 192.0.2.10" {
		t.Fatalf("arguments = %q", got)
	}
	if _, err := describe(catalog(), "lab", "guest", borrowed); code(t, err) != "access.unavailable" {
		t.Fatalf("a provisioned Machine accepted a borrowed account without the explicit extension: %v", err)
	}
	extended := borrowed
	extended.UserForProvisioned = true
	if _, err := describe(catalog(), "lab", "guest", extended); err != nil {
		t.Fatal(err)
	}
}

func TestAnUnresolvableTargetNeverProducesAPartialDescriptor(t *testing.T) {
	for _, test := range []struct {
		name, machineName, want string
	}{
		{"unnamed", "", "access.target"},
		{"outside the selected graph", "absent", "access.target"},
		{"reached locally", "controller", "access.unavailable"},
	} {
		t.Run(test.name, func(t *testing.T) {
			descriptor, err := describe(catalog(), "lab", test.machineName, machine.SSHOptions{})
			if descriptor != nil {
				t.Fatalf("descriptor = %+v", descriptor)
			}
			if got := code(t, err); got != test.want {
				t.Fatalf("code = %q, want %q", got, test.want)
			}
		})
	}
}

// A command value that cannot be presented exactly is not the command the
// operator would run, so the handoff refuses rather than changing it.
func TestACommandValueThatCannotBeEncodedRefusesTheHandoff(t *testing.T) {
	descriptor, err := describe(catalog(), "lab", "host", machine.SSHOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := descriptor.withCommand(nil); code(t, err) != "cli.usage" {
		t.Fatalf("empty command: %v", err)
	}
	with, err := descriptor.withCommand([]string{"systemctl", "status\nrm -rf /"})
	if err != nil {
		t.Fatal(err)
	}
	if err := with.encodable(); code(t, err) != "access.handoff" {
		t.Fatalf("control character accepted: %v", err)
	}
}

func TestACommandIsPreservedAsAnArgumentVector(t *testing.T) {
	descriptor, err := describe(catalog(), "lab", "host", machine.SSHOptions{})
	if err != nil {
		t.Fatal(err)
	}
	with, err := descriptor.withCommand([]string{"--help", "a b"})
	if err != nil {
		t.Fatal(err)
	}
	if err := with.encodable(); err != nil {
		t.Fatal(err)
	}
	arguments := with.Arguments
	if arguments[len(arguments)-2] != "--help" || arguments[len(arguments)-1] != "a b" {
		t.Fatalf("arguments = %+v", arguments)
	}
}
