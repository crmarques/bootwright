package cli

import (
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/cli/output"
	"github.com/crmarques/bootwright/internal/converge"
	"github.com/crmarques/bootwright/internal/converge/workflow"
	"github.com/crmarques/bootwright/internal/infra/artifacts"
	"github.com/crmarques/bootwright/internal/state/graph"
	"github.com/crmarques/bootwright/internal/state/view"
)

// printDestroyOrphans lists Bootwright-owned resources still recorded in the ownership
// store but no longer declared in desired state. A full `bootwright destroy` reclaims
// them via the ownership-record sweep; this preview surfaces them so the operator knows
// they exist. Read-only.
func printDestroyOrphans(w io.Writer, orphans []workflow.UndeclaredResource) {
	if len(orphans) == 0 {
		return
	}
	p := output.NewContinuation(w)
	p.Section("Owned but no longer declared")
	for _, o := range orphans {
		label := o.Kind + "/" + o.Name
		if o.Cluster != "" {
			label += " (cluster " + o.Cluster + ")"
		}
		p.Status(output.StatusWarn, label, "not in desired state; a full `bootwright destroy` reclaims it")
	}
}

// printDestroyPreview lists the user-visible resources `destroy` will
// remove for the current scope, before the confirmation prompt. The
// preview is concise on purpose: the user can read the YAML for full
// detail. The output differs by scope because the two destroy stages remove
// very different things: cluster destroy removes cluster-stage runtime and
// managed storage state; infra destroy tears down VMs, networks, provider
// services, infra component services, and provider-owned machine disks.
func printDestroyPreview(w io.Writer, scope converge.Scope, clustersDir string, state v1alpha1.State) {
	switch scope.Name {
	case "clusters", "container-cluster":
		printDestroyClustersPreview(w, clustersDir, state)
	case "infra":
		printDestroyInfraPreview(w, state)
	}
}

func printDestroyClustersPreview(w io.Writer, clustersDir string, state v1alpha1.State) {
	if len(state.ContainerClusters) == 0 && len(state.StorageClusters) == 0 {
		return
	}
	p := output.NewContinuation(w)
	p.Section("Will destroy")
	var items []output.Item
	storageNames := make([]string, 0, len(state.StorageClusters))
	for _, cluster := range state.StorageClusters {
		storageNames = append(storageNames, cluster.Metadata.Name)
	}
	sort.Strings(storageNames)
	for _, name := range storageNames {
		items = append(items, output.Item{Label: "storage cluster " + name, Detail: "managed services and generated attachment records"})
	}

	names := make([]string, 0, len(state.ContainerClusters))
	for _, c := range state.ContainerClusters {
		names = append(names, c.Metadata.Name)
	}
	sort.Strings(names)
	for _, name := range names {
		items = append(items, output.Item{Label: "cluster " + name, Detail: "runtime dir " + filepath.Join(clustersDir, name, "runtime") + " and generated add-on records"})
	}
	p.List(items)
	p.Status(output.StatusInfo, "destroy clusters", "machine substrate cleanup is handled by destroy --stage infra")
}

func printDestroyInfraPreview(w io.Writer, state v1alpha1.State) {
	if len(state.Machines) == 0 && len(state.ContainerClusters) == 0 && len(state.StorageClusters) == 0 {
		return
	}
	p := output.NewContinuation(w)
	p.Section("Will destroy")

	clusters := make([]string, 0, len(state.ContainerClusters))
	for _, c := range state.ContainerClusters {
		clusters = append(clusters, c.Metadata.Name)
	}
	sort.Strings(clusters)
	var items []output.Item
	for _, name := range clusters {
		items = append(items, output.Item{Label: "cluster " + name, Detail: "substrate"})
	}

	for _, name := range clusters {
		cluster, ok := containerClusterByName(state, name)
		if !ok {
			continue
		}
		ci, _ := stateview.ClusterInstallForContainerCluster(state, cluster)
		machines := len(ci.Machines)
		services := destroyManagedServices(state, ci)
		detail := fmt.Sprintf("%d machine(s)", machines)
		if services != "" {
			detail += "; managed " + services
		}
		items = append(items, output.Item{Label: "cluster " + name + " infra", Detail: detail})
	}
	storageNames := make([]string, 0, len(state.StorageClusters))
	for _, cluster := range state.StorageClusters {
		storageNames = append(storageNames, cluster.Metadata.Name)
	}
	sort.Strings(storageNames)
	for _, name := range storageNames {
		items = append(items, output.Item{Label: "storage cluster " + name + " infra", Detail: "provider-owned machines and declared managed disks"})
	}
	p.List(items)
}

func printDestroyArtifactServerPreview(w io.Writer, state v1alpha1.State) {
	consumers := artifactServerClusterConsumers(state)
	if len(consumers) == 0 {
		return
	}
	p := output.NewContinuation(w)
	p.Section("Will destroy")
	var names []string
	for name := range consumers {
		names = append(names, name)
	}
	sort.Strings(names)
	items := make([]output.Item, 0, len(names))
	for _, name := range names {
		usage := consumers[name]
		items = append(items, output.Item{
			Label:  "artifact-server " + name,
			Detail: "machine " + usage.machineRef + "; BMC ISO fetches for " + strings.Join(usage.clusters, ", "),
		})
	}
	p.List(items)
}

type artifactServerDestroyUsage struct {
	machineRef string
	clusters   []string
}

func artifactServerClusterConsumers(state v1alpha1.State) map[string]artifactServerDestroyUsage {
	out := map[string]artifactServerDestroyUsage{}
	for _, ocp := range state.ContainerClusters {
		ci, ok := stateview.ClusterInstallForContainerCluster(state, ocp)
		if !ok || !artifacts.ClusterNeedsPublication(state, ci, ocp) {
			continue
		}
		server, ok := artifacts.Select(state, ci)
		if !ok || server.Config == nil {
			continue
		}
		usage := out[server.Component.Metadata.Name]
		usage.machineRef = server.Config.MachineRef.Name
		usage.clusters = append(usage.clusters, ocp.Metadata.Name)
		out[server.Component.Metadata.Name] = usage
	}
	for name, usage := range out {
		sort.Strings(usage.clusters)
		out[name] = usage
	}
	return out
}

func containerClusterByName(state v1alpha1.State, name string) (v1alpha1.ContainerCluster, bool) {
	for _, cluster := range state.ContainerClusters {
		if cluster.Metadata.Name == name {
			return cluster, true
		}
	}
	return v1alpha1.ContainerCluster{}, false
}

func destroyManagedServices(state v1alpha1.State, ci v1alpha1.ClusterInstall) string {
	var parts []string
	if clusterUsesLoadBalancerComponent(ci) {
		parts = append(parts, "loadBalancers")
	}
	if clusterUsesManagedNameResolution(state, ci) {
		parts = append(parts, "nameResolution")
	}
	if environmentUsesManagedNTP(state) {
		parts = append(parts, "ntp")
	}
	if environmentUsesManagedProxy(state) {
		parts = append(parts, "proxy")
	}
	if clusterUsesManagedRegistry(state, ci) {
		parts = append(parts, "registry")
	}
	if ocp, ok := stategraph.SelectedClusterForInstall(state.ContainerClusters, ci.Metadata.Name); ok && artifacts.ClusterNeedsPublication(state, ci, ocp) {
		if server, ok := artifacts.Select(state, ci); ok && server.Config != nil {
			parts = append(parts, "artifacts")
		}
	}
	return strings.Join(parts, ", ")
}
