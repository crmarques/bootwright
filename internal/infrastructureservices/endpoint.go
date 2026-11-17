package infrastructureservices

import (
	"strconv"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

func FormatPort(value int) string {
	if value <= 0 {
		return "0"
	}
	return strconv.Itoa(value)
}

// HostPort writes one socket as a URL does, bracketing an IPv6 host.
func HostPort(host string, port int) string {
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	return host + ":" + FormatPort(port)
}

func AddressOf(machine api.Object, reference string) (string, bool) {
	if reference == "" {
		return "", false
	}
	for _, address := range machine.Spec().Get("network", "addresses").Items() {
		if address.Get("name").Text() == reference {
			return address.Get("address").Text(), true
		}
	}
	return "", false
}

func machineAddress(machine api.Object, reference string) string {
	address, _ := AddressOf(machine, reference)
	return address
}

func ResolveAddress(machine api.Object, reference string) (string, error) {
	if reference == "" {
		return "", refusal("api.required", "the address reference is empty", "name an address on "+machine.Identity())
	}
	address, declared := AddressOf(machine, reference)
	if !declared {
		return "", refusal("api.reference", "the address reference does not resolve on its Machine", "name an address declared on "+machine.Identity())
	}
	if address == "" {
		return "", refusal("api.value", "the referenced Machine address is empty", "correct the address on "+machine.Identity())
	}
	return address, nil
}

func NamedEntry(values api.Value, name string) (api.Value, bool) {
	if name == "" {
		if values.Len() != 1 {
			return api.Value{}, false
		}
		return values.Items()[0], true
	}
	return values.Named("name", name)
}

func ResolveEndpointURL(catalog api.Catalog, server api.Object, endpointRef, identity string) (string, error) {
	endpoint, ok := NamedEntry(server.Spec().Get("endpoints"), endpointRef)
	if !ok {
		return "", refusal("api.reference", "the selected artifact server endpoint does not resolve",
			"correct artifactServerEndpoint.endpointRef on "+identity)
	}
	listener, ok := NamedEntry(server.Spec().Get("listeners"), endpoint.Get("listenerRef").Text())
	if !ok {
		return "", refusal("api.reference", "the selected endpoint names no listener on its server",
			"correct the endpoint on "+server.Identity())
	}
	machine, ok := catalog.Find(api.Machine, server.Spec().Get("machineRef").Text())
	if !ok {
		return "", refusal("api.reference", "the artifact server's placement Machine is not in the selected graph",
			"declare it or correct machineRef on "+server.Identity())
	}
	address, err := ResolveAddress(machine, endpoint.Get("addressRef").Text())
	if err != nil {
		return "", err
	}
	port, ok := listener.Get("port").Int64()
	if !ok || port < 1 {
		return "", refusal("api.value", "the selected listener declares no port", "correct the listener on "+server.Identity())
	}
	return listener.Get("protocol").Text() + "://" + HostPort(address, int(port)), nil
}

func EndpointURL(c api.Catalog, server api.Object, endpointRef string) string {
	location, err := ResolveEndpointURL(c, server, endpointRef, "")
	if err != nil {
		return ""
	}
	return location
}

func refusal(code, message, remediation string) error {
	return diagnostics.NewFailureWithRemediation(code, message, "", remediation)
}

// ServiceEndpoint resolves a name or time service selection to the address a
// consumer reaches it at, and the managed object whose block must complete first.
func ServiceEndpoint(catalog api.Catalog, kind api.Kind, selection api.Value, identity string) (string, string, error) {
	reference := selection.Get("serverRef").Text()
	server, ok := catalog.Find(kind, reference)
	if !ok {
		return "", "", refusal("api.reference", "a selected "+string(kind)+" is not in the selected graph", "declare "+reference+" or correct the selection on "+identity)
	}
	if server.Spec().Get("management").Text() != "managed" {
		address := server.Spec().Get("address").Text()
		if address == "" {
			return "", "", refusal("api.required", "the selected external "+string(kind)+" declares no address", "set spec.address on "+server.Identity())
		}
		return address, "", nil
	}
	placement, ok := catalog.Find(api.Machine, server.Spec().Get("machineRef").Text())
	if !ok {
		return "", "", refusal("api.reference", "the service's placement Machine is not in the selected graph", "declare it or correct machineRef on "+server.Identity())
	}
	endpoints := server.Spec().Get("endpoints")
	entry, found := NamedEntry(endpoints, selection.Get("endpointRef").Text())
	if !found {
		return "", "", refusal("api.reference", "a service selection names no endpoint", "set endpointRef on the selection of "+identity)
	}
	address, err := ResolveAddress(placement, entry.Get("addressRef").Text())
	if err != nil {
		return "", "", err
	}
	return address, server.Name(), nil
}
