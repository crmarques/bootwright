package artifactserver

import (
	"net/netip"
	"slices"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/infrastructureservices/managedservice"
	machineref "github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

// Unsupported lists every artifact server this capability cannot realize, in
// canonical order. Kinds no capability claims at all are the engine's own
// refusal, not this capability's.
func Unsupported(catalog api.Catalog) []string {
	return lifecycle.Identities(Refusals(catalog))
}

// Refusals refuses every artifact server this capability cannot realize, with
// its reason and remedy.
func Refusals(catalog api.Catalog) []lifecycle.Refusal {
	var found []lifecycle.Refusal
	for _, server := range catalog.OfKind(api.ArtifactServer) {
		if server.Spec().Get("management").Text() == "managed" && server.Spec().Get("retention").Text() == "install-only" {
			found = append(found, lifecycle.RefusalOf(server,
				"this executable serves no managed artifact server with install-only retention",
				"declare spec.retention: persistent on "+server.Identity()+", or omit it"))
		}
	}
	return lifecycle.SortRefusals(found)
}

// Requests derives one frozen request per managed artifact server, in
// canonical object order. It reads no host, endpoint or Secret material.
func Requests(catalog api.Catalog, controllerMachine, contextName string) ([]Request, error) {
	if !api.ValidLexical("name", contextName) {
		return nil, refusal("lifecycle.state", "the lifecycle context identity is invalid", "")
	}
	servers := catalog.OfKind(api.ArtifactServer)
	slices.SortFunc(servers, func(x, y api.Object) int { return strings.Compare(x.Name(), y.Name()) })
	var requests []Request
	for _, server := range servers {
		if server.Spec().Get("management").Text() != "managed" {
			continue
		}
		request, err := requestFor(catalog, server, controllerMachine, contextName)
		if err != nil {
			return nil, err
		}
		requests = append(requests, request)
	}
	return requests, nil
}

func requestFor(catalog api.Catalog, server api.Object, controllerMachine, contextName string) (Request, error) {
	name := server.Name()
	if !managedservice.SafeSegment(name) {
		return Request{}, refusal("lifecycle.state", "the artifact server name is not a safe host identifier", "rename "+server.Identity())
	}
	spec := server.Spec()
	machine, found := catalog.Find(api.Machine, spec.Get("machineRef").Text())
	if !found {
		return Request{}, refusal("api.reference", "the artifact server's placement Machine is not in the selected graph", "declare "+spec.Get("machineRef").Text()+" or change the reference")
	}
	placement, err := machineref.PlacementFor(machine, controllerMachine)
	if err != nil {
		return Request{}, err
	}
	image, err := managedservice.ImageFor(spec, server.Identity(), defaultImage)
	if err != nil {
		return Request{}, err
	}
	egress, err := managedservice.EgressFor(catalog, machine)
	if err != nil {
		return Request{}, err
	}
	listeners, err := listenersFor(spec, server.Identity())
	if err != nil {
		return Request{}, err
	}
	endpoints, err := endpointsFor(spec, machine, listeners, server.Identity())
	if err != nil {
		return Request{}, err
	}
	request := Request{
		BindAddress: spec.Get("bindAddress").Text(),
		ContentRoot: ContentRoot(contextName, name),
		Egress:      egress,
		Endpoints:   endpoints,
		Identity:    Identity{Block: BlockID(name), Context: contextName, Service: name},
		Image:       image,
		Listeners:   listeners,
		Placement:   placement,
		Unit:        unitPrefix + "-" + contextName + "-artifacts-" + name,
		Version:     requestVersion,
	}
	if request.BindAddress == "" {
		return Request{}, refusal("api.required", "the managed artifact server has no effective bind address", "set spec.bindAddress on "+server.Identity())
	}
	if _, err := netip.ParseAddr(request.BindAddress); err != nil {
		return Request{}, refusal("api.value", "the managed artifact server bind address is not an IP literal", "set spec.bindAddress on "+server.Identity())
	}
	if request.usesTLS() {
		reference := spec.Get("tls", "secretRef").Text()
		if reference == "" {
			return Request{}, refusal("api.required", "an HTTPS artifact listener requires a serving certificate", "set spec.tls.secretRef on "+server.Identity())
		}
		minimum := spec.Get("tls", "minVersion").Text()
		if minimum == "" {
			minimum = "TLSv1.2"
		}
		request.TLS = &TLS{MinVersion: minimum, Secret: reference}
	}
	return request, nil
}

func BlockID(service string) string { return "artifact-server-" + service }

func listenersFor(spec api.Value, identity string) ([]Listener, error) {
	items := spec.Get("listeners").Items()
	if len(items) == 0 {
		return nil, refusal("api.required", "a managed artifact server requires at least one listener", "declare spec.listeners on "+identity)
	}
	listeners := make([]Listener, 0, len(items))
	for _, item := range items {
		port, ok := item.Get("port").Int64()
		if !ok || port < 1 || port > 65535 {
			return nil, refusal("api.value", "an artifact listener port is out of range", "correct spec.listeners on "+identity)
		}
		protocol := item.Get("protocol").Text()
		if protocol != "http" && protocol != "https" {
			return nil, refusal("api.value", "an artifact listener protocol is not recognized", "correct spec.listeners on "+identity)
		}
		listeners = append(listeners, Listener{Name: item.Get("name").Text(), Port: int(port), Protocol: protocol})
	}
	slices.SortFunc(listeners, func(x, y Listener) int { return strings.Compare(x.Name, y.Name) })
	return listeners, nil
}

func endpointsFor(spec api.Value, machine api.Object, listeners []Listener, identity string) ([]Endpoint, error) {
	items := spec.Get("endpoints").Items()
	endpoints := make([]Endpoint, 0, len(items))
	for _, item := range items {
		listener := item.Get("listenerRef").Text()
		if !slices.ContainsFunc(listeners, func(candidate Listener) bool { return candidate.Name == listener }) {
			return nil, refusal("api.reference", "an artifact endpoint names an unknown listener", "correct spec.endpoints on "+identity)
		}
		address, err := machineref.ResolveAddress(machine, item.Get("addressRef").Text())
		if err != nil {
			return nil, err
		}
		endpoints = append(endpoints, Endpoint{Address: address, Listener: listener, Name: item.Get("name").Text()})
	}
	slices.SortFunc(endpoints, func(x, y Endpoint) int { return strings.Compare(x.Name, y.Name) })
	return endpoints, nil
}

func refusal(code, message, remediation string) error {
	return diagnostics.NewFailureWithRemediation(code, message, "", remediation)
}
