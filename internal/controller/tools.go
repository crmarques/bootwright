package controller

import (
	"slices"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/addons"
	addonpackages "github.com/crmarques/bootwright/internal/addons/packages"
	"github.com/crmarques/bootwright/internal/containercluster"
	"github.com/crmarques/bootwright/internal/controller/evidence"
	"github.com/crmarques/bootwright/internal/controller/tooltable"
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
	versions, err := tooltable.TargetVersions(environments[0])
	if err != nil {
		return nil, err
	}
	virtctl := tooltable.VirtctlRequest(versions)
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
			if !addons.Provides(addon, "kubevirt") {
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
	for _, machine := range catalog.Machines() {
		if name := machine.ProviderRef; name != "" {
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
		result[index] = tooltable.Mirrored(catalog, result[index])
		if result[index], err = tooltable.Pinned(result[index]); err != nil {
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
