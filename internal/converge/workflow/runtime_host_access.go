package workflow

import (
	"errors"
	"sort"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/roles"
	secret "github.com/crmarques/bootwright/internal/secrets"
)

func kubeVirtHostClustersForRun(opts RunOptions) []string {
	if opts.Playbook == applyHostVirtctlPlaybook {
		if host := extraVarValue(opts.ExtraVarPairs, "bootwright_task_host_cluster_name"); host != "" {
			return []string{host}
		}
		return nil
	}
	if !playbookUsesKubeVirtHostKubeconfigs(opts.Playbook) {
		return nil
	}
	machines := make(map[string]v1alpha1.Machine, len(opts.State.Machines))
	for _, machine := range opts.State.Machines {
		machines[machine.Metadata.Name] = machine
	}
	providerNames := map[string]bool{}
	for _, cluster := range opts.State.ContainerClusters {
		for _, node := range cluster.Spec.Nodes {
			if machine, ok := machines[node.MachineRef.Name]; ok {
				providerNames[machine.Spec.Substrate.ProviderRef.Name] = true
			}
		}
	}
	for _, cluster := range opts.State.StorageClusters {
		if cluster.Spec.Ceph == nil {
			continue
		}
		for _, node := range cluster.Spec.Ceph.Topology.Nodes {
			machine, ok := machines[node.MachineRef.Name]
			if ok && v1alpha1.MachineInstallsOS(machine) {
				providerNames[machine.Spec.Substrate.ProviderRef.Name] = true
			}
		}
	}
	seen := map[string]bool{}
	var clusters []string
	for _, provider := range opts.State.InfraProviders {
		kubeVirt := provider.Spec.KubeVirt
		if !providerNames[provider.Metadata.Name] || provider.Spec.Type != v1alpha1.ProvisionerKubeVirt || kubeVirt == nil || kubeVirt.HostClusterRef == nil {
			continue
		}
		name := kubeVirt.HostClusterRef.Name
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		clusters = append(clusters, name)
	}
	sort.Strings(clusters)
	return clusters
}

func playbookUsesKubeVirtHostKubeconfigs(playbook string) bool {
	switch playbook {
	case applyClusterInstallPlaybook,
		applyManagedMachineOSPlaybook,
		applyBootMachinePlaybook,
		roles.PlaybookTaskMachineInfraDestroy,
		roles.PlaybookWorkflowInfraApply,
		roles.PlaybookWorkflowInfraDestroy,
		roles.PlaybookWorkflowClustersApply,
		roles.PlaybookWorkflowContainerClusterApply,
		roles.PlaybookWorkflowAllApply:
		return true
	default:
		return false
	}
}

func extraVarValue(pairs []string, name string) string {
	prefix := name + "="
	for i := len(pairs) - 1; i >= 0; i-- {
		if strings.HasPrefix(pairs[i], prefix) {
			return strings.TrimPrefix(pairs[i], prefix)
		}
	}
	return ""
}

func withMaterializedKubeVirtHostKubeconfigs(contextName, clustersDir, hostKubeconfigDir string, clusters []string, fn func(map[string]string) error) error {
	if fn == nil {
		return errors.New("KubeVirt host kubeconfig callback is nil")
	}
	seen := map[string]bool{}
	unique := make([]string, 0, len(clusters))
	for _, name := range clusters {
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		unique = append(unique, name)
	}
	clusters = unique
	sort.Strings(clusters)
	paths := make(map[string]string, len(clusters))
	var materialize func(int) error
	materialize = func(index int) error {
		if index == len(clusters) {
			return fn(paths)
		}
		cluster := clusters[index]
		store := secret.NewContextStore(contextName, ClusterSecretsDir(clustersDir, cluster))
		return store.WithMaterialized(secret.MaterialKey{Name: "kubeconfig", Role: secret.MaterialPrimary}, hostKubeconfigDir, func(path string) error {
			paths[cluster] = path
			defer delete(paths, cluster)
			return materialize(index + 1)
		})
	}
	return materialize(0)
}
