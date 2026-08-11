package workflow

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/converge/remedy"
	"github.com/crmarques/bootwright/internal/roles"
	secret "github.com/crmarques/bootwright/internal/secrets"
)

func kubeVirtHostClustersForRun(opts RunOptions) []string {
	if opts.UseKubeVirtHostClusterScope {
		return sortedUniqueStrings(opts.KubeVirtHostClusters)
	}
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

func destroyTaskKubeVirtHostClusters(task ApplyTask) []string {
	if task.Playbook != roles.PlaybookTaskMachineInfraDestroy {
		return nil
	}
	if extraVarValue(task.ExtraVarPairs, MachineInfraRecordsOnlyExtraVar) == "true" {
		return nil
	}
	machines := map[string]bool{}
	for _, name := range DestroyTaskMachineKeys(task.Entry) {
		machines[name] = true
	}
	for _, cluster := range DestroyTaskClusterKeys(task.Entry) {
		for _, name := range ClusterSubstrateMachineNames(task.State, cluster) {
			machines[name] = true
		}
	}
	for _, name := range strings.Split(extraVarValue(task.ExtraVarPairs, DestroyMachineScopeExtraVar), ",") {
		if name = strings.TrimSpace(name); name != "" {
			machines[name] = true
		}
	}
	if len(machines) == 0 {
		fallback := RunOptions{State: task.State, Playbook: task.Playbook}
		return kubeVirtHostClustersForRun(fallback)
	}
	providerNames := map[string]bool{}
	matchedMachine := false
	for _, machine := range task.State.Machines {
		if machines[machine.Metadata.Name] {
			matchedMachine = true
			providerNames[machine.Spec.Substrate.ProviderRef.Name] = true
		}
	}
	if !matchedMachine {
		fallback := RunOptions{State: task.State, Playbook: task.Playbook}
		return kubeVirtHostClustersForRun(fallback)
	}
	seen := map[string]bool{}
	var clusters []string
	for _, provider := range task.State.InfraProviders {
		kubeVirt := provider.Spec.KubeVirt
		if !providerNames[provider.Metadata.Name] || provider.Spec.Type != v1alpha1.ProvisionerKubeVirt || kubeVirt == nil || kubeVirt.HostClusterRef == nil {
			continue
		}
		name := strings.TrimSpace(kubeVirt.HostClusterRef.Name)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		clusters = append(clusters, name)
	}
	sort.Strings(clusters)
	return clusters
}

func sortedUniqueStrings(values []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func playbookToleratesMissingKubeVirtHostKubeconfig(playbook string) bool {
	switch playbook {
	case roles.PlaybookTaskMachineInfraDestroy, roles.PlaybookWorkflowInfraDestroy:
		return true
	default:
		return false
	}
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

type KubeVirtHostClusterAccessError struct {
	Cluster string
	Path    string
}

func (e *KubeVirtHostClusterAccessError) Error() string {
	return fmt.Sprintf("ContainerCluster/%s hosts KubeVirt machines in this run but holds no captured kubeconfig at %s; install or reconcile that host cluster before retrying", e.Cluster, e.Path)
}

func (e *KubeVirtHostClusterAccessError) Remedy() remedy.Request {
	return remedy.Request{
		Action: remedy.ActionReconcileContainerClusterThenRetrySameSelection,
		Targets: []remedy.Target{{
			Role: remedy.TargetRoleContainerCluster,
			Name: e.Cluster,
		}},
	}
}

func withMaterializedKubeVirtHostKubeconfigs(contextName, clustersDir, hostKubeconfigDir string, clusters []string, tolerateMissing bool, fn func(map[string]string) error) error {
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
		status, err := store.Inspect(secret.MaterialKey{Name: "kubeconfig", Role: secret.MaterialPrimary})
		if err != nil {
			return err
		}
		if status.State == secret.MaterialStateMissing {
			if tolerateMissing {
				return materialize(index + 1)
			}
			return &KubeVirtHostClusterAccessError{Cluster: cluster, Path: status.Path}
		}
		return store.WithMaterialized(secret.MaterialKey{Name: "kubeconfig", Role: secret.MaterialPrimary}, hostKubeconfigDir, func(path string) error {
			paths[cluster] = path
			defer delete(paths, cluster)
			return materialize(index + 1)
		})
	}
	return materialize(0)
}
