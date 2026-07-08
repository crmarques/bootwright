package preflight

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	secret "github.com/crmarques/bootwright/internal/secrets"
)

func kubeVirtHostClusterChecks(state v1alpha1.State, selected []Phase, clustersDir, secretsDir string, deps Deps) []Check {
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
			usable["host:"+name] = path
		}
	}
	// Providers that point at an external hub via kubeconfigRef (rather than a
	// greenfield hostClusterRef this run installs) have their kubeconfig on disk
	// before the run, so the live API/CRD probe is runnable at preflight. A
	// missing or malformed file is already reported by the secret-material
	// checks, so skip the probe when the file is absent to avoid a duplicate
	// FAIL; when it is present, gate the KubeVirt API the same as the host arm.
	idx := secret.NewIndex(state)
	for _, p := range state.InfraProviders {
		k := p.Spec.KubeVirt
		if k == nil || k.KubeconfigRef == nil || k.KubeconfigRef.Name == "" {
			continue
		}
		if k.HostClusterRef != nil && k.HostClusterRef.Name != "" {
			continue // handled by the hostClusterRef arm above
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
	// Per provider (networkAttachments are provider-scoped, even when several
	// providers share one host cluster), verify the referenced network resolves
	// on the host cluster whose kubeconfig is usable.
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
