// Package operator owns the controller-side bootstrap and OpenShift
// CLI installation domain. Inputs are state + path parameters; outputs
// are plans (slices of BootstrapStep) and command specs. Side-effectful
// runners live in the calling CLI layer so this package stays testable
// without process exec.
//
// The split exists because earlier these decisions lived inline in
// internal/cli/operator.go, which had no tests and mixed flag parsing
// with the planning logic. Pulling the planners here gives them their
// own package boundary and a unit-test surface.
package operator

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/provisioning/render"
)

// BootstrapStep is one labelled command in the controller bootstrap
// sequence. The CLI runs each step in order and aborts on the first
// non-zero exit.
type BootstrapStep struct {
	Label string
	Cmd   []string
}

// CLIInstallSpec captures the inputs to the controller CLI install
// playbook (downloading openshift-install / oc / kubectl into the
// requested install directory at the pinned OCP release version).
type CLIInstallSpec struct {
	OCPReleaseVersion string
	InstallDir        string
	BundleDir         string
	Executable        string
}

type ProcessDeps struct {
	LookPath      func(string) (string, error)
	CommandOutput func(string, ...string) ([]byte, error)
	UID           func() int
}

var DefaultProcessDeps = ProcessDeps{
	LookPath: exec.LookPath,
	CommandOutput: func(name string, args ...string) ([]byte, error) {
		return exec.Command(name, args...).CombinedOutput()
	},
	UID: os.Getuid,
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

// ParsePythonVersion parses "Python 3.12.4" → (3, 12). The leading
// "Python " is optional so callers can also pass already-trimmed
// version strings.
func ParsePythonVersion(s string) (major, minor int, err error) {
	s = strings.TrimPrefix(s, "Python ")
	parts := strings.SplitN(s, ".", 3)
	if len(parts) < 2 {
		return 0, 0, fmt.Errorf("unexpected version string: %q", s)
	}
	major, err = strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, fmt.Errorf("parse major: %w", err)
	}
	minor, err = strconv.Atoi(parts[1])
	if err != nil {
		return 0, 0, fmt.Errorf("parse minor: %w", err)
	}
	return major, minor, nil
}

// ResolvePython312 walks PATH for a python3.12-or-newer interpreter
// and returns its binary name + true if found. Used by the bootstrap
// planner to decide whether to add an install step.
func ResolvePython312() (string, bool) {
	return ResolvePython312With(DefaultProcessDeps)
}

func ResolvePython312With(deps ProcessDeps) (string, bool) {
	for _, bin := range []string{"python3.12", "python3"} {
		if _, err := deps.LookPath(bin); err != nil {
			continue
		}
		if pythonAtLeast312(deps, bin) {
			return bin, true
		}
	}
	return "", false
}

func pythonAtLeast312(deps ProcessDeps, bin string) bool {
	out, err := deps.CommandOutput(bin, "--version")
	if err != nil {
		return false
	}
	major, minor, err := ParsePythonVersion(strings.TrimSpace(string(out)))
	if err != nil {
		return false
	}
	return major > 3 || (major == 3 && minor >= 12)
}

// Python312InstallCmd returns the platform-appropriate install command
// for python3.12 (dnf or apt-get), prefixed with sudo when not running
// as root. Returns nil when no supported package manager is on PATH.
func Python312InstallCmd(preserveProxyEnv bool) []string {
	return Python312InstallCmdWith(DefaultProcessDeps, preserveProxyEnv)
}

func Python312InstallCmdWith(deps ProcessDeps, preserveProxyEnv bool) []string {
	type pkgMgr struct {
		bin  string
		args []string
	}
	for _, pm := range []pkgMgr{
		{"dnf", []string{"dnf", "install", "-y", "python3.12"}},
		{"apt-get", []string{"apt-get", "install", "-y", "python3.12"}},
	} {
		if _, err := deps.LookPath(pm.bin); err != nil {
			continue
		}
		if deps.UID() == 0 {
			return pm.args
		}
		if _, err := deps.LookPath("sudo"); err == nil {
			return SudoPackageInstallCmd(pm.args, preserveProxyEnv)
		}
		return pm.args
	}
	return nil
}

func rootManagedCmdWith(deps ProcessDeps, args []string, preserveProxyEnv bool) []string {
	out := append([]string(nil), args...)
	if deps.UID() == 0 {
		return out
	}
	if _, err := deps.LookPath("sudo"); err == nil {
		return SudoPackageInstallCmd(out, preserveProxyEnv)
	}
	return out
}

// SudoPackageInstallCmd wraps an install command with sudo (and the
// proxy --preserve-env list when the caller asked for ambient proxy
// inheritance).
func SudoPackageInstallCmd(args []string, preserveProxyEnv bool) []string {
	out := []string{"sudo"}
	if preserveProxyEnv {
		out = append(out, "--preserve-env="+SudoPreservedProxyVars)
	}
	return append(out, args...)
}

// SudoPreservedProxyVars is the comma-joined env-var list that sudo
// should inherit when running install commands behind an HTTP proxy.
const SudoPreservedProxyVars = "HTTP_PROXY,HTTPS_PROXY,NO_PROXY,http_proxy,https_proxy,no_proxy"

