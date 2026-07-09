package bastion

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/crmarques/bootwright/internal/host/execution"
)

type BootstrapStep struct {
	Label string
	Cmd   []string
}

type ProcessDeps struct {
	LookPath      func(string) (string, error)
	CommandOutput func(string, ...string) ([]byte, error)
	UID           func() int
}

var DefaultProcessDeps = ProcessDeps{
	LookPath: execution.LookPath,
	CommandOutput: func(name string, args ...string) ([]byte, error) {
		var combined bytes.Buffer
		err := execution.OSRunner{}.Run(context.Background(), execution.Command{Name: name, Args: args, Stdout: &combined, Stderr: &combined})
		return combined.Bytes(), err
	},
	UID: os.Getuid,
}

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

func ResolvePython312With(deps ProcessDeps) (string, bool) {
	for _, bin := range pythonCandidateBins() {
		path, err := deps.LookPath(bin)
		if err != nil {
			continue
		}
		if path == "" {
			path = bin
		}
		if pythonAtLeast312(deps, path) {
			return path, true
		}
	}
	return "", false
}

func pythonCandidateBins() []string {
	out := make([]string, 0, 10)
	for minor := 12; minor <= 20; minor++ {
		out = append(out, fmt.Sprintf("python3.%d", minor))
	}
	return append(out, "python3")
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

func SudoPackageInstallCmd(args []string, preserveProxyEnv bool) []string {
	out := []string{"sudo"}
	if preserveProxyEnv {
		out = append(out, "--preserve-env="+SudoPreservedProxyVars)
	}
	return append(out, args...)
}

const SudoPreservedProxyVars = "HTTP_PROXY,HTTPS_PROXY,NO_PROXY,http_proxy,https_proxy,no_proxy"

func BootstrapPlanWith(deps ProcessDeps, venvDir string, venvBin func(name string) string, preserveProxyEnv bool, rootManagedVenv bool, pyvmomiPin string) ([]BootstrapStep, error) {
	pin, err := AnsibleCorePinnedVersion()
	if err != nil {
		return nil, err
	}
	pipPin, err := ComponentPinnedVersion("pip")
	if err != nil {
		return nil, err
	}
	var steps []BootstrapStep
	venvPython := venvBin("python")
	venvUsable := pythonAtLeast312(deps, venvPython)
	venvPipVersion := ""
	venvAnsibleVersion := ""
	venvPyvmomiVersion := ""
	if venvUsable {
		venvPipVersion = pythonPipVersion(deps, venvPython)
		venvAnsibleVersion = pythonPackageVersion(deps, venvPython, "ansible-core")
		if pyvmomiPin != "" {
			venvPyvmomiVersion = pythonPackageVersion(deps, venvPython, "pyvmomi")
		}
	}
	pyvmomiPinned := pyvmomiPin == "" || venvPyvmomiVersion == pyvmomiPin
	pyvmomiStep := func() BootstrapStep {
		cmd := []string{venvPython, "-m", "pip", "install", "pyvmomi==" + pyvmomiPin}
		if rootManagedVenv {
			cmd = rootManagedCmdWith(deps, cmd, preserveProxyEnv)
		}
		return bootstrapStep("install pyvmomi=="+pyvmomiPin+" into venv", cmd)
	}
	if venvUsable && venvPipVersion == pipPin && venvAnsibleVersion == pin && pyvmomiPinned {
		return steps, nil
	}
	python, found := ResolvePython312With(deps)
	if !found && venvUsable {
		if venvPipVersion != pipPin {
			pipCmd := []string{venvPython, "-m", "pip", "install", "pip==" + pipPin}
			if rootManagedVenv {
				pipCmd = rootManagedCmdWith(deps, pipCmd, preserveProxyEnv)
			}
			steps = append(steps, bootstrapStep("install pip=="+pipPin+" in venv", pipCmd))
		}
		if venvAnsibleVersion != pin {
			ansibleCmd := []string{venvPython, "-m", "pip", "install", "ansible-core==" + pin}
			if rootManagedVenv {
				ansibleCmd = rootManagedCmdWith(deps, ansibleCmd, preserveProxyEnv)
			}
			steps = append(steps, bootstrapStep("install ansible-core=="+pin+" into venv", ansibleCmd))
		}
		if !pyvmomiPinned {
			steps = append(steps, pyvmomiStep())
		}
		return steps, nil
	}
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
	venvCmd := []string{python, "-m", "venv", "--clear", venvDir}
	if rootManagedVenv {
		venvCmd = rootManagedCmdWith(deps, venvCmd, preserveProxyEnv)
	}
	steps = append(steps, bootstrapStep("create ansible-core venv at "+venvDir, venvCmd))
	pipCmd := []string{venvPython, "-m", "pip", "install", "pip==" + pipPin}
	if rootManagedVenv {
		pipCmd = rootManagedCmdWith(deps, pipCmd, preserveProxyEnv)
	}
	steps = append(steps, bootstrapStep("install pip=="+pipPin+" in venv", pipCmd))
	ansibleCmd := []string{venvPython, "-m", "pip", "install", "ansible-core==" + pin}
	if rootManagedVenv {
		ansibleCmd = rootManagedCmdWith(deps, ansibleCmd, preserveProxyEnv)
	}
	steps = append(steps, bootstrapStep("install ansible-core=="+pin+" into venv", ansibleCmd))
	if pyvmomiPin != "" {
		steps = append(steps, pyvmomiStep())
	}
	return steps, nil
}

func bootstrapStep(label string, cmd []string) BootstrapStep {
	if len(cmd) > 0 && cmd[0] == "sudo" {
		label += " (requires sudo)"
	}
	return BootstrapStep{Label: label, Cmd: cmd}
}

func pythonPipVersion(deps ProcessDeps, pythonBin string) string {
	out, err := deps.CommandOutput(pythonBin, "-m", "pip", "--version")
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
