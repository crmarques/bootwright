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
	"github.com/crmarques/bootwright/internal/ownership"
	"github.com/crmarques/bootwright/internal/state/graph"
	"github.com/crmarques/bootwright/internal/state/view"
)

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
		if destroySweepReclaims(o.Kind) {
			p.Status(output.StatusWarn, label, "not in desired state; "+destroyReclaimsOrphanHint)
			continue
		}
		p.Status(output.StatusWarn, label, "not in desired state; "+destroyLeavesOrphanHint)
	}
}

const (
	destroyReclaimsOrphanHint = "a full `bootwright destroy` reclaims it"
	destroyLeavesOrphanHint   = "a full `bootwright destroy` does not reclaim this record — destroy it while it is still declared, or clean it up manually"
)

func destroyConfirmPrompt(storagePlanned bool) string {
	if storagePlanned {
		return "Confirm this DESTRUCTIVE action (accept data loss)? [y/N] (default: no): "
	}
	return "Continue with destroy? [y/N] (default: no): "
}

func destroySweepReclaims(kind string) bool {
	switch kind {
	case string(ownership.KindLibvirtDomain), string(ownership.KindLibvirtNetwork), string(ownership.KindManagedOSInstall):
		return true
	default:
		return false
	}
}

func printSkippedOwnershipRecords(w io.Writer, warnings []error) {
	if len(warnings) == 0 {
		return
	}
	p := output.NewContinuation(w)
	p.Section("Skipped ownership records")
	for _, warning := range warnings {
		p.Status(output.StatusWarn, "skipped", warning.Error())
	}
}

func printInfraComponentDestroyBlocks(w io.Writer, decision converge.InfraComponentDestroyDecision, override bool) {
	if len(decision.Blocks) == 0 && len(decision.Warnings) == 0 {
		return
	}
	p := output.NewContinuation(w)
	if len(decision.Blocks) > 0 {
		p.Section("Shared components owned or referenced by other contexts")
		for _, block := range decision.Blocks {
			label := block.ComponentKind + " " + block.Name
			if block.Host != "" {
				label += " on " + block.Host
			}
			relation := "co-owned or referenced by "
			if block.Unrecorded {
				relation = "owned (no ownership record in this context) by "
			}
			detail := relation + strings.Join(block.Contexts, ", ")
			if override {
				detail += "; WILL BE TORN DOWN because --force was supplied"
			} else {
				detail += "; refused unless --force"
			}
			p.Status(output.StatusWarn, label, detail)
		}
	}
	for _, warning := range decision.Warnings {
		p.Status(output.StatusWarn, "ownership scan", warning.Error()+"; treated as still in use")
	}
}

func printDestroyPreview(w io.Writer, scope converge.Scope, clustersDir string, state v1alpha1.State, storageWorkNames []string) {
	switch {
	case converge.DestroyIsFullScope(scope):
		printDestroyClustersPreview(w, clustersDir, state, storageWorkNames)
		printDestroyInfraPreview(w, state, storageWorkNames)
	case scope.Name == "clusters" || scope.Name == "container-cluster":
		printDestroyClustersPreview(w, clustersDir, state, storageWorkNames)
	case scope.Name == "infra":
		printDestroyInfraPreview(w, state, storageWorkNames)
	}
}

func destroyStoragePreviewNames(state v1alpha1.State, storageWorkNames []string) []string {
	if storageWorkNames != nil {
		out := append([]string(nil), storageWorkNames...)
		sort.Strings(out)
		return out
	}
	out := make([]string, 0, len(state.StorageClusters))
	for _, cluster := range state.StorageClusters {
		out = append(out, cluster.Metadata.Name)
	}
	sort.Strings(out)
	return out
}

func printDestroyClustersPreview(w io.Writer, clustersDir string, state v1alpha1.State, storageWorkNames []string) {
	storageNames := destroyStoragePreviewNames(state, storageWorkNames)
	if len(state.ContainerClusters) == 0 && len(storageNames) == 0 {
		return
	}
	p := output.NewContinuation(w)
	p.Section("Will destroy")
	var items []output.Item
	for _, name := range storageNames {
		items = append(items, output.Item{Label: "storage cluster " + name, Detail: "cephadm rm-cluster --zap-osds: ALL OSD DATA destroyed; declared devices wiped (wipefs + sgdisk --zap-all) — irreversible"})
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

func printDestroyInfraPreview(w io.Writer, state v1alpha1.State, storageWorkNames []string) {
	storageNames := destroyStoragePreviewNames(state, storageWorkNames)
	if len(state.Machines) == 0 && len(state.ContainerClusters) == 0 && len(storageNames) == 0 {
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
	for _, name := range storageNames {
		items = append(items, output.Item{Label: "storage cluster " + name + " infra", Detail: "provider-owned machines and declared managed disks wiped — ALL OSD DATA on this storage cluster is destroyed"})
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
		for _, ref := range clusterArtifactServerEndpointRefs(state, ci, ocp) {
			server, ok := artifacts.Select(state, ref)
			if !ok || server.Config == nil {
				continue
			}
			usage := out[server.Component.Metadata.Name]
			usage.machineRef = server.Config.MachineRef.Name
			usage.clusters = appendUniqueString(usage.clusters, ocp.Metadata.Name)
			out[server.Component.Metadata.Name] = usage
		}
	}
	for name, usage := range out {
		sort.Strings(usage.clusters)
		out[name] = usage
	}
	return out
}

func clusterArtifactServerEndpointRefs(state v1alpha1.State, ci v1alpha1.ClusterInstall, ocp v1alpha1.ContainerCluster) []v1alpha1.ArtifactServerEndpointRef {
	var refs []v1alpha1.ArtifactServerEndpointRef
	if artifacts.ClusterUsesBareMetalMachine(state, ci) {
		refs = append(refs, ci.Agent.RedfishVirtualMedia.ArtifactServerEndpoint)
	}
	if v1alpha1.InstallMode(ocp) == v1alpha1.InstallModeDisconnected {
		refs = append(refs, ci.Agent.BootArtifacts.ArtifactServerEndpoint)
	}
	return refs
}

func appendUniqueString(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
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
		if clusterUsesManagedArtifactServerEndpoint(state, ci, ocp) {
			parts = append(parts, "artifacts")
		}
	}
	return strings.Join(parts, ", ")
}

func clusterUsesManagedArtifactServerEndpoint(state v1alpha1.State, ci v1alpha1.ClusterInstall, ocp v1alpha1.ContainerCluster) bool {
	for _, ref := range clusterArtifactServerEndpointRefs(state, ci, ocp) {
		server, ok := artifacts.Select(state, ref)
		if ok && server.Config != nil {
			return true
		}
	}
	return false
}
