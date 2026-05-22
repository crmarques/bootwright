package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/cli/output"
	"github.com/crmarques/bootwright/internal/operator"
)

const (
	checkGroupControllerTools = "Controller tools"
	checkGroupInstallerTools  = "Installer tools"
	checkGroupSecretMaterial  = "Secret material"
)

func collectBastionChecks(state v1alpha1.State, hostStateDir string, deps preflightDeps) []preflightCheck {
	checks := []preflightCheck{
		pythonVersionCheck(deps),
		binaryCheck(checkGroupControllerTools, "ansible-playbook", []string{filepath.Join(ansibleVenvDir(), "bin")}, "bootwright apply bastion", deps),
		binaryCheck(checkGroupControllerTools, "tar", nil, "install tar on PATH", deps),
	}
	if deps.uid() != 0 {
		checks = append(checks, binaryCheck(checkGroupControllerTools, "sudo", nil, "install sudo on PATH or run bootwright as root", deps))
	}
	if releaseVersion := operator.StateOpenShiftReleaseVersion(state); releaseVersion != "" {
		checks = append(checks,
			binaryCheck(checkGroupInstallerTools, "kubectl", openshiftInstallSearchDirs(hostStateDir), "bootwright apply bastion", deps),
			ocpCLIVersionCheck(checkGroupInstallerTools, "oc", openshiftInstallSearchDirs(hostStateDir), releaseVersion, "bootwright apply bastion", deps),
			openshiftInstallVersionCheck(checkGroupInstallerTools, openshiftInstallSearchDirs(hostStateDir), releaseVersion, "bootwright apply bastion", deps),
		)
	}
	return checks
}

func pythonVersionCheck(deps preflightDeps) preflightCheck {
	name := "python3 >= 3.12"
	for _, bin := range []string{"python3.12", "python3"} {
		if _, err := deps.lookPath(bin, nil); err != nil {
			continue
		}
		out, err := deps.commandOutput(bin, "--version")
		if err != nil {
			continue
		}
		major, minor, err := operator.ParsePythonVersion(strings.TrimSpace(string(out)))
		if err != nil {
			continue
		}
		ver := fmt.Sprintf("%d.%d", major, minor)
		if major > 3 || (major == 3 && minor >= 12) {
			return okCheck(checkGroupControllerTools, name, bin+" "+ver)
		}
		return failCheck(checkGroupControllerTools, name, bin+" is "+ver, "Ansible runtime bootstrap requires Python 3.12 or newer", "bootwright apply bastion")
	}
	return failCheck(checkGroupControllerTools, name, "not found", "Bootwright cannot run the managed Ansible runtime", "bootwright apply bastion")
}

type preflightCheck = output.Check

type preflightDeps struct {
	lookPath      func(name string, extraDirs []string) (string, error)
	statPath      func(path string) (os.FileInfo, error)
	commandOutput func(name string, args ...string) ([]byte, error)
	uid           func() int
}

var defaultPreflightDeps = preflightDeps{
	lookPath: defaultLookPath,
	statPath: os.Stat,
	commandOutput: func(name string, args ...string) ([]byte, error) {
		return exec.Command(name, args...).CombinedOutput()
	},
	uid: os.Getuid,
}

