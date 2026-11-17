package tooltable

import (
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/controller/evidence"
	"github.com/crmarques/bootwright/internal/infrastructureservices"
)

func VirtctlRequest(versions map[string]string) evidence.ToolRequest {
	return evidence.ToolRequest{Kind: "virtctl", Version: versions["virtctl"], Compatibility: "kubevirt"}
}

// ProviderTool is the client of one kind the selected Environment's
// controller stage installs for a provider's arm, which that arm's port runs.
func ProviderTool(catalog api.Catalog, kind string) (evidence.ToolRequest, error) {
	environments := catalog.OfKind(api.Environment)
	if len(environments) != 1 {
		return evidence.ToolRequest{}, Failure(api.Object{}, "api.invariant", "", "tool selection requires exactly one effective Environment")
	}
	versions, err := TargetVersions(environments[0])
	if err != nil {
		return evidence.ToolRequest{}, err
	}
	request := evidence.ToolRequest{Kind: kind, Version: versions[kind]}
	if kind == "virtctl" {
		request = VirtctlRequest(versions)
	}
	if request, err = Pinned(Mirrored(catalog, request)); err != nil {
		return evidence.ToolRequest{}, Failure(environments[0], "controller.state", "", err.Error())
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

func Mirrored(catalog api.Catalog, request evidence.ToolRequest) evidence.ToolRequest {
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

func TargetVersions(environment api.Object) (map[string]string, error) {
	selected, err := SelectDependencyVersions(environment)
	if err != nil {
		return nil, err
	}
	versions := map[string]string{"helm": selected.Helm, "govc": selected.Govc, "virtctl": selected.Virtctl, "kubectl": ""}
	for kind, value := range versions {
		if value == "" {
			pin, found, err := PinOf(kind)
			if err != nil || !found {
				return nil, Failure(environment, "controller.state", "", "this build's tool table names no default "+kind+" release")
			}
			value = pin.Default.Version
		}
		versions[kind] = "v" + value
	}
	return versions, nil
}

func Pinned(request evidence.ToolRequest) (evidence.ToolRequest, error) {
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
