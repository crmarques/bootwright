// Package preflight collects environmental-readiness checks — bastion tools,
// installer tools, secret material, SSH host trust, and addon prerequisites —
// from desired state and the local host. It returns plain Check data;
// presentation lives with the callers.
package preflight

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/converge/bastion"
	"github.com/crmarques/bootwright/internal/host/callerio"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/workspace"
)

const (
	checkGroupControllerTools = "Bastion tools"
	checkGroupInstallerTools  = "Installer tools"
	checkGroupSecretMaterial  = "Secret material"
)

// Status mirrors the CLI output status vocabulary for the values preflight
// checks emit, so rendered text is identical after the boundary mapping.
type Status string

const (
	StatusOK   Status = "OK"
	StatusFail Status = "FAIL"
	StatusWarn Status = "WARN"
)

type Check struct {
	Group       string
	Name        string
	Status      Status
	Evidence    string
	Impact      string
	Remediation string
}

// Phase is the slice of a workflow phase preflight scoping needs: its name.
type Phase struct {
	Name string
}

func CollectBastionChecks(deps Deps) []Check {
	checks := []Check{
		pythonVersionCheck(deps),
		binaryCheck(checkGroupControllerTools, "ansible-playbook", []string{filepath.Join(workspace.AnsibleVenvDir(), "bin")}, "bootwright bastion setup", deps),
		binaryCheck(checkGroupControllerTools, "tar", nil, "install tar on PATH", deps),
	}
	if deps.UID() != 0 {
		checks = append(checks, binaryCheck(checkGroupControllerTools, "sudo", nil, "install sudo on PATH or run bootwright as root", deps))
	}
	return checks
}

func pythonVersionCheck(deps Deps) Check {
	name := "python3 >= 3.12"
	for _, bin := range []string{"python3.12", "python3"} {
		path, err := deps.LookPath(bin, nil)
		if err != nil {
			continue
		}
		out, err := deps.CommandOutput(path, "--version")
		if err != nil {
			continue
		}
		major, minor, err := bastion.ParsePythonVersion(strings.TrimSpace(string(out)))
		if err != nil {
			continue
		}
		ver := fmt.Sprintf("%d.%d", major, minor)
		if major > 3 || (major == 3 && minor >= 12) {
			return okCheck(checkGroupControllerTools, name, path+" "+ver)
		}
		return failCheck(checkGroupControllerTools, name, path+" is "+ver, "Ansible runtime bootstrap requires Python 3.12 or newer", "bootwright bastion setup")
	}
	return failCheck(checkGroupControllerTools, name, "not found", "Bootwright cannot run the managed Ansible runtime", "bootwright bastion setup")
}

// Deps carries the host probes preflight checks use, so tests can inject
// fakes instead of touching the real host.
type Deps struct {
	LookPath         func(name string, extraDirs []string) (string, error)
	StatPath         func(path string) (os.FileInfo, error)
	StatExternalPath func(path string) (os.FileInfo, error)
	CommandOutput    func(name string, args ...string) ([]byte, error)
	// CommandOutputLocalRoot runs a command as the local-root process itself,
	// without de-escalating to the invoking caller. Probes that read root-owned
	// managed-workspace artifacts (a host cluster kubeconfig, the managed Ansible
	// venv interpreter) must use it: under a sudo self-escalated apply, the plain
	// CommandOutput de-escalates to the caller (callerio), who cannot traverse the
	// 0700 root-owned workspace, so a caller-run kubectl/python fails with EACCES
	// on a file that is in fact present and valid. This mirrors the apply path,
	// which runs oc/kubectl against the same kubeconfig as root.
	CommandOutputLocalRoot func(name string, args ...string) ([]byte, error)
	UID                    func() int
	// HTTPDo issues API-endpoint probes (vCenter session auth). A nil
	// value makes each check build a real client honoring the endpoint's
	// certificate-verification setting.
	HTTPDo func(req *http.Request, insecureSkipVerify bool) (*http.Response, error)
}

