package agentinstall

import (
	"net/url"
	"slices"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

// RegistryMirror names only the credential binding and the host it authorizes.
// The attempt merges its material into the pull secret before building media.
type RegistryMirror struct {
	CredentialsRef  string `json:"credentialsRef,omitempty"`
	CredentialsType string `json:"credentialsType,omitempty"`
	Host            string `json:"host"`
}

func registryMirror(catalog api.Catalog, cluster api.Object) (*RegistryMirror, error) {
	selection := cluster.Spec().Get("install", "registries", "mirror")
	if !selection.Present() {
		return nil, nil
	}
	registry, found := catalog.Find(api.Registry, selection.Get("registryRef").Text())
	if !found {
		return nil, refusal("api.reference", "the selected registry mirror is absent", "declare the Registry selected by "+cluster.Identity())
	}
	if registry.Spec().Get("management").Text() != "external" {
		return nil, refusal("lifecycle.unsupported", "this executable installs through external registry mirrors only", "select an external Registry on "+cluster.Identity())
	}
	if trust := registry.Spec().Get("trustBundleRef").Text(); trust != "" && !slices.Contains(cluster.Spec().Get("install", "additionalTrustBundleRefs").Strings(), trust) {
		return nil, refusal("api.invariant", "the registry mirror trust bundle is not selected by the cluster", "add Secret "+trust+" to install.additionalTrustBundleRefs on "+cluster.Identity())
	}
	location := registry.Spec().Get("url").Text()
	if !strings.Contains(location, "://") {
		location = "https://" + location
	}
	parsed, err := url.Parse(location)
	if err != nil || parsed.Host == "" || parsed.User != nil {
		return nil, refusal("api.value", "the registry mirror has no usable host", "correct the selected Registry URL")
	}
	mirror := &RegistryMirror{Host: parsed.Host, CredentialsRef: registry.Spec().Get("credentialsRef").Text()}
	if mirror.CredentialsRef != "" {
		declaration, found := catalog.Find(api.Secret, mirror.CredentialsRef)
		if !found {
			return nil, refusal("api.reference", "the registry mirror credential Secret is absent", "declare Secret "+mirror.CredentialsRef)
		}
		mirror.CredentialsType = declaration.Spec().Get("type").Text()
		if mirror.CredentialsType != "usernamePassword" && mirror.CredentialsType != "dockerConfigJson" {
			return nil, refusal("api.value", "the registry mirror credential Secret has an unsupported type", "select a usernamePassword or dockerConfigJson Secret")
		}
	}
	return mirror, nil
}
