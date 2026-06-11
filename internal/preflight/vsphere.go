package preflight

import (
	"path/filepath"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/workspace"
)

func stateNeedsVSphere(state v1alpha1.State) bool {
	providers := map[string]v1alpha1.InfraProvider{}
	for _, provider := range state.InfraProviders {
		providers[provider.Metadata.Name] = provider
	}
	for _, machine := range state.Machines {
		provider, ok := providers[machine.Spec.Substrate.ProviderRef.Name]
		if ok && provider.Spec.Type == v1alpha1.ProvisionerVSphere {
			return true
		}
	}
	return false
}

// vspherePyvmomiCheck verifies the managed Ansible venv python can import
// pyVmomi — the community.vmware modules that create vSphere machines run
// on the controller through that interpreter.
func vspherePyvmomiCheck(deps Deps) Check {
	name := "pyvmomi (vCenter SDK)"
	venvPython := filepath.Join(workspace.AnsibleVenvDir(), "bin", "python")
	out, err := deps.CommandOutput(venvPython, "-c", "import pyVmomi")
	if err != nil {
		evidence := strings.TrimSpace(string(out))
		if evidence == "" {
			evidence = err.Error()
		}
		return failCheck(checkGroupInstallerTools, name, evidence, "vSphere machine creation drives vCenter through pyvmomi-backed Ansible modules", "bootwright bastion setup")
	}
	return okCheck(checkGroupInstallerTools, name, venvPython+" imports pyVmomi")
}
