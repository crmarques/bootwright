// Package bastion owns local bootstrap planning and controller-side OpenShift
// CLI installation planning. Inputs are state + path parameters; outputs
// are plans (slices of BootstrapStep) and command specs. Side-effectful
// runners live in the calling CLI layer so this package stays testable
// without process exec.
//
// The split exists because earlier these decisions lived inline in
// internal/cli/operator.go, which had no tests and mixed flag parsing
// with the planning logic. Pulling the planners here gives them their
// own package boundary and a unit-test surface.
package bastion

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/render"
)

// CLIInstallSpec captures the inputs to the controller CLI install
// playbook (downloading openshift-install / oc / kubectl into the
// requested install directory at the pinned OCP release version).
type CLIInstallSpec struct {
	OCPReleaseVersion string
	InstallDir        string
	BundleDir         string
	Executable        string
}

// PlannedCommand returns the ansible-playbook invocation the CLI will
// run to materialise the controller-side OpenShift CLIs. The returned
// argv is fully resolved (absolute paths against BundleDir) so callers
// can echo it on dry-run and pass it straight to exec.Command.
func (s CLIInstallSpec) PlannedCommand(localInventoryName string) []string {
	return []string{
		s.Executable,
		"-i", filepath.Join(s.BundleDir, localInventoryName),
		filepath.Join(s.BundleDir, "playbooks", "targets", "bastion", "apply-clis.yml"),
		"-e", "bootwright_openshift_release_version=" + s.OCPReleaseVersion,
		"-e", "bootwright_clis_install_dir=" + s.InstallDir,
	}
}

// ComponentPinnedVersion returns a controller/runtime pin declared in
// render.ComponentPins. Single source of truth for bootstrap tooling.
func ComponentPinnedVersion(name string) (string, error) {
	for _, pin := range render.ComponentPins(v1alpha1.State{}) {
		if pin.Name == name {
			return pin.Version, nil
		}
	}
	return "", fmt.Errorf("%s pin missing from render.ComponentPins", name)
}

// AnsibleCorePinnedVersion returns the pinned ansible-core version
// declared in render.ComponentPins. Single source of truth for what
// goes into the controller venv.
func AnsibleCorePinnedVersion() (string, error) {
	return ComponentPinnedVersion("ansible-core")
}

// StateOpenShiftReleaseVersion returns the first non-empty OpenShift
// release version declared on any ContainerCluster. Empty when no
// fixture pins a release (controller CLI install becomes a no-op then).
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

// PlanCLIInstall returns a non-nil spec when the loaded state pins an
// OpenShift release. nil means "no release pinned — skip CLI install".
// venvBin lets the caller resolve `ansible-playbook` without coupling
// us to the CLI's path layout.
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
	}
}
