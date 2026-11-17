package artifactserver

import (
	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/infrastructureservices"
	"github.com/crmarques/bootwright/internal/infrastructureservices/managedservice"
	machineref "github.com/crmarques/bootwright/internal/machine"
)

// servedRoot is the directory this server serves, beneath its own content
// root, and privatePrefix is the subtree beneath it that only one named
// machine may read.
const (
	servedRoot    = "public"
	privatePrefix = "private"
)

// Publication is one piece of content a consumer owns beneath this server's
// served root, and the URL a fetcher reads it at. The server owns the root; a
// consumer owns exactly the subtree it publishes and removes that subtree in
// its own inverse.
type Publication struct {
	Path string
	URL  string
}

// Selected resolves the managed server one consumer selects. A consumer
// publishes only into a managed server, because an external one is nobody's to
// write into.
func Selected(catalog api.Catalog, selection api.Value, identity string) (api.Object, error) {
	server, ok := catalog.Find(api.ArtifactServer, selection.Get("serverRef").Text())
	if !ok {
		return api.Object{}, refusal("api.reference", "the selected artifact server is not in the selected graph",
			"declare it or correct artifactServerEndpoint.serverRef on "+identity)
	}
	if server.Spec().Get("management").Text() != "managed" {
		return api.Object{}, refusal("lifecycle.state", "a consumer publishes only into a managed artifact server",
			"select a managed server on "+identity)
	}
	return server, nil
}

// PublicPath is the subtree `<consumer>/<object>/` one consumer owns beneath
// the served root, and the file it publishes there.
func PublicPath(catalog api.Catalog, server api.Object, selection api.Value, contextName, consumer, object, leaf, identity string) (Publication, error) {
	base, err := infrastructureservices.ResolveEndpointURL(catalog, server, selection.Get("endpointRef").Text(), identity)
	if err != nil {
		return Publication{}, err
	}
	return Publication{
		Path: ContentRoot(contextName, server.Name()) + "/" + servedRoot + "/" + consumer + "/" + object + "/" + leaf,
		URL:  base + "/" + consumer + "/" + object + "/" + leaf,
	}, nil
}

// PrivatePath is the subtree a consumer owns for material only one machine may
// read, and the certificate that machine verifies the fetch against. It may
// also carry that machine's own image, when the image names the private URL.
// The unguessable final segment is not here: the attempt that publishes mints
// it, so the plan, the evidence and every log name only the parent. A consumer
// that hands this URL to a remote fetcher sends it only over a verified
// channel.
func PrivatePath(catalog api.Catalog, server api.Object, selection api.Value, contextName, consumer, object, identity string) (Publication, string, error) {
	base, err := infrastructureservices.ResolveEndpointURL(catalog, server, selection.Get("endpointRef").Text(), identity)
	if err != nil {
		return Publication{}, "", err
	}
	if !hasPrefix(base, "https://") {
		return Publication{}, "", refusal("lifecycle.state", "material only one machine may read is served only over a verified connection",
			"select an https endpoint on "+identity)
	}
	certificate := server.Spec().Get("tls", "secretRef").Text()
	if certificate == "" {
		return Publication{}, "", refusal("api.required", "the selected artifact server declares no serving certificate to verify",
			"set spec.tls.secretRef on "+server.Identity())
	}
	return Publication{
		Path: ContentRoot(contextName, server.Name()) + "/" + servedRoot + "/" + privatePrefix + "/" + consumer + "/" + object,
		URL:  base + "/" + privatePrefix + "/" + consumer + "/" + object,
	}, certificate, nil
}

// ContentRoot is the host directory this server owns for one context. A
// consumer publishes beneath the root that server created, so the layout has
// one owner rather than one copy per consumer.
func ContentRoot(contextName, server string) string {
	return managedservice.ContentRoot(contextName, "artifact-server", server)
}

func ServedRoot(contextName, server string) string {
	return ContentRoot(contextName, server) + "/" + servedRoot
}

// PlacementFor is the arm a consumer's publication runs through: the Machine
// this server is placed on, reached the way managed services reach it.
func PlacementFor(catalog api.Catalog, server api.Object, controllerMachine string) (machineref.Placement, error) {
	machine, ok := catalog.Find(api.Machine, server.Spec().Get("machineRef").Text())
	if !ok {
		return machineref.Placement{}, refusal("api.reference", "the artifact server's placement Machine is not in the selected graph",
			"declare it or correct machineRef on "+server.Identity())
	}
	return machineref.PlacementFor(machine, controllerMachine)
}

// ServicePlacementFor is the arm the server's own block and its mirror fill run
// through, which reaches a Machine this context installs as well.
func ServicePlacementFor(catalog api.Catalog, server api.Object, controllerMachine string) (machineref.Placement, error) {
	machine, ok := catalog.Find(api.Machine, server.Spec().Get("machineRef").Text())
	if !ok {
		return machineref.Placement{}, refusal("api.reference", "the artifact server's placement Machine is not in the selected graph",
			"declare it or correct machineRef on "+server.Identity())
	}
	return machineref.ServicePlacementFor(machine, controllerMachine)
}

func hasPrefix(value, prefix string) bool {
	return len(value) >= len(prefix) && value[:len(prefix)] == prefix
}
