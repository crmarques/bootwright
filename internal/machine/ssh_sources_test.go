package machine

import (
	"slices"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

func TestSSHSourcePrefixesUseTheNamedNumericContact(t *testing.T) {
	record := func(fields ...api.FieldValue) api.Value { return api.MapValue(fields...) }
	contact := func(name, address string) api.Value {
		return record(api.FieldValue{Name: "name", Value: api.StringValue(name)}, api.FieldValue{Name: "address", Value: api.StringValue(address)})
	}
	controller := api.NewObject(api.Machine, "controller", api.Value{}, api.Value{}).WithSpec(api.Value{}.WithPath(
		api.ListValue(contact("lan", "192.0.2.2"), contact("fqdn", "controller.example.test")), "network", "addresses"))
	source := func(addressRef string) api.Value {
		return record(api.FieldValue{Name: "machine", Value: record(
			api.FieldValue{Name: "machineRef", Value: api.StringValue("controller")},
			api.FieldValue{Name: "addressRef", Value: api.StringValue(addressRef)},
		)})
	}
	target := api.NewObject(api.Machine, "target", api.Value{}, api.Value{})
	catalog := api.NewCatalog([]api.Object{controller})
	withSources := func(entries ...api.Value) api.Object {
		return target.WithSpec(target.Spec().WithPath(api.ListValue(entries...), "firewall", "ssh", "allowedSources"))
	}
	prefixes, issue := SSHSourcePrefixes(withSources(
		source("lan"),
		record(api.FieldValue{Name: "ip", Value: api.StringValue("192.0.2.2")}),
		record(api.FieldValue{Name: "cidr", Value: api.StringValue("2001:db8:12::/64")}),
	), catalog)
	if issue != nil || !slices.Equal(prefixes, []string{"192.0.2.2/32", "2001:db8:12::/64"}) {
		t.Fatalf("prefixes = %v, issue = %v", prefixes, issue)
	}
	for _, machine := range []api.Object{withSources(), withSources(source("fqdn")), withSources(source("absent"))} {
		if _, issue := SSHSourcePrefixes(machine, catalog); issue == nil {
			t.Fatalf("accepted invalid sources: %v", machine.Spec())
		}
	}
}
