package compilation_test

import (
	"context"
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/desiredstate/yamlstream"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

// A zone names an interface of the controller, not a host, and would put a %
// in the ssh/<address>:<port>/ qualifier of every host reservation the
// Machine places, which the controller record refuses at write. The address
// grammar has no zone, so a zoned SSH address, bare or with a prefix, refuses
// api.value at validate, before any plan.
func TestAZonedSSHAddressRefusesAtValidate(t *testing.T) {
	compiler := compilation.NewCompiler(yamlstream.Parser{}, nil)
	for _, address := range []string{"fe80::1%eth0", "fe80::1%eth0/64", "fe80::1"} {
		machine := "\n---\napiVersion: bootwright.io/v1alpha1\nkind: Machine\nmetadata: {name: host}\nspec:\n  os: {provided: true}\n  network:\n    addresses: [{name: ssh, address: '" + address + "'}]\n  access:\n    ssh: {addressRef: ssh, auth: {operatorIdentity: {}}}\n"
		_, _, err := compiler.Compile(context.Background(), sources(environmentYAML+machine))
		got := []string{}
		for _, d := range diagnostics.Of(err) {
			if d.Object != nil && d.Object.Kind == string(api.Machine) {
				got = append(got, d.Code+" "+d.Field+" "+d.Message)
			}
		}
		want := []string{}
		if strings.Contains(address, "%") {
			want = []string{"api.value $.spec.network.addresses[0].address value does not match the required address grammar"}
		}
		if !slices.Equal(got, want) {
			t.Errorf("%s: diagnostics = %q, want %q", address, got, want)
		}
	}
}