var DefaultDeps = Deps{
	LookPath:         DefaultLookPath,
	StatPath:         secret.Stat,
	StatExternalPath: secret.StatExternalFile,
	CommandOutput: func(name string, args ...string) ([]byte, error) {
		if out, ok, err := callerio.CommandOutput(name, args...); ok {
			return out, err
		}
		return exec.Command(name, args...).CombinedOutput()
	},
	CommandOutputLocalRoot: func(name string, args ...string) ([]byte, error) {
		return exec.Command(name, args...).CombinedOutput()
	},
	UID: os.Getuid,
}

func (d Deps) statSecretPath(path string, externalSource bool) (os.FileInfo, error) {
	if externalSource && d.StatExternalPath != nil {
		return d.StatExternalPath(path)
	}
	return d.StatPath(path)
}

// CollectChecks gathers the readiness checks for the selected phases.
// hostTrustScope, when non-nil, restricts the SSH host-trust check to that set
// of Machine names — the apply path passes the machines its planned tasks will
// connect to. A nil scope checks every managed-trust machine in the state.
// secretScope, when non-nil, restricts secret-material and storage-tool checks
// to the run's genuine work targets, ignoring render-reference pull-ins.
func CollectChecks(state v1alpha1.State, selected []Phase, hasState bool, contextName, secretsDir string, clustersDir string, deps Deps, hostTrustScope map[string]bool, secretScope *SecretScope) []Check {
	var checks []Check
	addonsNeedAnsible := phaseInScope("add-ons", selected, hasState) && stateNeedsStorageExternalDetailsSSH(state)
	if selectedNeedsAnsible(selected) || addonsNeedAnsible {
		checks = append(checks,
			binaryCheck(checkGroupControllerTools, "ansible-playbook", []string{filepath.Join(workspace.AnsibleVenvDir(), "bin")}, "bootwright bastion setup", deps),
			binaryCheck(checkGroupControllerTools, "python3", nil, "bootwright bastion setup", deps),
		)
	}
	if phaseInScope("machines", selected, hasState) && stateNeedsKubeVirt(state) {
		checks = append(checks, binaryCheck(checkGroupInstallerTools, "kubectl", nil, "install kubectl on PATH", deps))
	}
	if hasState {
		checks = append(checks, installerMediaChecks(state, selected, deps, secretScope)...)
		checks = append(checks, installSourceReachabilityChecks(state, selected, deps, secretScope)...)
	}
	if anyPhaseInScope([]string{"machines", "base"}, selected) && hasState && stateNeedsVSphere(state) {
		checks = append(checks, vspherePyvmomiCheck(deps))
	}
	if phaseInScope("base", selected, hasState) {
		// virtctl is provisioned, version-matched to each host cluster, by the
		// deps stage (the controller_virtctl role). Require it up front only when
		// base runs without deps (nothing will provision it); when deps is in
		// scope — including a full apply with no --stage filter — the provision
		// task installs it before boot, so a missing virtctl here is not a gate.
		if stateNeedsKubeVirt(state) && !phaseInScope("deps", selected, hasState) {
			checks = append(checks, binaryCheck(checkGroupInstallerTools, "virtctl", nil, "install virtctl on PATH, or include the deps stage so bootwright provisions it from the host cluster", deps))
		}
	}
	if phaseInScope("add-ons", selected, hasState) && len(state.ClusterAddonBindings) > 0 {
		checks = append(checks, binaryCheck(checkGroupInstallerTools, "oc", nil, "install oc on PATH", deps))
	}
	if (phaseInScope("deps", selected, hasState) || phaseInScope("base", selected, hasState)) && stateHasManagedStorageClustersInScope(state, secretScope) {
		checks = append(checks,
			binaryCheck(checkGroupControllerTools, "ssh", nil, "install ssh on PATH", deps),
			binaryCheck(checkGroupControllerTools, "scp", nil, "install scp on PATH", deps),
		)
	}
	if hasState {
		checks = append(checks, secretRefChecksScoped(state, secretsDir, selected, deps, secretScope)...)
		checks = append(checks, hostTrustChecks(state, secretsDir, selected, deps, hostTrustScope)...)
		checks = append(checks, generatedSelfSignedDriftChecks(state, secretsDir)...)
		checks = append(checks, kubeVirtHostClusterChecks(state, selected, clustersDir, deps)...)
		checks = append(checks, vsphereVCenterChecks(state, selected, contextName, secretsDir, deps)...)
	}
	return checks
}

