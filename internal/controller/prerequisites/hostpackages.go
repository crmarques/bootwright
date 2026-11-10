package prerequisites

import (
	"errors"
	"regexp"
	"slices"
	"strings"
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
	baselineHostRequirements         = []string{"openssh-clients"}
	containerRuntimeHostRequirements = []string{"podman"}
	libvirtHostRequirements          = []string{"libvirt-client"}
	hypervisorRequirements           = []string{
		"libvirt-daemon", "libvirt-daemon-driver-network", "libvirt-daemon-driver-qemu",
		"libvirt-daemon-driver-storage-core", "qemu-img", "qemu-kvm-core", "swtpm", "swtpm-tools",
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
	if selected.ContainerRuntime {
		requirements = append(requirements, containerRuntimeHostRequirements...)
	}
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
	if selected.IPUtils {
		requirements = append(requirements, "iputils")
	}
	return requirements
}

type InstalledPackage struct {
	NEVRA       string `json:"nevra"`
	InstallTime int64  `json:"installTime"`
}

var installedNEVRA = regexp.MustCompile(`^([A-Za-z0-9][A-Za-z0-9._+-]*)-([0-9]+):([A-Za-z0-9._+~^]+)-([A-Za-z0-9._+~^]+)\.([A-Za-z0-9_]+)$`)

const MaxAddedPackages = 1024

func packageParts(p InstalledPackage) []string {
	if len(p.NEVRA) > 256 || p.InstallTime <= 0 {
		return nil
	}
	return installedNEVRA.FindStringSubmatch(p.NEVRA)
}

func ValidInstalledPackage(p InstalledPackage) bool { return packageParts(p) != nil }

func packageIdentity(p InstalledPackage) string {
	if match := packageParts(p); match != nil {
		return match[1] + "." + match[5]
	}
	return ""
}

func PackageArgument(p InstalledPackage) string {
	if match := packageParts(p); match != nil {
		return match[1] + "-" + match[3] + "-" + match[4] + "." + match[5]
	}
	return ""
}

func TransactionAdded(before, after []InstalledPackage) []InstalledPackage {
	held := map[string]bool{}
	for _, item := range before {
		held[packageIdentity(item)] = true
	}
	added := []InstalledPackage{}
	for _, item := range after {
		if ValidInstalledPackage(item) && !held[packageIdentity(item)] {
			added = append(added, item)
		}
	}
	return MergeAdded(added)
}

func MergeAdded(sets ...[]InstalledPackage) []InstalledPackage {
	merged := []InstalledPackage{}
	seen := map[string]bool{}
	for _, set := range sets {
		for _, item := range set {
			if identity := packageIdentity(item); identity != "" && !seen[identity] {
				seen[identity] = true
				merged = append(merged, item)
			}
		}
	}
	slices.SortFunc(merged, func(a, b InstalledPackage) int { return strings.Compare(a.NEVRA, b.NEVRA) })
	return merged
}
