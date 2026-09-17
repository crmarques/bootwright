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
	// InstallerMedia is the intent of the image-building tooling. It is not
	// declarable: the tooling reads media the operator already imported, and
	// no consumer has asked to pin it. The hypervisor closure has no intent of
	// its own either, because it runs the release its client speaks. It is
	// omitted when unset so a record written before it existed still encodes
	// to the bytes it was persisted as.
	InstallerMedia string `json:"installerMedia,omitempty"`
	Helm           string `json:"helm"`
	Govc           string `json:"govc"`
	Virtctl        string `json:"virtctl"`
}

func DefaultDependencyVersions() DependencyVersions {
	return DependencyVersions{
		Python: "latest", Ansible: "latest", Podman: "latest", OpenSSH: "latest",
		NMState: "latest", Libvirt: "latest", InstallerMedia: "latest",
		Helm: "latest", Govc: "latest", Virtctl: "latest",
	}
}

// Baseline is the version intent of the context-independent prerequisites
// setup owns. A context declares only the versions its own controller stage
// installs, so those values never select or supersede a retained resolution.
func (v DependencyVersions) Baseline() DependencyVersions {
	return DependencyVersions{Python: v.Python, Ansible: v.Ansible, Podman: v.Podman, OpenSSH: v.OpenSSH, NMState: v.NMState}
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
		case "helm":
			target = &versions.Helm
		case "govc":
			target = &versions.Govc
		case "virtctl":
			target = &versions.Virtctl
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
