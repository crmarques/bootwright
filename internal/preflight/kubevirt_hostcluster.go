package preflight

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	secret "github.com/crmarques/bootwright/internal/secrets"
)

func kubeVirtHostClusterChecks(state v1alpha1.State, selected []Phase, clustersDir, secretsDir string, deps Deps, secretScope *SecretScope) []Check {
	if !anyPhaseInScope([]string{"machines", "base"}, selected) {
		return nil
	}
	provisionedThisRun := map[string]bool{}
	if phaseInScope("base", selected, true) {
		for _, cluster := range state.ContainerClusters {
			provisionedThisRun[cluster.Metadata.Name] = true
		}
	}
	seen := map[string]bool{}
	usable := map[string]string{}
	var checks []Check
	for _, p := range state.InfraProviders {
		if p.Spec.KubeVirt == nil || p.Spec.KubeVirt.HostClusterRef == nil || p.Spec.KubeVirt.HostClusterRef.Name == "" {
			continue
		}
		if !kubeVirtProviderInScope(p.Metadata.Name, state, secretScope) {
			continue
		}
		name := p.Spec.KubeVirt.HostClusterRef.Name
		if seen[name] {
			continue
		}
		seen[name] = true
		path := filepath.Join(clustersDir, name, "secrets", "kubeconfig")
		info, err := deps.StatPath(path)
		switch {
		case err != nil && provisionedThisRun[name]:
			checks = append(checks, infoCheck(checkGroupInstallerTools, name+" kubeconfig", "will be provisioned this run: "+name+" installs in the base phase before its dependent KubeVirt child clusters boot"))
		case err != nil:
			checks = append(checks, failCheck(checkGroupInstallerTools, name+" kubeconfig", path+" missing", "KubeVirt child clusters need the host cluster kubeconfig", "include "+name+" in --clusters or run bootwright apply --stage clusters --clusters "+name+" --yes first"))
		case info.IsDir():
			checks = append(checks, failCheck(checkGroupInstallerTools, name+" kubeconfig", path+" is a directory", "KubeVirt child clusters need the host cluster kubeconfig file", "replace "+path+" with the host cluster kubeconfig"))
		default:
			checks = append(checks, okCheck(checkGroupInstallerTools, name+" kubeconfig", path))
			checks = append(checks, kubeVirtAPIReadyCheck(name, path, deps))
			usable["host:"+name] = path
		}
	}
	idx := secret.NewIndex(state)
	for _, p := range state.InfraProviders {
		k := p.Spec.KubeVirt
		if k == nil || k.KubeconfigRef == nil || k.KubeconfigRef.Name == "" {
			continue
		}
		if k.HostClusterRef != nil && k.HostClusterRef.Name != "" {
			continue
		}
		if !kubeVirtProviderInScope(p.Metadata.Name, state, secretScope) {
			continue
		}
		refName := k.KubeconfigRef.Name
		if seen["kc:"+refName] {
			continue
		}
		seen["kc:"+refName] = true
		path := secret.ResolveMaterialPath(refName, idx, secretsDir, secret.MaterialPrimary)
		externalSource := secret.MaterialPathUsesExternalSource(refName, idx, secret.MaterialPrimary)
		info, err := deps.statSecretPath(path, externalSource)
		if err != nil || info.IsDir() {
			continue
		}
		checks = append(checks, kubeVirtAPIReadyCheck(refName, path, deps))
		usable["kc:"+refName] = path
	}
	for _, p := range state.InfraProviders {
		k := p.Spec.KubeVirt
		if k == nil {
			continue
		}
		var key string
		switch {
		case k.HostClusterRef != nil && k.HostClusterRef.Name != "":
			key = "host:" + k.HostClusterRef.Name
		case k.KubeconfigRef != nil && k.KubeconfigRef.Name != "":
			key = "kc:" + k.KubeconfigRef.Name
		default:
			continue
		}
		path, ok := usable[key]
		if !ok {
			continue
		}
		checks = append(checks, kubeVirtNetworkRefChecks(p, path, deps)...)
	}
	return checks
}

func kubeVirtProviderInScope(providerName string, state v1alpha1.State, secretScope *SecretScope) bool {
	if secretScope == nil {
		return true
	}
	for _, machine := range state.Machines {
		if machine.Spec.Substrate.ProviderRef.Name == providerName && secretScope.allowsMachine(machine.Metadata.Name) {
			return true
		}
	}
	return false
}

func kubeVirtAPIReadyCheck(name, kubeconfigPath string, deps Deps) Check {
	f, ferr := os.Open(kubeconfigPath)
	if ferr != nil {
		return failCheck(checkGroupInstallerTools, name+" KubeVirt API", ferr.Error(), "KubeVirt child clusters need a readable host cluster kubeconfig", "ensure "+kubeconfigPath+" is a readable, valid kubeconfig (bootwright manages it under the root-owned workspace)")
	}
	_ = f.Close()
	out, err := deps.CommandOutputLocalRoot("kubectl", "--kubeconfig", kubeconfigPath, "--request-timeout=5s", "get", "crd", "virtualmachines.kubevirt.io", "-o", "name")
	if err != nil {
		evidence := strings.TrimSpace(string(out))
		if evidence == "" {
			evidence = err.Error()
		}
		if kubeconfigUnreadable(evidence) {
			return failCheck(checkGroupInstallerTools, name+" KubeVirt API", evidence, "KubeVirt child clusters need a readable host cluster kubeconfig", "ensure "+kubeconfigPath+" is a readable, valid kubeconfig (bootwright manages it under the root-owned workspace)")
		}
		return failCheck(checkGroupInstallerTools, name+" KubeVirt API", evidence, "KubeVirt child clusters need OpenShift Virtualization ready on the host cluster", "run bootwright apply --stage clusters --clusters "+name+" --yes first")
	}
	if !strings.Contains(string(out), "virtualmachines.kubevirt.io") {
		return failCheck(checkGroupInstallerTools, name+" KubeVirt API", strings.TrimSpace(string(out)), "KubeVirt child clusters need OpenShift Virtualization ready on the host cluster", "run bootwright apply --stage clusters --clusters "+name+" --yes first")
	}
	return okCheck(checkGroupInstallerTools, name+" KubeVirt API", "virtualmachines.kubevirt.io")
}

func kubeconfigUnreadable(evidence string) bool {
	return strings.Contains(evidence, "error loading config file") ||
		strings.Contains(evidence, "permission denied")
}
