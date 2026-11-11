package infrastructureservices

import (
	"strconv"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

const MirrorPath = "mirror"

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
