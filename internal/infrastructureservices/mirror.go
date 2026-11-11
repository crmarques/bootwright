package infrastructureservices

import (
	"strconv"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

const (
	MirrorPath                   = "mirror"
	MirrorFillFamily             = "mirror-fill"
	OpenShiftReleaseRepository   = "quay.io/openshift-release-dev/ocp-release"
	OpenShiftComponentRepository = "quay.io/openshift-release-dev/ocp-v4.0-art-dev"
)

type ArtifactMirror struct {
	URL            string
	TrustBundleRef string
	Server         api.Object
	Managed        bool
}

func ArtifactMirrorOf(c api.Catalog) (ArtifactMirror, bool) {
	environments := c.OfKind(api.Environment)
	if len(environments) != 1 {
		return ArtifactMirror{}, false
	}
	artifacts := environments[0].Spec().Get("mirror", "artifacts")
	if !artifacts.Present() {
		return ArtifactMirror{}, false
	}
	mirror := ArtifactMirror{URL: artifacts.Get("url").Text(), TrustBundleRef: artifacts.Get("trustBundleRef").Text()}
	if !artifacts.Has("serverRef") {
		return mirror, mirror.URL != ""
	}
	server, found := c.Find(api.ArtifactServer, artifacts.Get("serverRef").Text())
	if !found || server.Spec().Get("management").Text() != "managed" {
		return mirror, false
	}
	mirror.Server, mirror.Managed = server, true
	if base := EndpointURL(c, server, artifacts.Get("endpointRef").Text()); base != "" {
		mirror.URL = strings.TrimSuffix(base, ":443") + "/" + MirrorPath + "/" + environments[0].Name()
	}
	if mirror.TrustBundleRef == "" {
		mirror.TrustBundleRef = IssuingAuthority(c, server.Spec().Get("tls", "secretRef").Text())
	}
	return mirror, true
}

type ImageMirror struct {
	Location       string
	CredentialsRef string
	TrustBundleRef string
	Registry       api.Object
	Managed        bool
}

func ImageMirrorOf(c api.Catalog) (ImageMirror, bool) {
	environments := c.OfKind(api.Environment)
	if len(environments) != 1 {
		return ImageMirror{}, false
	}
	selection := environments[0].Spec().Get("mirror", "registry")
	registry, found := c.Find(api.Registry, selection.Get("registryRef").Text())
	if !selection.Has("registryRef") || !found {
		return ImageMirror{}, false
	}
	spec := registry.Spec()
	mirror := ImageMirror{Registry: registry, CredentialsRef: spec.Get("credentialsRef").Text(), TrustBundleRef: spec.Get("trustBundleRef").Text()}
	if spec.Get("management").Text() != "managed" {
		mirror.Location = spec.Get("url").Text()
		return mirror, true
	}
	mirror.Managed = true
	if mirror.TrustBundleRef == "" {
		mirror.TrustBundleRef = IssuingAuthority(c, spec.Get("tls", "secretRef").Text())
	}
	endpoint, selected := namedEntry(spec.Get("endpoints"), selection.Get("endpointRef").Text())
	machine, placed := c.Find(api.Machine, spec.Get("machineRef").Text())
	port, numbered := spec.Get("port").Int64()
	if !selected || !placed || !numbered || port < 1 || port > 65535 {
		return mirror, true
	}
	if address := machineAddress(machine, endpoint.Get("addressRef").Text()); address != "" {
		if strings.Contains(address, ":") {
			address = "[" + address + "]"
		}
		mirror.Location = address + ":" + strconv.FormatInt(port, 10)
	}
	return mirror, true
}

func ImageMirrorHost(c api.Catalog) string {
	mirror, declared := ImageMirrorOf(c)
	if !declared || !mirror.Managed {
		return ""
	}
	return mirror.Registry.Spec().Get("machineRef").Text()
}

func MirrorHost(c api.Catalog) string {
	mirror, declared := ArtifactMirrorOf(c)
	if !declared || !mirror.Managed {
		return ""
	}
	return mirror.Server.Spec().Get("machineRef").Text()
}

func IssuingAuthority(c api.Catalog, certificate string) string {
	secret, found := c.Find(api.Secret, certificate)
	if certificate == "" || !found {
		return ""
	}
	issuer := secret.Spec().Get("source", "generated", "issuerRef").Text()
	if authority, found := c.Find(api.Secret, issuer); !found || authority.Spec().Get("type").Text() != "caBundle" {
		return ""
	}
	return issuer
}

func EndpointURL(c api.Catalog, server api.Object, endpointRef string) string {
	spec := server.Spec()
	endpoint, ok := namedEntry(spec.Get("endpoints"), endpointRef)
	if !ok {
		return ""
	}
	listener, ok := namedEntry(spec.Get("listeners"), endpoint.Get("listenerRef").Text())
	if !ok {
		return ""
	}
	machine, ok := c.Find(api.Machine, spec.Get("machineRef").Text())
	if !ok {
		return ""
	}
	address := machineAddress(machine, endpoint.Get("addressRef").Text())
	port, ok := listener.Get("port").Int64()
	if address == "" || !ok || port < 1 {
		return ""
	}
	if strings.Contains(address, ":") {
		address = "[" + address + "]"
	}
	return listener.Get("protocol").Text() + "://" + address + ":" + strconv.FormatInt(port, 10)
}

func namedEntry(values api.Value, name string) (api.Value, bool) {
	if name == "" {
		if values.Len() != 1 {
			return api.Value{}, false
		}
		return values.Items()[0], true
	}
	for _, item := range values.Items() {
		if item.Get("name").Text() == name {
			return item, true
		}
	}
	return api.Value{}, false
}
