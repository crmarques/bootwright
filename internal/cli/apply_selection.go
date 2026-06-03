package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	extensionplan "github.com/crmarques/bootwright/internal/addons/plan"
	extensionrecords "github.com/crmarques/bootwright/internal/addons/records"
	"github.com/crmarques/bootwright/internal/converge/workflow"
)

func validateKubeVirtClusterSelection(state v1alpha1.State, scope scopeSpec, clusters string, clustersDir string) error {
	if strings.TrimSpace(clusters) == "" {
		return nil
	}
	if scope.name != "infra" && scope.name != "clusters" && scope.name != "all" {
		return nil
	}
	containerNames, _, err := clusterRootNamesForTarget(state, clusters)
	if err != nil {
		return err
	}
	selected := map[string]bool{}
	for _, name := range containerNames {
		selected[name] = true
	}
	parentsByChild := kubeVirtHostParentsByChild(state)
	for _, child := range containerNames {
		for _, parent := range parentsByChild[child] {
			if selected[parent] {
				continue
			}
			ready, err := kubeVirtParentReady(state, clustersDir, parent)
			if err != nil {
				return err
			}
			if !ready {
				return fmt.Errorf("ContainerCluster/%s uses KubeVirt hostContainerClusterRef %q but the host cluster is not installed and KubeVirt-ready; include %s in --clusters or apply it first", child, parent, parent)
			}
		}
	}
	return nil
}

func kubeVirtHostParentsByChild(state v1alpha1.State) map[string][]string {
	providers := map[string]v1alpha1.InfraProvider{}
	for _, provider := range state.InfraProviders {
		providers[provider.Metadata.Name] = provider
	}
	infras := map[string]v1alpha1.ClusterInfra{}
	for _, infra := range state.ClusterInfras {
		infras[infra.Metadata.Name] = infra
	}
	out := map[string][]string{}
	for _, cluster := range state.ContainerClusters {
		if len(cluster.Spec.Nodes) == 0 {
			continue
		}
		infra, ok := infras[cluster.Spec.Nodes[0].MachineRef.ClusterInfra]
		if !ok {
			continue
		}
		for _, machine := range infra.Spec.Components.Machines {
			profile, ok := clusterMachineProfile(providers, machine)
			if !ok || profile.KubeVirt == nil || profile.KubeVirt.HostContainerClusterRef == nil {
				continue
			}
			parent := profile.KubeVirt.HostContainerClusterRef.Name
			if parent == "" {
				continue
			}
			out[cluster.Metadata.Name] = appendUniqueClusterName(out[cluster.Metadata.Name], parent)
		}
	}
	return out
}

func clusterMachineProfile(providers map[string]v1alpha1.InfraProvider, machine v1alpha1.ClusterMachineComponent) (v1alpha1.MachineProfileCapability, bool) {
	provider, ok := providers[machine.From.Provider]
	if !ok || machine.From.Profile == "" {
		return v1alpha1.MachineProfileCapability{}, false
	}
	for _, profile := range provider.Spec.MachineProfiles {
		if profile.Name == machine.From.Profile {
			return profile, true
		}
	}
	return v1alpha1.MachineProfileCapability{}, false
}

func kubeVirtParentReady(state v1alpha1.State, clustersDir string, parent string) (bool, error) {
	record, found, err := workflow.LoadClusterInstallRecord(clustersDir, parent)
	if err != nil {
		return false, err
	}
	if !found || record.Status != workflow.ClusterInstallStatusInstalled || record.Phase != workflow.ClusterInstallPhaseComplete {
		return false, nil
	}
	kubeconfigPath := filepath.Join(clustersDir, parent, "secrets", "kubeconfig")
	info, err := os.Stat(kubeconfigPath)
	if err != nil || info.IsDir() {
		return false, nil
	}
	plans, err := extensionplan.BindingPlans(state)
	if err != nil {
		return false, err
	}
	for _, plan := range plans {
		if plan.Cluster != parent {
			continue
		}
		for _, extension := range plan.Addons {
			if !clusterAddonProvides(extension.Extension, v1alpha1.ClusterAddonProvidesKubeVirt) {
				continue
			}
			record, found, err := extensionrecords.LoadRecord(clustersDir, parent, extension.Name)
			if err != nil {
				return false, err
			}
			if found && record.Status == extensionrecords.RecordStatusReady {
				return true, nil
			}
		}
	}
	return false, nil
}

func clusterAddonProvides(addon v1alpha1.ClusterAddon, capability string) bool {
	for _, item := range addon.Spec.Provides {
		if item == capability {
			return true
		}
	}
	return false
}

func appendUniqueClusterName(items []string, value string) []string {
	for _, item := range items {
		if item == value {
			return items
		}
	}
	return append(items, value)
}
