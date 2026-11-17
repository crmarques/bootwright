package infrastructureservices

import (
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
	endpoint, selected := NamedEntry(spec.Get("endpoints"), selection.Get("endpointRef").Text())
	machine, placed := c.Find(api.Machine, spec.Get("machineRef").Text())
	port, numbered := spec.Get("port").Int64()
	if !selected || !placed || !numbered || port < 1 || port > 65535 {
		return mirror, true
	}
	if address := machineAddress(machine, endpoint.Get("addressRef").Text()); address != "" {
		mirror.Location = HostPort(address, int(port))
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

type MirrorRefusal struct {
	Reason      string
	Remediation string
}

type ManagedMirror struct {
	Service   api.Object
	Field     string
	Host      string
	Addresses []string
	Conflict  *MirrorRefusal
	Unlocated *MirrorRefusal
}

func ManagedMirrors(c api.Catalog) []ManagedMirror {
	environments := c.OfKind(api.Environment)
	if len(environments) != 1 {
		return nil
	}
	var mirrors []ManagedMirror
	artifacts, declared := ArtifactMirrorOf(c)
	artifactsManaged := declared && artifacts.Managed
	if image, declared := ImageMirrorOf(c); declared && image.Managed {
		mirrors = append(mirrors, imageMirrorPlacement(c, image, artifacts, artifactsManaged))
	}
	if artifactsManaged {
		mirrors = append(mirrors, artifactMirrorPlacement(c, environments[0], artifacts))
	}
	return mirrors
}

func imageMirrorPlacement(c api.Catalog, image ImageMirror, artifacts ArtifactMirror, artifactsManaged bool) ManagedMirror {
	registry := image.Registry.Identity()
	mirror := managedMirror(c, image.Registry, "$.spec.mirror.registry.registryRef")
	if other := Placement(artifacts.Server); artifactsManaged && other != mirror.Host {
		mirror.Conflict = &MirrorRefusal{
			Reason:      "the image and artifact mirrors share one mirror host, and " + registry + " is on Machine/" + mirror.Host + " while " + artifacts.Server.Identity() + " is on Machine/" + other,
			Remediation: "place " + registry + " and " + artifacts.Server.Identity() + " on one Machine"}
	}
	if image.Location == "" {
		mirror.Unlocated = &MirrorRefusal{
			Reason:      "the image mirror is read at " + registry + "'s selected endpoint, and that endpoint names no address on Machine/" + mirror.Host,
			Remediation: "select an endpoint of " + registry + " with an address of Machine/" + mirror.Host + " in spec.mirror.registry.endpointRef"}
	}
	return mirror
}

func artifactMirrorPlacement(c api.Catalog, environment api.Object, artifacts ArtifactMirror) ManagedMirror {
	server := artifacts.Server.Identity()
	mirror := managedMirror(c, artifacts.Server, "$.spec.mirror.artifacts.serverRef")
	switch {
	case api.ValidLexical("artifact-mirror-url", artifacts.URL):
	case strings.HasPrefix(artifacts.URL, "https://"):
		mirror.Unlocated = &MirrorRefusal{
			Reason:      "the artifact mirror's URL " + artifacts.URL + " exceeds the bound a client path fits beneath",
			Remediation: "shorten the name of " + environment.Identity() + ", which the mirror's URL ends with, or select a shorter endpoint address of " + server}
	default:
		mirror.Unlocated = &MirrorRefusal{
			Reason:      "the artifact mirror is read over HTTPS beneath " + server + "'s selected endpoint, and that endpoint names no HTTPS URL a client path fits beneath",
			Remediation: "select an https endpoint of " + server + " in spec.mirror.artifacts.endpointRef of " + environment.Identity()}
	}
	return mirror
}

func managedMirror(c api.Catalog, service api.Object, field string) ManagedMirror {
	mirror := ManagedMirror{Service: service, Field: field, Host: Placement(service), Addresses: []string{service.Spec().Get("bindAddress").Text()}}
	machine, _ := c.Find(api.Machine, mirror.Host)
	for _, endpoint := range service.Spec().Get("endpoints").Items() {
		for _, entry := range machine.Spec().Get("network", "addresses").Items() {
			if entry.Get("name").Equal(endpoint.Get("addressRef")) {
				mirror.Addresses = append(mirror.Addresses, entry.Get("address").Text())
			}
		}
	}
	return mirror
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
