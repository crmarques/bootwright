package preflight

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	secret "github.com/crmarques/bootwright/internal/secrets"
)

func kubeVirtHostClusterChecks(state v1alpha1.State, selected []Phase, contextName, clustersDir, secretsDir, runtimeDir string, deps Deps, secretScope *SecretScope) []Check {
	if !anyPhaseInScope([]string{"machines", "base"}, selected) {
		return nil
	}
	provisionedThisRun := map[string]bool{}
	if phaseInScope("base", selected, true) {
		for _, cluster := range state.ContainerClusters {
			provisionedThisRun[cluster.Metadata.Name] = true
		}
	}
	type probeTarget struct {
		name           string
		path           string
		hostCluster    bool
		externalSource bool
		providers      []v1alpha1.InfraProvider
	}
	idx := secret.NewIndex(state)
	seen := map[string]*probeTarget{}
	var targets []*probeTarget
	for _, p := range state.InfraProviders {
		k := p.Spec.KubeVirt
		if k == nil || !kubeVirtProviderInScope(p.Metadata.Name, state, secretScope) {
			continue
		}
		var key string
		var target probeTarget
		switch {
		case k.HostClusterRef != nil && k.HostClusterRef.Name != "":
			target.name = k.HostClusterRef.Name
			target.path = filepath.Join(clustersDir, target.name, "secrets", "kubeconfig")
			target.hostCluster = true
			key = "host:" + target.name
		case k.KubeconfigRef != nil && k.KubeconfigRef.Name != "":
			target.name = k.KubeconfigRef.Name
			target.path = secret.ResolveMaterialPath(target.name, idx, secretsDir, secret.MaterialPrimary)
			target.externalSource = secret.MaterialPathUsesExternalSource(target.name, idx, secret.MaterialPrimary)
			key = "kubeconfig:" + target.name
		default:
			continue
		}
		current, ok := seen[key]
		if !ok {
			current = &target
			seen[key] = current
			targets = append(targets, current)
		}
		current.providers = append(current.providers, p)
	}
	var checks []Check
	probe := func(target *probeTarget, kubeconfigPath string) []Check {
		var targetChecks []Check
		apiCheck := kubeVirtCheckSourcePath(kubeVirtAPIReadyCheck(target.name, kubeconfigPath, deps), kubeconfigPath, target.path)
		targetChecks = append(targetChecks, apiCheck)
		if apiCheck.Status != StatusOK {
			return targetChecks
		}
		for _, provider := range target.providers {
			networkChecks := kubeVirtNetworkRefChecks(provider, kubeconfigPath, deps)
			for i := range networkChecks {
				networkChecks[i] = kubeVirtCheckSourcePath(networkChecks[i], kubeconfigPath, target.path)
			}
			targetChecks = append(targetChecks, networkChecks...)
		}
		return targetChecks
	}
	for _, target := range targets {
		info, err := deps.statSecretPath(target.path, target.externalSource)
		if target.hostCluster {
			switch {
			case err != nil && provisionedThisRun[target.name]:
				checks = append(checks, infoCheck(checkGroupInstallerTools, target.name+" kubeconfig", "will be provisioned this run: "+target.name+" installs in the base phase before its dependent KubeVirt child clusters boot"))
			case err != nil:
				checks = append(checks, failCheck(checkGroupInstallerTools, target.name+" kubeconfig", target.path+" missing", "KubeVirt child clusters need the host cluster kubeconfig", "include "+target.name+" in --clusters or run bootwright apply --stage clusters --clusters "+target.name+" --yes first"))
			case info.IsDir():
				checks = append(checks, failCheck(checkGroupInstallerTools, target.name+" kubeconfig", target.path+" is a directory", "KubeVirt child clusters need the host cluster kubeconfig file", "replace "+target.path+" with the host cluster kubeconfig"))
			default:
				checks = append(checks, okCheck(checkGroupInstallerTools, target.name+" kubeconfig", target.path))
				store := secret.NewContextStore(contextName, filepath.Dir(target.path))
				var targetChecks []Check
				err = store.WithMaterialized(secret.MaterialKey{Name: "kubeconfig", Role: secret.MaterialPrimary}, runtimeDir, func(kubeconfigPath string) error {
					targetChecks = probe(target, kubeconfigPath)
					return nil
				})
				if err != nil {
					checks = append(checks, kubeVirtMaterializationFailure(target.name, target.path, err))
				} else {
					checks = append(checks, targetChecks...)
				}
			}
			continue
		}
		if err != nil || info.IsDir() {
			continue
		}
		resolver := secret.NewResolver(contextName, secretsDir, idx)
		var targetChecks []Check
		err = resolver.WithMaterialized(target.name, secret.MaterialPrimary, runtimeDir, func(kubeconfigPath string) error {
			targetChecks = probe(target, kubeconfigPath)
			return nil
		})
		if err != nil {
			checks = append(checks, kubeVirtMaterializationFailure(target.name, target.path, err))
		} else {
			checks = append(checks, targetChecks...)
		}
	}
	return checks
}

func kubeVirtMaterializationFailure(name, kubeconfigPath string, err error) Check {
	return failCheck(checkGroupInstallerTools, name+" KubeVirt API", err.Error(), "KubeVirt child clusters need a readable host cluster kubeconfig", "ensure "+kubeconfigPath+" contains readable kubeconfig material and any Bootwright encryption key is available")
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
	out, err := deps.CommandOutputLocalRoot("kubectl", "--kubeconfig", kubeconfigPath, "--request-timeout=5s", "get", "--raw=/apis/subresources.kubevirt.io/v1/version")
	if err != nil {
		evidence := strings.TrimSpace(string(out))
		if evidence == "" {
			evidence = err.Error()
		}
		kubernetesOut, kubernetesErr := deps.CommandOutputLocalRoot("kubectl", "--kubeconfig", kubeconfigPath, "--request-timeout=5s", "get", "--raw=/version")
		if kubernetesErr == nil && strings.Contains(string(kubernetesOut), `"gitVersion"`) {
			return failCheck(checkGroupInstallerTools, name+" KubeVirt API", evidence, "KubeVirt child clusters need OpenShift Virtualization ready on the host cluster", "run bootwright apply --stage clusters --clusters "+name+" --yes first")
		}
		return failCheck(checkGroupInstallerTools, name+" KubeVirt API", evidence, "KubeVirt child clusters need access to the host cluster Kubernetes API", "ensure "+kubeconfigPath+" identifies a reachable Kubernetes API server and grants access (Bootwright manages it under the root-owned workspace)")
	}
	if !strings.Contains(string(out), `"gitVersion"`) {
		return failCheck(checkGroupInstallerTools, name+" KubeVirt API", strings.TrimSpace(string(out)), "KubeVirt child clusters need access to the host cluster Kubernetes API", "ensure "+kubeconfigPath+" identifies a reachable Kubernetes API server and grants access (Bootwright manages it under the root-owned workspace)")
	}
	return okCheck(checkGroupInstallerTools, name+" KubeVirt API", "subresources.kubevirt.io/v1/version")
}

func kubeVirtCheckSourcePath(check Check, kubeconfigPath, sourcePath string) Check {
	if kubeconfigPath != sourcePath {
		check.Evidence = strings.ReplaceAll(check.Evidence, kubeconfigPath, sourcePath)
		check.Remediation = strings.ReplaceAll(check.Remediation, kubeconfigPath, sourcePath)
	}
	return check
}

func kubernetesResourceMissing(evidence, name string) bool {
	return strings.Contains(evidence, "NotFound") && strings.Contains(evidence, name)
}