func collectPreflightChecks(state v1alpha1.State, selected []Phase, hasState bool, secretsDir string, hostStateDir string, deps preflightDeps) []preflightCheck {
	checks := []preflightCheck{
		binaryCheck(checkGroupControllerTools, "ansible-playbook", []string{filepath.Join(ansibleVenvDir(), "bin")}, "bootwright apply bastion", deps),
		binaryCheck(checkGroupControllerTools, "python3", nil, "bootwright apply bastion", deps),
	}
	if phaseInScope("cluster", selected, hasState) && stateNeedsKubeVirt(state) {
		checks = append(checks, binaryCheck(checkGroupInstallerTools, "kubectl", nil, "install kubectl on PATH", deps))
	}
	if phaseInScope("clusters", selected, hasState) {
		releaseVersion := operator.StateOpenShiftReleaseVersion(state)
		checks = append(checks,
			binaryCheck(checkGroupInstallerTools, "kubectl", openshiftInstallSearchDirs(hostStateDir), "bootwright apply bastion", deps),
			ocpCLIVersionCheck(checkGroupInstallerTools, "oc", openshiftInstallSearchDirs(hostStateDir), releaseVersion, "bootwright apply bastion", deps),
			openshiftInstallVersionCheck(checkGroupInstallerTools, openshiftInstallSearchDirs(hostStateDir), releaseVersion, "bootwright apply bastion", deps),
		)
		if stateNeedsKubeVirt(state) {
			checks = append(checks, binaryCheck(checkGroupInstallerTools, "virtctl", nil, "install virtctl on PATH", deps))
		}
	}
	if hasState {
		checks = append(checks, secretRefChecks(state, secretsDir, selected, deps)...)
		checks = append(checks, generatedSelfSignedDriftChecks(state, secretsDir)...)
	}
	return checks
}

func phaseInScope(name string, selected []Phase, hasState bool) bool {
	if len(selected) == 0 {
		return hasState
	}
	for _, p := range selected {
		if p.Name == name {
			return true
		}
	}
	return false
}

func anyPhaseInScope(names []string, selected []Phase) bool {
	for _, name := range names {
		if phaseInScope(name, selected, true) {
			return true
		}
	}
	return false
}

func stateNeedsKubeVirt(state v1alpha1.State) bool {
	for _, p := range state.InfraProviders {
		for _, mp := range p.Spec.MachineProfiles {
			if mp.KubeVirt != nil {
				return true
			}
		}
	}
	return false
}

func binaryCheck(group, name string, extraDirs []string, remediation string, deps preflightDeps) preflightCheck {
	path, err := deps.lookPath(name, extraDirs)
	if err != nil {
		if remediation == "" {
			remediation = "install " + name + " on PATH"
		}
		return failCheck(group, name, "not found", "Required command is unavailable to this workflow", remediation)
	}
	return okCheck(group, name, path)
}

func ocpCLIVersionCheck(group, name string, extraDirs []string, wantVersion, remediation string, deps preflightDeps) preflightCheck {
	return binaryExactVersionCheck(group, name, extraDirs, wantVersion, remediation, deps, parseOCClientVersion, "version", "--client")
}

func openshiftInstallVersionCheck(group string, extraDirs []string, wantVersion, remediation string, deps preflightDeps) preflightCheck {
	return binaryExactVersionCheck(group, "openshift-install", extraDirs, wantVersion, remediation, deps, parseOpenShiftInstallVersion, "version")
}

func binaryExactVersionCheck(group, name string, extraDirs []string, wantVersion, remediation string, deps preflightDeps, parse func(string) string, args ...string) preflightCheck {
	if strings.TrimSpace(wantVersion) == "" {
		return binaryCheck(group, name, extraDirs, remediation, deps)
	}
	path, err := deps.lookPath(name, extraDirs)
	if err != nil {
		if remediation == "" {
			remediation = "install " + name + " on PATH"
		}
		return failCheck(group, name, "not found", "Required command is unavailable to this workflow", remediation)
	}
	out, err := deps.commandOutput(path, args...)
	if err != nil {
		return failCheck(group, name, path+" version probe failed", "Installed OpenShift CLI version could not be verified", remediation)
	}
	gotVersion := parse(string(out))
	if gotVersion == "" {
		return failCheck(group, name, path+" version could not be parsed", "Installed OpenShift CLI version could not be verified", remediation)
	}
	if gotVersion != wantVersion {
		return failCheck(group, name, path+" is "+gotVersion+"; want "+wantVersion, "Installed OpenShift CLI version does not match requested release", remediation)
	}
	return okCheck(group, name, path+" ("+gotVersion+")")
}

