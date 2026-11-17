package controller

import (
	"slices"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/addons"
	addonpackages "github.com/crmarques/bootwright/internal/addons/packages"
	"github.com/crmarques/bootwright/internal/containercluster"
	"github.com/crmarques/bootwright/internal/controller/evidence"
	"github.com/crmarques/bootwright/internal/infrastructureservices"
	"github.com/crmarques/bootwright/internal/substrate"
)

// SelectTools consumes only the selected effective graph. The artifact mirror
// changes acquisition, never target version or compatibility requirements.
func SelectTools(catalog api.Catalog) ([]evidence.ToolRequest, error) {
	return SelectToolsWithRegistry(catalog, addonpackages.Builtin())
}

func SelectToolsWithRegistry(catalog api.Catalog, registry addons.Registry) ([]evidence.ToolRequest, error) {
	environments := catalog.OfKind(api.Environment)
	if len(environments) != 1 {
		return nil, selectionFailure(api.Object{}, "api.invariant", "", "tool selection requires exactly one effective Environment")
	}
	versions, err := targetToolVersions(environments[0])
	if err != nil {
		return nil, err
	}
	virtctl := virtctlRequest(versions)
	result := []evidence.ToolRequest{}
	for _, cluster := range catalog.OfCategory(api.ContainerClusterCategory) {
		facts, _ := containercluster.FactsOf(cluster.Kind())
		tools, refused := facts.ControllerTools(cluster)
		if refused != nil {
			return nil, selectionFailure(cluster, refused.Code, refused.Field, refused.Message)
		}
		for _, tool := range tools {
			version := tool.Version
			if version == "" {
				version = versions[tool.Kind]
			}
			result = append(result, evidence.ToolRequest{Kind: tool.Kind, Version: version, Compatibility: tool.Compatibility})
		}
	}
	for _, binding := range catalog.OfKind(api.ClusterAddonBinding) {
		selected, issues := addons.ExpandBinding(binding, catalog)
		if len(issues) != 0 {
			return nil, selectionFailure(binding, "api.reference", "$.spec.addonRefs", "target tool selection requires fully resolved add-on bindings")
		}
		for _, addon := range selected {
			if !slices.Contains(addon.Spec().Get("provides").Strings(), "kubevirt") {
				continue
			}
			result = append(result, virtctl)
		}
	}
	instances, issues := addons.Instances(catalog, registry)
	if len(issues) > 0 {
		return nil, selectionFailure(api.Object{}, "api.reference", "$.spec.packages", "target tool selection requires fully resolved package capabilities")
	}
	for _, instance := range instances {
		if slices.Contains(instance.Package.Provides, "kubevirt") {
			result = append(result, virtctl)
		}
	}
	providers := map[string]bool{}
	for _, machine := range catalog.OfKind(api.Machine) {
		if name := machine.Spec().Get("substrate", "providerRef").Text(); name != "" {
			providers[name] = true
		}
	}
	for _, provider := range catalog.OfKind(api.InfraProvider) {
		if !providers[provider.Name()] {
			continue
		}
		for _, tool := range substrate.ProviderTools(provider) {
			if tool == virtctl.Kind {
				result = append(result, virtctl)
			} else {
				result = append(result, evidence.ToolRequest{Kind: tool, Version: versions[tool]})
			}
		}
	}
	for index := range result {
		result[index] = mirrored(catalog, result[index])
		if result[index], err = pinned(result[index]); err != nil {
			return nil, selectionFailure(environments[0], "controller.state", "", err.Error())
		}
	}
	slices.SortFunc(result, evidence.CompareToolRequests)
	result = slices.Compact(result)
	if len(result) > 128 {
		return nil, selectionFailure(environments[0], "controller.unsupported", "", "target tool selection exceeds its bounded closure")
	}
	return result, nil
}

func virtctlRequest(versions map[string]string) evidence.ToolRequest {
	return evidence.ToolRequest{Kind: "virtctl", Version: versions["virtctl"], Compatibility: "kubevirt"}
}

// ProviderTool is the client of one kind the selected Environment's
// controller stage installs for a provider's arm, which that arm's port runs.
func ProviderTool(catalog api.Catalog, kind string) (evidence.ToolRequest, error) {
	environments := catalog.OfKind(api.Environment)
	if len(environments) != 1 {
		return evidence.ToolRequest{}, selectionFailure(api.Object{}, "api.invariant", "", "tool selection requires exactly one effective Environment")
	}
	versions, err := targetToolVersions(environments[0])
	if err != nil {
		return evidence.ToolRequest{}, err
	}
	request := evidence.ToolRequest{Kind: kind, Version: versions[kind]}
	if kind == "virtctl" {
		request = virtctlRequest(versions)
	}
	if request, err = pinned(mirrored(catalog, request)); err != nil {
		return evidence.ToolRequest{}, selectionFailure(environments[0], "controller.state", "", err.Error())
	}
	return request, nil
}

// PublicOrigin says whether a client is served from a host any account
// publishes on, which a declared artifact mirror replaces.
func PublicOrigin(request evidence.ToolRequest) bool {
	switch request.Kind {
	case "helm", "govc", "kubectl", "virtctl":
		return true
	}
	return request.Compatibility == "okd"
}

func ArtifactMirror(catalog api.Catalog) (string, string) {
	mirror, _ := infrastructureservices.ArtifactMirrorOf(catalog)
	return mirror.URL, mirror.TrustBundleRef
}

func mirrored(catalog api.Catalog, request evidence.ToolRequest) evidence.ToolRequest {
	location, _ := ArtifactMirror(catalog)
	base := strings.TrimRight(location, "/")
	if base == "" || !PublicOrigin(request) {
		return request
	}
	request.Mirror = base + "/" + request.Kind
	if request.Kind == "helm" {
		request.Mirror += "/" + request.Version
	}
	return request
}

func targetToolVersions(environment api.Object) (map[string]string, error) {
	selected, err := selectDependencyVersions(environment)
	if err != nil {
		return nil, err
	}
	versions := map[string]string{"helm": selected.Helm, "govc": selected.Govc, "virtctl": selected.Virtctl, "kubectl": ""}
	for kind, value := range versions {
		if value == "" {
			pin, found, err := PinOf(kind)
			if err != nil || !found {
				return nil, selectionFailure(environment, "controller.state", "", "this build's tool table names no default "+kind+" release")
			}
			value = pin.Default.Version
		}
		versions[kind] = "v" + value
	}
	return versions, nil
}

func pinned(request evidence.ToolRequest) (evidence.ToolRequest, error) {
	pin, found, err := PinOf(request.Kind)
	if err != nil {
		return evidence.ToolRequest{}, err
	}
	compatibility := ""
	if request.Kind == "virtctl" {
		compatibility = "kubevirt"
	}
	if found && request.Compatibility == compatibility && strings.TrimPrefix(request.Version, "v") == pin.Default.Version {
		request.SHA256, request.Bytes = pin.Default.SHA256, pin.Default.Bytes
	}
	return request, nil
}