func selectedNeedsAnsible(selected []Phase) bool {
	if len(selected) == 0 {
		return true
	}
	for _, phase := range selected {
		if phase.Name != "add-ons" {
			return true
		}
	}
	return false
}

func stateNeedsStorageExternalDetailsSSH(state v1alpha1.State) bool {
	for _, export := range state.StorageExports {
		if export.Spec.ExternalDetails != nil && export.Spec.ExternalDetails.SSHExecution != nil {
			return true
		}
	}
	return false
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
	providers := map[string]v1alpha1.InfraProvider{}
	for _, provider := range state.InfraProviders {
		providers[provider.Metadata.Name] = provider
	}
	for _, machine := range state.Machines {
		provider, ok := providers[machine.Spec.Substrate.ProviderRef.Name]
		if ok && provider.Spec.Type == v1alpha1.ProvisionerKubeVirt {
			return true
		}
	}
	return false
}

func binaryCheck(group, name string, extraDirs []string, remediation string, deps Deps) Check {
	path, err := deps.LookPath(name, extraDirs)
	if err != nil {
		if remediation == "" {
			remediation = "install " + name + " on PATH"
		}
		return failCheck(group, name, "not found", "Required command is unavailable to this workflow", remediation)
	}
	return okCheck(group, name, path)
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

func secretsDirCheck(secretsDir string, deps Deps) Check {
	name := "secrets directory"
	info, err := deps.StatPath(secretsDir)
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

func secretFileCheck(refName, path, label string, publicKey, contextBacked, externalSource bool, deps Deps) Check {
	name := label
	info, err := deps.statSecretPath(path, externalSource)
	if err != nil {
		remediation := "create " + path + " or update Environment.spec.secrets[" + refName + "].file"
		switch {
		case strings.Contains(label, "pullSecretRef"):
			remediation = "bootwright secret set --name " + refName + " --pull-secret <path>"
		case strings.Contains(label, "tls.crt") || strings.Contains(label, "tls.key"):
			remediation = "bootwright secret set --name " + refName + " --tls-cert <cert-chain.pem> --tls-key <key.pem>"
		case strings.Contains(label, "credentialsRef") || strings.Contains(label, "proxyAuthRef"):
			remediation = "bootwright secret set --name " + refName + " --from-file <path>"
		case (strings.Contains(label, " keyRef") || strings.Contains(label, "nodeSSH")) && !contextBacked:
			remediation = "create the file declared by Environment.spec.secrets[" + refName + "].file"
		case contextBacked:
			remediation = "bootwright secret generate or create " + path + " with bootwright secret set"
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

func generatedSecretCheck(refName, path, label, generatedKind string, deps Deps) Check {
	name := label
	info, err := deps.StatPath(path)
	if err != nil {
		remediation := "bootwright secret generate"
		if generatedKind == "credentials" {
			remediation = "bootwright secret generate or bootwright secret set --name " + refName + " --from-file <path>"
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

func okCheck(group, name, evidence string) Check {
	return Check{
		Group:    group,
		Name:     name,
		Status:   StatusOK,
		Evidence: evidence,
	}
}

func failCheck(group, name, evidence, impact, remediation string) Check {
	return Check{
		Group:       group,
		Name:        name,
		Status:      StatusFail,
		Evidence:    evidence,
		Impact:      impact,
		Remediation: remediation,
	}
}
