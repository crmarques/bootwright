package converge

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/crmarques/bootwright/api/v1alpha1"
	extensionplan "github.com/crmarques/bootwright/internal/addons/plan"
	extensionrecords "github.com/crmarques/bootwright/internal/addons/records"
	"github.com/crmarques/bootwright/internal/converge/workflow"
	"github.com/crmarques/bootwright/internal/state/view"
)

// ValidateKubeVirtClusterSelection checks that every selected ContainerCluster
// whose machines live on a KubeVirt provider either co-selects its host
// cluster or finds it already installed and KubeVirt-ready. The CLI resolves
// the --clusters selection to containerNames (via internal/clusteraccess)
// before calling in.
func ValidateKubeVirtClusterSelection(state v1alpha1.State, containerNames []string, clustersDir string) error {
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
				return fmt.Errorf("ContainerCluster/%s uses KubeVirt hostClusterRef %q but the host cluster is not installed and KubeVirt-ready; include %s in --clusters or apply it first", child, parent, parent)
			}
		}
	}
	return nil
}

func kubeVirtHostParentsByChild(state v1alpha1.State) map[string][]string {
	out := map[string][]string{}
	for _, cluster := range state.ContainerClusters {
		for _, node := range cluster.Spec.Hosts {
			machine, ok := stateview.Machine(state, node.MachineRef.Name)
			if !ok {
				continue
			}
			provider, ok := stateview.Provider(state, machine.Spec.Substrate.ProviderRef.Name)
			if !ok || provider.Spec.Type != v1alpha1.ProvisionerKubeVirt || provider.Spec.KubeVirt == nil || provider.Spec.KubeVirt.HostClusterRef == nil {
				continue
			}
			parent := provider.Spec.KubeVirt.HostClusterRef.Name
			if parent == "" {
				continue
			}
			out[cluster.Metadata.Name] = appendUniqueClusterName(out[cluster.Metadata.Name], parent)
		}
	}
	return out
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
