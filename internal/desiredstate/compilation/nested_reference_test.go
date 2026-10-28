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

const nestedMachine = "\n---\napiVersion: bootwright.io/v1alpha1\nkind: Machine\nmetadata: {name: %s}\nspec:\n  os: {provided: true}\n  network:\n    addresses: [{name: lan, address: 192.0.2.10/24}]\n"

const nestedProxy = "\n---\napiVersion: bootwright.io/v1alpha1\nkind: Proxy\nmetadata: {name: edge}\nspec:\n  management: managed\n  machineRef: host\n  endpoints: [{name: main, addressRef: lan}]\n"

func nestedDocument(name, extra string) string {
	return strings.Replace(nestedMachine, "%s", name, 1) + extra
}

// A nested, endpoint or native-service reference resolves through the shared
// unresolved-reference diagnostic and its suppression rules, whatever kind
// declares it: an entry its owner does not declare is refused at the field,
// naming the owner, while an owner that does not resolve or does not decode,
// or an invariant of the referring object at that field, leaves the other
// diagnostic as the only one.
func TestANestedReferenceResolvesThroughTheSharedDiagnostic(t *testing.T) {
	ssh := "  access:\n    ssh: {addressRef: %s, auth: {operatorIdentity: {}}}\n"
	for _, row := range []struct {
		name, input string
		rules       []compilation.Rules
		want        []string
	}{
		{"a declared nested entry", nestedDocument("host", strings.Replace(ssh, "%s", "lan", 1)), nil, nil},
		{"an entry of the referring object", nestedDocument("host", strings.Replace(ssh, "%s", "wan", 1)), nil,
			[]string{"api.reference Machine/host $.spec.access.ssh.addressRef no address named wan is declared on Machine/host"}},
		{"an authored value outside the name grammar", nestedDocument("host", strings.Replace(ssh, "%s", "'Wan Link'", 1)), nil,
			[]string{"api.reference Machine/host $.spec.access.ssh.addressRef no address of the authored name is declared on Machine/host"}},
		{"an entry of another object", nestedDocument("host", "") + nestedProxy + nestedDocument("client", "  proxy: {proxyRef: edge, endpointRef: other}\n"), nil,
			[]string{"api.reference Machine/client $.spec.proxy.endpointRef no endpoint named other is declared on Proxy/edge"}},
		{"an owner that does not resolve", nestedDocument("client", "  proxy: {proxyRef: absent, endpointRef: other}\n"), nil,
			[]string{"api.reference Machine/client $.spec.proxy.proxyRef no Proxy named absent is in the selected input"}},
		{"an owner that does not decode", nestedDocument("host", "") + strings.Replace(nestedProxy, "  management: managed\n", "  management: managed\n  unknown: true\n", 1) + nestedDocument("client", "  proxy: {proxyRef: edge, endpointRef: other}\n"), nil,
			[]string{"api.field Proxy/edge $.spec.unknown"}},
		{"an invariant at the reference", nestedDocument("host", strings.Replace(ssh, "%s", "wan", 1)), []compilation.Rules{{Validate: func(object api.Object, _ api.Catalog) []api.Issue {
			if object.Identity() != "Machine/host" {
				return nil
			}
			return []api.Issue{{Code: "api.invariant", Field: "$.spec.access.ssh.addressRef", Message: "forbidden here", Remediation: "remove it"}}
		}}}, []string{"api.invariant Machine/host $.spec.access.ssh.addressRef forbidden here"}},
	} {
		t.Run(row.name, func(t *testing.T) {
			compiler := compilation.NewCompiler(yamlstream.Parser{}, nil, row.rules...)
			_, _, err := compiler.Compile(context.Background(), sources(environmentYAML+row.input))
			got := []string{}
			for _, d := range diagnostics.Of(err) {
				if d.Object == nil || d.Object.Kind == string(api.Environment) {
					continue
				}
				line := d.Code + " " + d.Object.Kind + "/" + d.Object.Name + " " + d.Field
				if d.Code != "api.field" {
					line += " " + d.Message
				}
				got = append(got, line)
			}
			if !slices.Equal(got, row.want) {
				t.Fatalf("diagnostics = %q, want %q (all %+v)", got, row.want, diagnostics.Of(err))
			}
		})
	}
}