// BootstrapPlan computes the controller bootstrap sequence: optionally
// install python3.12, create a venv at venvDir, install pinned pip,
// and install pinned ansible-core. venvBin returns the absolute path of a
// venv binary so the plan stays free of cli-package path helpers.
func BootstrapPlan(venvDir string, venvBin func(name string) string, preserveProxyEnv bool, rootManagedVenv bool) ([]BootstrapStep, error) {
	return BootstrapPlanWith(DefaultProcessDeps, venvDir, venvBin, preserveProxyEnv, rootManagedVenv)
}

func BootstrapPlanWith(deps ProcessDeps, venvDir string, venvBin func(name string) string, preserveProxyEnv bool, rootManagedVenv bool) ([]BootstrapStep, error) {
	pin, err := AnsibleCorePinnedVersion()
	if err != nil {
		return nil, err
	}
	pipPin, err := ComponentPinnedVersion("pip")
	if err != nil {
		return nil, err
	}
	python, found := ResolvePython312With(deps)
	var steps []BootstrapStep
	if !found {
		installCmd := Python312InstallCmdWith(deps, preserveProxyEnv)
		if installCmd == nil {
			return nil, fmt.Errorf("python3.12 not found; install it manually or ensure dnf or apt-get is available")
		}
		label := "install python3.12"
		if installCmd[0] == "sudo" {
			label += " (requires sudo)"
		}
		steps = append(steps, BootstrapStep{
			Label: label,
			Cmd:   installCmd,
		})
		python = "python3.12"
	}
	venvPython := venvBin("python")
	venvMissing := !pythonAtLeast312(deps, venvPython)
	if venvMissing {
		venvCmd := []string{python, "-m", "venv", venvDir}
		if rootManagedVenv {
			venvCmd = rootManagedCmdWith(deps, venvCmd, preserveProxyEnv)
		}
		steps = append(steps, bootstrapStep("create ansible-core venv at "+venvDir, venvCmd))
	}
	if venvMissing || pipVersion(deps, venvBin("pip")) != pipPin {
		pipCmd := []string{venvBin("pip"), "install", "pip==" + pipPin}
		if rootManagedVenv {
			pipCmd = rootManagedCmdWith(deps, pipCmd, preserveProxyEnv)
		}
		steps = append(steps, bootstrapStep("install pip=="+pipPin+" in venv", pipCmd))
	}
	if venvMissing || pythonPackageVersion(deps, venvPython, "ansible-core") != pin {
		ansibleCmd := []string{venvBin("pip"), "install", "ansible-core==" + pin}
		if rootManagedVenv {
			ansibleCmd = rootManagedCmdWith(deps, ansibleCmd, preserveProxyEnv)
		}
		steps = append(steps, bootstrapStep("install ansible-core=="+pin+" into venv", ansibleCmd))
	}
	return steps, nil
}

func bootstrapStep(label string, cmd []string) BootstrapStep {
	if len(cmd) > 0 && cmd[0] == "sudo" {
		label += " (requires sudo)"
	}
	return BootstrapStep{Label: label, Cmd: cmd}
}

func pipVersion(deps ProcessDeps, pipBin string) string {
	out, err := deps.CommandOutput(pipBin, "--version")
	if err != nil {
		return ""
	}
	fields := strings.Fields(string(out))
	if len(fields) < 2 || fields[0] != "pip" {
		return ""
	}
	return fields[1]
}

func pythonPackageVersion(deps ProcessDeps, pythonBin string, pkg string) string {
	out, err := deps.CommandOutput(pythonBin, "-m", "pip", "show", pkg)
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(out), "\n") {
		key, value, ok := strings.Cut(line, ":")
		if ok && strings.TrimSpace(key) == "Version" {
			return strings.TrimSpace(value)
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

// MergeBootstrapEnv returns a new env slice with bootstrap-relevant
// overlays applied: ambient HTTP_PROXY/HTTPS_PROXY/NO_PROXY are
// stripped (callers re-inject the values they want), then `extra`
// pairs are appended. Used by the CLI runner that exec's the
// bootstrap steps.
func MergeBootstrapEnv(base []string, extra map[string]string) []string {
	return mergeEnv(stripProxyEnv(base), extra)
}

func mergeEnv(base []string, extra map[string]string) []string {
	if len(extra) == 0 {
		out := make([]string, len(base))
		copy(out, base)
		return out
	}
	overrides := map[string]bool{}
	for k := range extra {
		overrides[k] = true
	}
	out := make([]string, 0, len(base)+len(extra))
	for _, kv := range base {
		eq := strings.IndexByte(kv, '=')
		if eq > 0 && overrides[kv[:eq]] {
			continue
		}
		out = append(out, kv)
	}
	for k, v := range extra {
		out = append(out, k+"="+v)
	}
	return out
}

func stripProxyEnv(env []string) []string {
	if len(env) == 0 {
		return env
	}
	out := make([]string, 0, len(env))
	for _, kv := range env {
		eq := strings.IndexByte(kv, '=')
		if eq <= 0 {
			out = append(out, kv)
			continue
		}
		if _, ok := bootstrapProxyEnvKeys[kv[:eq]]; ok {
			continue
		}
		out = append(out, kv)
	}
	return out
}

var bootstrapProxyEnvKeys = map[string]struct{}{
	"HTTP_PROXY":  {},
	"HTTPS_PROXY": {},
	"NO_PROXY":    {},
	"http_proxy":  {},
	"https_proxy": {},
	"no_proxy":    {},
}
