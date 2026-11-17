package infrastructureservices

import (
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

func failureCode(t *testing.T, err error) string {
	t.Helper()
	failure, ok := err.(*diagnostics.Failure)
	if !ok || len(failure.Diagnostics) != 1 {
		t.Fatalf("error = %#v", err)
	}
	return failure.Diagnostics[0].Code
}

func lookupGraph() api.Catalog {
	return api.NewCatalog([]api.Object{
		obj(api.Machine, "host", m("network", m("addresses", list(m("name", "ip", "address", "192.0.2.7"), m("name", "blank", "address", ""))))),
		obj(api.DNSServer, "managed", m("management", "managed", "machineRef", "host", "endpoints", list(m("name", "main", "addressRef", "ip"), m("name", "empty", "addressRef", "blank"), m("name", "dangling", "addressRef", "nope")))),
		obj(api.DNSServer, "unplaced", m("management", "managed", "machineRef", "gone", "endpoints", list(m("name", "main", "addressRef", "ip")))),
		obj(api.DNSServer, "external", m("management", "external", "address", "198.51.100.1")),
		obj(api.DNSServer, "addressless", m("management", "external")),
	})
}

func TestServiceEndpointResolvesWhereAConsumerReachesAService(t *testing.T) {
	catalog := lookupGraph()
	address, managed, err := ServiceEndpoint(catalog, api.DNSServer, m("serverRef", "managed", "endpointRef", "main"), "Environment/lab")
	if err != nil || address != "192.0.2.7" || managed != "managed" {
		t.Errorf("managed = %q, %q, %v", address, managed, err)
	}
	address, managed, err = ServiceEndpoint(catalog, api.DNSServer, m("serverRef", "external"), "Environment/lab")
	if err != nil || address != "198.51.100.1" || managed != "" {
		t.Errorf("external = %q, %q, %v", address, managed, err)
	}
}

func TestServiceEndpointRefusesWhatItCannotResolve(t *testing.T) {
	catalog := lookupGraph()
	for name, test := range map[string]struct {
		selection api.Value
		code      string
	}{
		"a service outside the graph": {m("serverRef", "absent"), "api.reference"},
		"an external without address": {m("serverRef", "addressless"), "api.required"},
		"a missing placement Machine": {m("serverRef", "unplaced", "endpointRef", "main"), "api.reference"},
		"an unnamed endpoint":         {m("serverRef", "managed", "endpointRef", "missing"), "api.reference"},
		"an unresolved address":       {m("serverRef", "managed", "endpointRef", "dangling"), "api.reference"},
		"an empty address":            {m("serverRef", "managed", "endpointRef", "empty"), "api.value"},
	} {
		address, managed, err := ServiceEndpoint(catalog, api.DNSServer, test.selection, "Environment/lab")
		if err == nil || address != "" || managed != "" {
			t.Errorf("%s: resolved %q, %q", name, address, managed)
			continue
		}
		if got := failureCode(t, err); got != test.code {
			t.Errorf("%s: code = %q, want %q", name, got, test.code)
		}
	}
}

func TestAddressResolutionRefusesAnEmptyReference(t *testing.T) {
	machine := obj(api.Machine, "host", m("network", m("addresses", list(m("name", "ip", "address", "192.0.2.7")))))
	if _, err := ResolveAddress(machine, ""); err == nil || failureCode(t, err) != "api.required" {
		t.Errorf("empty reference: %v", err)
	}
	if _, found := AddressOf(machine, ""); found {
		t.Error("an empty reference names an address")
	}
	if address, found := AddressOf(machine, "other"); found || address != "" {
		t.Errorf("an unknown name = %q, %t", address, found)
	}
}

func TestFormatPortWritesZeroForANonPositivePort(t *testing.T) {
	for port, want := range map[int]string{-1: "0", 0: "0", 53: "53"} {
		if got := FormatPort(port); got != want {
			t.Errorf("FormatPort(%d) = %q, want %q", port, got, want)
		}
	}
}

func TestNamedEntryNeedsAnUnambiguousChoice(t *testing.T) {
	two := list(m("name", "a"), m("name", "b"))
	if _, found := NamedEntry(two, ""); found {
		t.Error("two entries answered an empty name")
	}
	if entry, found := NamedEntry(list(m("name", "a")), ""); !found || entry.Get("name").Text() != "a" {
		t.Errorf("the sole entry = %v, %t", entry, found)
	}
	if entry, found := NamedEntry(two, "b"); !found || entry.Get("name").Text() != "b" {
		t.Errorf("named entry = %v, %t", entry, found)
	}
	if _, found := NamedEntry(two, "c"); found {
		t.Error("an unknown name resolved")
	}
}

func TestEndpointURLRefusesEveryBrokenLink(t *testing.T) {
	machine := obj(api.Machine, "host", m("network", m("addresses", list(m("name", "ip", "address", "192.0.2.7")))))
	listeners := list(m("name", "http", "protocol", "http", "port", api.IntegerValue("80")), m("name", "bad", "protocol", "http", "port", api.IntegerValue("0")))
	server := func(machineRef string, endpoints api.Value) api.Object {
		return obj(api.ArtifactServer, "files", m("machineRef", machineRef, "listeners", listeners, "endpoints", endpoints))
	}
	for name, test := range map[string]struct {
		server api.Object
		want   string
		code   string
	}{
		"resolves":              {server("host", list(m("name", "e", "listenerRef", "http", "addressRef", "ip"))), "http://192.0.2.7:80", ""},
		"unknown listener":      {server("host", list(m("name", "e", "listenerRef", "nope", "addressRef", "ip"))), "", "api.reference"},
		"missing machine":       {server("gone", list(m("name", "e", "listenerRef", "http", "addressRef", "ip"))), "", "api.reference"},
		"unresolved address":    {server("host", list(m("name", "e", "listenerRef", "http", "addressRef", "nope"))), "", "api.reference"},
		"listener without port": {server("host", list(m("name", "e", "listenerRef", "bad", "addressRef", "ip"))), "", "api.value"},
	} {
		catalog := api.NewCatalog([]api.Object{machine, test.server})
		got, err := ResolveEndpointURL(catalog, test.server, "e", "Environment/lab")
		if got != test.want || (err == nil) != (test.code == "") {
			t.Errorf("%s: %q, %v", name, got, err)
			continue
		}
		if err != nil {
			if code := failureCode(t, err); code != test.code {
				t.Errorf("%s: code = %q, want %q", name, code, test.code)
			}
		}
	}
}
