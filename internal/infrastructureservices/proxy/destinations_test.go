package proxy

import (
	"reflect"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/infrastructureservices/managedservice"
)

func m(kv ...any) api.Value {
	fields := []api.FieldValue{}
	for i := 0; i < len(kv); i += 2 {
		value, ok := kv[i+1].(api.Value)
		if !ok {
			value = api.StringValue(kv[i+1].(string))
		}
		fields = append(fields, api.FieldValue{Name: kv[i].(string), Value: value})
	}
	return api.MapValue(fields...)
}

func mirrored(artifacts, registry string) api.Catalog {
	environment := api.NewObject(api.Environment, "lab", m(), m("mirror", m("artifacts", m("url", artifacts), "registry", m("registryRef", "mirror"))))
	return api.NewCatalog([]api.Object{environment, api.NewObject(api.Registry, "mirror", m(), m("management", "external", "url", registry))})
}

func TestAProxyFreezesItsDeclaredDestinationsAndTheEnvironmentsMirrorHosts(t *testing.T) {
	declared := api.StringList("quay.io:443", ".redhat.com", "2001:DB8::50", "[2001:db8::50]:8443", "198.51.100.50", "mirror.lab.example.test:443")
	found, err := destinations(mirrored("https://mirror.lab.example.test/tools", "registry.lab.example.test:8443/mirror"), declared)
	want := []managedservice.Destination{
		{Domain: ".redhat.com"}, {Domain: "mirror.lab.example.test", Port: 443}, {Domain: "quay.io", Port: 443},
		{Domain: "registry.lab.example.test", Port: 8443},
		{Address: "198.51.100.50"}, {Address: "2001:db8::50"}, {Address: "2001:db8::50", Port: 8443},
	}
	if err != nil || !reflect.DeepEqual(found, want) {
		t.Fatalf("destinations = %+v (%v)", found, err)
	}
	found, err = destinations(mirrored("https://[2001:db8::80]/tools", "192.0.2.80/mirror"), api.StringList(".redhat.com"))
	want = []managedservice.Destination{{Domain: ".redhat.com"}, {Address: "192.0.2.80", Port: 443}, {Address: "2001:db8::80", Port: 443}}
	if err != nil || !reflect.DeepEqual(found, want) {
		t.Fatalf("address mirrors = %+v (%v)", found, err)
	}
}

func TestAProxyWithoutAllowedDestinationsFreezesNoneAndAnEmptyListRefuses(t *testing.T) {
	catalog := mirrored("https://mirror.lab.example.test/tools", "registry.lab.example.test")
	if found, err := destinations(catalog, api.Value{}); err != nil || found != nil {
		t.Fatalf("an undeclared list froze %+v (%v)", found, err)
	}
	if _, err := destinations(catalog, api.ListValue()); err == nil {
		t.Fatal("an empty list froze as a proxy admitting every destination")
	}
	if _, err := destinations(catalog, api.StringList("*.redhat.com")); err == nil {
		t.Fatal("an entry outside the grammar froze")
	}
}

func TestADerivedMirrorHostOutsideTheGrammarNamesTheMirror(t *testing.T) {
	_, err := destinations(mirrored("https://[fe80::1%25eth0]/tools", "registry.lab.example.test"), api.StringList(".redhat.com"))
	if reported := diagnostics.Of(err); err == nil || len(reported) != 1 || !strings.Contains(reported[0].Remediation, "spec.mirror") {
		t.Fatalf("a zoned mirror host froze or blamed the authored list: %v", err)
	}
}
