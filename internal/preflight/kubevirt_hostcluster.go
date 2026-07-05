package preflight

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func kubeVirtHostClusterChecks(state v1alpha1.State, selected []Phase, clustersDir string, deps Deps) []Check {
	if !anyPhaseInScope([]string{"machines", "base"}, selected) {
		return nil
	}
	// A KubeVirt host cluster that is itself a ContainerCluster this run installs
	// has its kubeconfig produced during the run: the apply scheduler orders the
	// host cluster's install ahead of the dependent child clusters' node boot via
	// the clusterInstalled capability (see workflow.kubeVirtHostClusterReadiness),
	// so a missing kubeconfig is expected, not a gate. Mirror that scheduler
	// signal — the host is a ContainerCluster present in the scoped plan state,
	// with the kubeconfig-producing base phase in scope — and report INFO instead
	// of FAIL. A host that is external/pre-existing (not a ContainerCluster in the
	// plan state) or a run that does not install it (base out of scope) stays
	// gated: its kubeconfig must already be on disk.
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
			usable[name] = path
		}
	}
	// Per provider (networkAttachments are provider-scoped, even when several
	// providers share one host cluster), verify the referenced network resolves
	// on the host cluster whose kubeconfig is usable.
	for _, p := range state.InfraProviders {
		if p.Spec.KubeVirt == nil || p.Spec.KubeVirt.HostClusterRef == nil {
			continue
		}
		path, ok := usable[p.Spec.KubeVirt.HostClusterRef.Name]
		if !ok {
			continue
		}
		checks = append(checks, kubeVirtNetworkRefChecks(p, path, deps)...)
	}
	return checks
}

func kubeVirtAPIReadyCheck(name, kubeconfigPath string, deps Deps) Check {
	// Read the kubeconfig directly before shelling out. The probe runs as local
	// root (like CommandOutputLocalRoot), so an os.Open failure is an unambiguous
	// unreadable/missing kubeconfig we can classify here — keeping a genuine
	// EACCES out of the "KubeVirt not ready → re-apply" bucket even when a given
	// kubectl build words the error in a way kubeconfigUnreadable does not match.
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

// kubeconfigUnreadable reports whether a kubectl failure is the host cluster
// kubeconfig being unloadable (unreadable or malformed) rather than the cluster
// being unreachable or KubeVirt not installed. The two failure classes want
// different remediations, so an EACCES is never reported as "KubeVirt not ready"
// or "create the network".
func kubeconfigUnreadable(evidence string) bool {
	return strings.Contains(evidence, "error loading config file") ||
		strings.Contains(evidence, "permission denied")
}
