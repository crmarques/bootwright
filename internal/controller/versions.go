package controller

import (
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

// DependencyVersions is version intent, not evidence of a resolved release.
// Each value is latest or one exact release. An override selects no capability.
type DependencyVersions struct {
	Python  string `json:"python"`
	Ansible string `json:"ansible"`
	Podman  string `json:"podman"`
	OpenSSH string `json:"openssh"`
	NMState string `json:"nmstate"`
	Libvirt string `json:"libvirt"`
	Helm    string `json:"helm"`
	Govc    string `json:"govc"`
	Virtctl string `json:"virtctl"`
}

func DefaultDependencyVersions() DependencyVersions {
	return DependencyVersions{"latest", "latest", "latest", "latest", "latest", "latest", "latest", "latest", "latest"}
}

func selectDependencyVersions(environment api.Object) (DependencyVersions, error) {
	versions := DefaultDependencyVersions()
	configured := environment.Spec().Get("dependencyVersions")
	if configured.Present() && configured.Type() != api.Mapping {
		return DependencyVersions{}, selectionFailure(environment, "api.value", "$.spec.dependencyVersions", "dependency versions must be an object")
	}
	for _, field := range configured.Fields() {
		var target *string
		grammar := "cli-version"
		switch field.Name {
		case "python":
			target = &versions.Python
		case "ansible":
			target = &versions.Ansible
		case "helm":
			target = &versions.Helm
		case "govc":
			target = &versions.Govc
		case "virtctl":
			target = &versions.Virtctl
		case "podman":
			target, grammar = &versions.Podman, "package-version"
		case "openssh":
			target, grammar = &versions.OpenSSH, "package-version"
		case "nmstate":
			target, grammar = &versions.NMState, "package-version"
		case "libvirt":
			target, grammar = &versions.Libvirt, "package-version"
		default:
			return DependencyVersions{}, selectionFailure(environment, "api.value", "$.spec.dependencyVersions."+field.Name, "dependency version names must identify a supported dependency")
		}
		if field.Value.Type() != api.String || !api.ValidLexical(grammar, field.Value.Text()) {
			return DependencyVersions{}, selectionFailure(environment, "api.value", "$.spec.dependencyVersions."+field.Name, "dependency version must be latest or an exact supported release")
		}
		*target = field.Value.Text()
		if grammar == "cli-version" {
			*target = strings.TrimPrefix(*target, "v")
		}
	}
	return versions, nil
}
