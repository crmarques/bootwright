package converge

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	extensionplan "github.com/crmarques/bootwright/internal/addons/plan"
	extensionrecords "github.com/crmarques/bootwright/internal/addons/records"
	"github.com/crmarques/bootwright/internal/converge/workflow"
	"github.com/crmarques/bootwright/internal/ownership"
)

type KubeVirtTenantConflict struct {
	Host    string
	Tenants []string
}

func KubeVirtTenantsByHost(state v1alpha1.State) map[string][]string {
	out := map[string][]string{}
	for child, parents := range kubeVirtHostParentsByChild(state) {
		for _, parent := range parents {
			out[parent] = appendUniqueClusterName(out[parent], child)
		}
	}
	return out
}

func ProvisionedStorageTenants(ownershipRecords []ownership.ResourceRecord) map[string]bool {
	out := map[string]bool{}
	for _, name := range OwnedStorageClusterRecordNames(ownershipRecords) {
		out[name] = true
	}
	return out
}

func installedKubeVirtTenants(clustersDir string, tenants []string, provisionedStorage map[string]bool) []string {
	var out []string
	for _, tenant := range tenants {
		if provisionedStorage[tenant] {
			out = append(out, tenant)
			continue
		}
		record, found, err := workflow.LoadClusterInstallRecord(clustersDir, tenant)
		if err != nil || !found || record.Status == workflow.ClusterInstallStatusDestroyed {
			continue
		}
		out = append(out, tenant)
	}
	return out
}

func KubeVirtTenantDestroyDescriptors(state v1alpha1.State, clustersDir string, hosts []string, provisionedStorage map[string]bool) []string {
	tenantsByHost := KubeVirtTenantsByHost(state)
	var out []string
	seen := map[string]bool{}
	for _, host := range hosts {
		if seen[host] {
			continue
		}
		seen[host] = true
		installed := installedKubeVirtTenants(clustersDir, tenantsByHost[host], provisionedStorage)
		if len(installed) == 0 {
			continue
		}
		sort.Strings(installed)
		out = append(out, fmt.Sprintf("also destroys nested cluster(s) %s hosted on %s via KubeVirt", strings.Join(installed, ", "), host))
	}
	return out
}

func KubeVirtTenantCollateral(state v1alpha1.State, clustersDir string, destroyedHosts, selected []string, provisionedStorage map[string]bool) []KubeVirtTenantConflict {
	inScope := map[string]bool{}
	for _, name := range selected {
		inScope[name] = true
	}
	tenantsByHost := KubeVirtTenantsByHost(state)
	var out []KubeVirtTenantConflict
	seen := map[string]bool{}
	for _, host := range destroyedHosts {
		if seen[host] {
			continue
		}
		seen[host] = true
		var candidates []string
		for _, tenant := range tenantsByHost[host] {
			if inScope[tenant] {
				continue
			}
			candidates = append(candidates, tenant)
		}
		blocked := installedKubeVirtTenants(clustersDir, candidates, provisionedStorage)
		if len(blocked) == 0 {
			continue
		}
		sort.Strings(blocked)
		out = append(out, KubeVirtTenantConflict{Host: host, Tenants: blocked})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Host < out[j].Host })
	return out
}

func KubeVirtTenantDestroyConflicts(state v1alpha1.State, clustersDir string, selectedRoots []string, provisionedStorage map[string]bool) []KubeVirtTenantConflict {
	return KubeVirtTenantCollateral(state, clustersDir, selectedRoots, selectedRoots, provisionedStorage)
}

func FormatKubeVirtTenantConflicts(conflicts []KubeVirtTenantConflict, retryCommand string) error {
	var b strings.Builder
	b.WriteString("refusing to destroy KubeVirt host cluster(s) still hosting nested cluster(s) left running:\n")
	for _, c := range conflicts {
		b.WriteString(fmt.Sprintf("  - ContainerCluster %s hosts %s\n", c.Host, strings.Join(c.Tenants, ", ")))
	}
	b.WriteString("include the nested cluster(s) in the same selection, or destroy them first")
	if retryCommand != "" {
		b.WriteString(" with `" + retryCommand + "`")
	}
	b.WriteString("; no --authorize token widens the selected work set")
	return fmt.Errorf("%s", b.String())
}

func KubeVirtConflictTenants(conflicts []KubeVirtTenantConflict) []string {
	seen := map[string]bool{}
	var out []string
	for _, c := range conflicts {
		for _, tenant := range c.Tenants {
			if seen[tenant] {
				continue
			}
			seen[tenant] = true
			out = append(out, tenant)
		}
	}
	sort.Strings(out)
	return out
}

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
	return workflow.KubeVirtHostParentNames(state)
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
