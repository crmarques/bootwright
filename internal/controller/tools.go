package controller

import (
	"cmp"
	"slices"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/addons"
)

// ToolRequest describes a declarative tool requirement and publisher family.
// Setup freezes latest to an exact release before acquiring executable bytes.
type ToolRequest struct {
	Kind          string `json:"kind"`
	Version       string `json:"version"`
	Compatibility string `json:"compatibility"`
	Mirror        string `json:"mirror"`
}

// InstalledTool names one executable the controller stage published. A
// consumer asks for it by this identity so it runs the exact file that stage
// installed rather than whatever a search path offers.
type InstalledTool struct {
	Kind          string
	Compatibility string
	Version       string
	Executable    string
}

// SelectTools consumes only the selected effective graph. Download mirrors
// change acquisition, never target version or compatibility requirements.
func SelectTools(catalog api.Catalog) ([]ToolRequest, error) {
	environments := catalog.OfKind(api.Environment)
	if len(environments) != 1 {
		return nil, selectionFailure(api.Object{}, "api.invariant", "", "tool selection requires exactly one effective Environment")
	}
	downloads := environments[0].Spec().Get("downloads")
	versions, err := targetToolVersions(environments[0])
	if err != nil {
		return nil, err
	}
	virtctl := ToolRequest{Kind: "virtctl", Version: versions["virtctl"], Compatibility: "kubevirt", Mirror: downloads.Get("virtctlMirror").Text()}
	result := []ToolRequest{}
	for _, cluster := range catalog.OfKind(api.ContainerCluster) {
		distribution := cluster.Spec().Get("distribution")
		kind, version := distribution.Get("type").Text(), distribution.Get("release", "version").Text()
		// A pinned release image is the payload, not a client version. The
		// declared release still selects the clients; without one there is
		// nothing to match and setup refuses rather than inferring it.
		if (kind != "openshift" && kind != "okd") || version == "" {
			return nil, selectionFailure(cluster, "controller.unsupported", "$.spec.distribution.release", "target tools require an exact supported release version; declare release.version for a cluster pinned to a release image")
		}
		for _, tool := range []string{"openshift-clients", "openshift-install"} {
			result = append(result, ToolRequest{Kind: tool, Version: version, Compatibility: kind, Mirror: downloads.Get("openshiftClientsMirror").Text()})
		}
		result = append(result, ToolRequest{Kind: "helm", Version: versions["helm"], Mirror: downloads.Get("helmMirror").Text()})
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
		if provider.Spec().Has("vsphere") {
			result = append(result, ToolRequest{Kind: "govc", Version: versions["govc"]})
		}
		if provider.Spec().Has("kubevirt") {
			result = append(result, virtctl)
		}
	}
	slices.SortFunc(result, CompareToolRequests)
	result = slices.Compact(result)
	if len(result) > 128 {
		return nil, selectionFailure(environments[0], "controller.unsupported", "", "target tool selection exceeds its bounded closure")
	}
	return result, nil
}

func targetToolVersions(environment api.Object) (map[string]string, error) {
	selected, err := selectDependencyVersions(environment)
	if err != nil {
		return nil, err
	}
	versions := map[string]string{"helm": selected.Helm, "govc": selected.Govc, "virtctl": selected.Virtctl}
	for key, value := range versions {
		if value != "latest" {
			versions[key] = "v" + value
		}
	}
	return versions, nil
}

func CompareToolRequests(a, b ToolRequest) int {
	for _, pair := range [][2]string{{a.Kind, b.Kind}, {a.Version, b.Version}, {a.Compatibility, b.Compatibility}, {a.Mirror, b.Mirror}} {
		if order := cmp.Compare(pair[0], pair[1]); order != 0 {
			return order
		}
	}
	return 0
}