func parseOCClientVersion(out string) string {
	for _, line := range strings.Split(out, "\n") {
		key, value, ok := strings.Cut(line, ":")
		if ok && strings.TrimSpace(key) == "Client Version" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func parseOpenShiftInstallVersion(out string) string {
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && filepath.Base(fields[0]) == "openshift-install" {
			return fields[1]
		}
	}
	return ""
}

func secretsDirCheck(secretsDir string, deps preflightDeps) preflightCheck {
	name := "secrets directory"
	info, err := deps.statPath(secretsDir)
	if err != nil {
		return failCheck(checkGroupSecretMaterial, name, secretsDir+" missing", "Context secret material cannot be read", "mkdir -p "+secretsDir+" && chmod 0700 "+secretsDir)
	}
	if !info.IsDir() {
		return failCheck(checkGroupSecretMaterial, name, secretsDir+" is not a directory", "Context secret material cannot be read", "replace it with a directory")
	}
	if got := info.Mode().Perm(); got != 0o700 {
		return failCheck(checkGroupSecretMaterial, name, fmt.Sprintf("%s mode %04o; expected 0700", secretsDir, got), "Secret directory permissions are too broad or too narrow", "chmod 0700 "+secretsDir)
	}
	return okCheck(checkGroupSecretMaterial, name, secretsDir)
}

func secretFileCheck(refName, path, label string, publicKey, contextBacked bool, deps preflightDeps) preflightCheck {
	name := label
	info, err := deps.statPath(path)
	if err != nil {
		remediation := "create " + path + " or update Environment.spec.secrets[" + refName + "].file"
		switch {
		case strings.Contains(label, "pullSecretRef"):
			remediation = "bootwright secret set " + refName + " --pull-secret <path>"
		case strings.Contains(label, "credentialRef") || strings.Contains(label, "credentialsRef") || strings.Contains(label, "proxyAuthRef"):
			remediation = "bootwright secret set " + refName + " --from-file <path>"
		case strings.Contains(label, "sshKeyRef") && !contextBacked:
			remediation = "create the file declared by Environment.spec.secrets[" + refName + "].file"
		case contextBacked:
			remediation = "create " + path + " with bootwright secret set or declare Environment.spec.secrets[" + refName + "].file"
		}
		return failCheck(checkGroupSecretMaterial, name, path+" missing", "Referenced secret material is required before apply", remediation)
	}
	if info.IsDir() {
		return failCheck(checkGroupSecretMaterial, name, path+" is a directory", "Referenced secret material must be a regular file", "replace "+path+" with a regular file")
	}
	if !publicKey {
		if got := info.Mode().Perm(); got != 0o600 {
			return failCheck(checkGroupSecretMaterial, name, fmt.Sprintf("%s mode %04o; expected 0600", path, got), "Secret file permissions are too broad or too narrow", "chmod 0600 "+path)
		}
	}
	return okCheck(checkGroupSecretMaterial, name, path)
}

func generatedSecretCheck(refName, path, label, generatedKind string, deps preflightDeps) preflightCheck {
	name := label
	info, err := deps.statPath(path)
	if err != nil {
		remediation := "bootwright secret generate"
		if generatedKind == "credentials" {
			remediation = "bootwright secret generate or bootwright secret set " + refName + " --from-file <path>"
		}
		return failCheck(checkGroupSecretMaterial, name, path+" missing", "Generated secret material is required before apply", remediation)
	}
	if info.IsDir() {
		return failCheck(checkGroupSecretMaterial, name, path+" is a directory", "Generated secret material must be a regular file", "remove "+path+" and run bootwright secret generate")
	}
	if got := info.Mode().Perm(); got != 0o600 {
		return failCheck(checkGroupSecretMaterial, name, fmt.Sprintf("%s mode %04o; expected 0600", path, got), "Secret file permissions are too broad or too narrow", "chmod 0600 "+path)
	}
	return okCheck(checkGroupSecretMaterial, name, path)
}

func okCheck(group, name, evidence string) preflightCheck {
	return preflightCheck{
		Group:    group,
		Name:     name,
		Status:   output.StatusOK,
		Evidence: evidence,
	}
}

func failCheck(group, name, evidence, impact, remediation string) preflightCheck {
	return preflightCheck{
		Group:       group,
		Name:        name,
		Status:      output.StatusFail,
		Evidence:    evidence,
		Impact:      impact,
		Remediation: remediation,
	}
}
