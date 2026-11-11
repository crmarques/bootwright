package agentinstall

import (
	"net/url"
	"slices"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/infrastructureservices"
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
	environment, declared := infrastructureservices.ImageMirrorOf(catalog)
	if !selection.Present() && !declared {
		return nil, nil
	}
	registry := environment.Registry
	if selection.Present() {
		found := false
		if registry, found = catalog.Find(api.Registry, selection.Get("registryRef").Text()); !found {
			return nil, refusal("api.reference", "the selected registry mirror is absent", "declare the Registry selected by "+cluster.Identity())
		}
		if declared && registry.Name() != environment.Registry.Name() {
			return nil, refusal("api.invariant", "the cluster selects "+registry.Identity()+" as its registry mirror beside the Environment's image mirror "+environment.Registry.Identity(),
				"select Registry "+environment.Registry.Name()+" in install.registries.mirror.registryRef on "+cluster.Identity()+", or remove install.registries.mirror")
		}
	}
	location, trust := registry.Spec().Get("url").Text(), registry.Spec().Get("trustBundleRef").Text()
	if registry.Spec().Get("management").Text() != "external" {
		if !declared {
			return nil, refusal("lifecycle.unsupported", "a managed Registry is a cluster's mirror only as the Environment's image mirror, whose host copies the release",
				"select an external Registry on "+cluster.Identity()+", or name "+registry.Identity()+" in spec.mirror.registry.registryRef on the Environment")
		}
		if reason, remedy := mirroredReleaseRefusal(cluster); reason != "" {
			return nil, refusal("lifecycle.unsupported", reason, remedy)
		}
		location, trust = environment.Location, environment.TrustBundleRef
	}
	if trust != "" && !slices.Contains(trustBundleRefs(catalog, cluster), trust) {
		return nil, refusal("api.invariant", "the registry mirror trust bundle is not selected by the cluster", "add Secret "+trust+" to install.additionalTrustBundleRefs on "+cluster.Identity())
	}
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

func mirroredReleaseRefusal(cluster api.Object) (reason, remediation string) {
	image := cluster.Spec().Get("distribution", "release", "image").Text()
	if image == "" {
		return "", ""
	}
	repository, digest, _ := strings.Cut(image, "@")
	tag := ""
	if index := strings.LastIndex(repository, ":"); index > strings.LastIndex(repository, "/") {
		repository, tag = repository[:index], repository[index+1:]
	}
	if repository != infrastructureservices.OpenShiftReleaseRepository {
		return "the image mirror's host copies a release only from " + infrastructureservices.OpenShiftReleaseRepository,
			"pin spec.distribution.release.image on " + cluster.Identity() + " in " + infrastructureservices.OpenShiftReleaseRepository + ", or remove it"
	}
	if (digest == "" || tag != "") && tag != cluster.Spec().Get("distribution", "release", "version").Text()+"-x86_64" {
		return "the image mirror's host copies a release by its digest or by its version's x86_64 tag alone",
			"pin spec.distribution.release.image on " + cluster.Identity() + " by digest with no other tag, or remove it"
	}
	return "", ""
}

func MirroredRelease(catalog api.Catalog, cluster api.Object) (digest string, ok bool) {
	mirror, err := registryMirror(catalog, cluster)
	environment, declared := infrastructureservices.ImageMirrorOf(catalog)
	if err != nil || mirror == nil || !declared || !environment.Managed || cluster.Spec().Get("distribution", "type").Text() != "openshift" {
		return "", false
	}
	_, digest, _ = strings.Cut(cluster.Spec().Get("distribution", "release", "image").Text(), "@")
	return digest, true
}

func trustBundleRefs(catalog api.Catalog, cluster api.Object) []string {
	references := cluster.Spec().Get("install", "additionalTrustBundleRefs").Strings()
	if mirror, declared := infrastructureservices.ImageMirrorOf(catalog); declared {
		if trust := mirror.TrustBundleRef; trust != "" && !slices.Contains(references, trust) {
			references = append(slices.Clone(references), trust)
		}
	}
	return references
}

var releaseRepositories = map[string][]string{
	"okd":       {"quay.io/okd/scos-content", "quay.io/okd/scos-release"},
	"openshift": {infrastructureservices.OpenShiftReleaseRepository, infrastructureservices.OpenShiftComponentRepository},
}

func ImageDigestSources(catalog api.Catalog, cluster api.Object) []any {
	var sources []any
	var authored []string
	for _, source := range cluster.Spec().Get("install", "registries", "imageDigestSources").Items() {
		authored = append(authored, source.Get("source").Text())
		sources = append(sources, nativeValue(source))
	}
	mirror, declared := infrastructureservices.ImageMirrorOf(catalog)
	if !declared {
		return sources
	}
	location := strings.TrimRight(mirror.Location, "/")
	distribution := cluster.Spec().Get("distribution", "type").Text()
	if distribution != "okd" {
		distribution = "openshift"
	}
	for _, source := range releaseRepositories[distribution] {
		if !mapped(authored, source) && location != "" {
			sources = append(sources, map[string]any{"source": source, "mirrors": []any{location + "/" + source}, "sourcePolicy": "NeverContactSource"})
		}
	}
	return sources
}

func mapped(sources []string, repository string) bool {
	return slices.ContainsFunc(sources, func(source string) bool {
		return repository == source || strings.HasPrefix(repository, source+"/")
	})
}

func NeverContacted(catalog api.Catalog, cluster api.Object, repository string) bool {
	var never []string
	for _, source := range ImageDigestSources(catalog, cluster) {
		if entry, _ := source.(map[string]any); entry["sourcePolicy"] == "NeverContactSource" {
			origin, _ := entry["source"].(string)
			never = append(never, origin)
		}
	}
	return mapped(never, repository)
}
