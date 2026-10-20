package prerequisites

import (
	"errors"
	"regexp"
	"slices"
)

type HostPackagePreview struct {
	Text      string   `json:"text"`
	Truncated bool     `json:"truncated,omitempty"`
	Notes     []string `json:"notes,omitempty"`
}

type HostPackageEvidence struct {
	Requirement string `json:"requirement"`
	Package     string `json:"package"`
	Repository  string `json:"repository,omitempty"`
}

var (
	baselineHostRequirements = []string{"openssh-clients", "podman"}
	libvirtHostRequirements  = []string{"libvirt-client"}
	hypervisorRequirements   = []string{
		"libvirt-daemon", "libvirt-daemon-driver-network", "libvirt-daemon-driver-qemu",
		"libvirt-daemon-driver-storage-core", "qemu-img", "qemu-kvm", "swtpm", "swtpm-tools",
	}
	installerMediaRequirements = []string{"lorax", "xorriso"}
	hostRequirement            = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]*(\([A-Za-z0-9._+-]+\))?( = [0-9]+:?[A-Za-z0-9._+~^-]*)?$`)
	hostPythonMinor            = regexp.MustCompile(`^3\.(0|[1-9][0-9]*)$`)
	hostPythonRequirement      = regexp.MustCompile(`^python(3\.(0|[1-9][0-9]*))$`)
)

func ValidHostRequirement(requirement string) bool {
	return len(requirement) <= 128 && hostRequirement.MatchString(requirement)
}

func BaselineHostRequirements(pythonMinor string) ([]string, error) {
	requirements := slices.Clone(baselineHostRequirements)
	if pythonMinor == "" {
		return requirements, nil
	}
	if !hostPythonMinor.MatchString(pythonMinor) {
		return nil, errors.New("the locked interpreter names no CPython minor")
	}
	return append(requirements, "python"+pythonMinor), nil
}

func PythonRequirementABI(requirement string) (string, bool) {
	match := hostPythonRequirement.FindStringSubmatch(requirement)
	if match == nil {
		return "", false
	}
	return match[1], true
}

func StageHostRequirements(selected NativeRequirements) []string {
	requirements := []string{}
	if selected.LibvirtClient {
		requirements = append(requirements, libvirtHostRequirements...)
	}
	if selected.Hypervisor {
		requirements = append(requirements, hypervisorRequirements...)
	}
	if selected.InstallerMedia {
		requirements = append(requirements, installerMediaRequirements...)
	}
	if selected.NMState {
		requirements = append(requirements, "nmstate")
	}
	return requirements
}
