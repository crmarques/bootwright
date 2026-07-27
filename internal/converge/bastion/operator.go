package bastion

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/render"
	"github.com/crmarques/bootwright/internal/roles"
)

type CLIInstallSpec struct {
	OCPReleaseVersion string
	InstallDir        string
	BundleDir         string
	Executable        string
	ClisReleaseURL    string
	HelmReleaseURL    string
	FIPSRequired      bool
}

func (s CLIInstallSpec) PlannedCommand(localInventoryName string) []string {
	argv := []string{
		s.Executable,
		"-i", filepath.Join(s.BundleDir, localInventoryName),
		roles.PlaybookWorkflowBastionApplyTools,
		"-e", "bootwright_openshift_release_version=" + s.OCPReleaseVersion,
		"-e", "bootwright_clis_install_dir=" + s.InstallDir,
		"-e", "bootwright_clis_release_url=" + s.ClisReleaseURL,
		"-e", "bootwright_helm_release_url=" + s.HelmReleaseURL,
	}
	if s.FIPSRequired {
		argv = append(argv, "-e", "bootwright_clis_fips_required=true")
	}
	return argv
}

func ComponentPinnedVersion(name string) (string, error) {
	for _, pin := range render.ComponentPins(v1alpha1.State{}) {
		if pin.Name == name {
			return pin.Version, nil
		}
	}
	return "", fmt.Errorf("%s pin missing from render.ComponentPins", name)
}

func AnsibleCorePinnedVersion() (string, error) {
	return ComponentPinnedVersion("ansible-core")
}

func StatePyvmomiPin(state v1alpha1.State) string {
	for _, pin := range render.ComponentPins(state) {
		if pin.Name == "pyvmomi" {
			return pin.Version
		}
	}
	return ""
}

func StateOpenShiftReleaseVersion(state v1alpha1.State) string {
	for _, cluster := range state.ContainerClusters {
		if v1alpha1.DistributionType(cluster) != v1alpha1.DistributionOpenShift {
			continue
		}
		if v := strings.TrimSpace(cluster.Spec.Distribution.Release.Version); v != "" {
			return v
		}
	}
	return ""
}

func StateRequiresFIPSInstaller(state v1alpha1.State) bool {
	for _, cluster := range state.ContainerClusters {
		if v1alpha1.DistributionType(cluster) != v1alpha1.DistributionOpenShift {
			continue
		}
		if cluster.Spec.Security.FIPS.Enabled {
			return true
		}
	}
	return false
}

func PlanCLIInstall(state v1alpha1.State, installDir, bundleDir string, venvBin func(name string) string) *CLIInstallSpec {
	version := StateOpenShiftReleaseVersion(state)
	if version == "" {
		return nil
	}
	return &CLIInstallSpec{
		OCPReleaseVersion: version,
		InstallDir:        installDir,
		BundleDir:         bundleDir,
		Executable:        venvBin("ansible-playbook"),
		ClisReleaseURL:    render.OpenShiftClientsReleaseURL(state, version),
		HelmReleaseURL:    render.HelmReleaseURL(state),
		FIPSRequired:      StateRequiresFIPSInstaller(state),
	}
}
