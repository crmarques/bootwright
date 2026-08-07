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

func destroyConfirmPrompt(dataLossUnauthorized bool) string {
	if dataLossUnauthorized {
		return "Confirm this DESTRUCTIVE action (accept data loss)? [y/N] (default: no): "
	}
	return "Continue with destroy? [y/N] (default: no): "
}

func destroyDataLossYesGuard(dataLoss workflow.DestroyDataLoss, yes, allowDestroy bool, selection runSelection) error {
	if allowDestroy || !yes || !dataLoss.Planned() {
		return nil
	}
	return fmt.Errorf("destroy would destroy data: %s. --yes does not authorize data loss: re-run `%s` to proceed non-interactively, or drop --yes to confirm interactively; if the list names a cluster you did not intend to destroy, re-run with %s to narrow the work set", dataLoss.Consequence(), selection.command("destroy", "--authorize "+authorizeDataLoss, "--yes"), selection.narrowFlag())
}

func mergeSkippedInputDocuments(groups ...[]error) []error {
	seen := map[string]bool{}
	var out []error
	for _, group := range groups {
		for _, err := range group {
			if err == nil || seen[err.Error()] {
				continue
			}
			seen[err.Error()] = true
			out = append(out, err)
		}
	}
	return out
}

func staleInputRefusal(contextName string, skipped []error) error {
	details := make([]string, 0, len(skipped))
	for _, err := range skipped {
		details = append(details, err.Error())
	}
	return fmt.Errorf("%d input document(s) of context %q no longer decode or validate against this build, so destroy cannot plan a teardown from them: %s; re-render the input for the current schema and run `bootwright context update --name %s -f <dir>`, run destroy from the build that applied this context, or re-run `bootwright destroy --authorize %s` to plan the teardown without those document(s) — whatever they declared is then left standing and reported", len(skipped), contextName, strings.Join(details, "; "), contextName, authorizeStaleInput)
}

func printSkippedInputDocuments(w io.Writer, skipped []error, authorized bool) {
	if len(skipped) == 0 {
		return
	}
	p := output.NewContinuation(w)
	p.Section("Skipped input documents")
	for _, err := range skipped {
		p.Status(output.StatusWarn, "input", err.Error())
	}
	if authorized {
		p.Warning("stale input", "these document(s) were skipped because --authorize "+authorizeStaleInput+" was supplied: whatever they declared is NOT in the teardown work set and is left standing. Check the owned-but-no-longer-declared list below before treating this context as gone")
	}
}

func destroySweepReclaims(kind string) bool {
	switch kind {
	case string(ownership.KindLibvirtDomain), string(ownership.KindLibvirtNetwork), string(ownership.KindManagedOSInstall):
		return true
	case string(ownership.KindBMCEmulator), string(ownership.KindInfraComponent):
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

func printInfraComponentDestroyBlocks(w io.Writer, decision converge.InfraComponentDestroyDecision, authorized bool) {
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
			if authorized {
				detail += "; WILL BE TORN DOWN because --authorize " + authorizeSharedInfra + " was supplied"
			} else {
				detail += "; refused unless --authorize " + authorizeSharedInfra
			}
			p.Status(output.StatusWarn, label, detail)
		}
	}
	for _, warning := range decision.Warnings {
		p.Status(output.StatusWarn, "ownership scan", warning.Error()+"; treated as still in use")
	}
}

func printDestroyPreview(w io.Writer, scope converge.Scope, clustersDir string, state v1alpha1.State, storageWorkNames, selectedMachines []string) {
	full := converge.DestroyIsFullScope(scope)
	runtimeLayer := full || scope.Name == "clusters" || scope.Name == "container-cluster"
	machineLayer := full || scope.Name == "infra"
	if !runtimeLayer && !machineLayer {
		return
	}
	if len(selectedMachines) > 0 {
		printDestroyMachinePreview(w, state, selectedMachines)
		return
	}
	storageNames := destroyStoragePreviewNames(state, storageWorkNames)
	containerNames := make([]string, 0, len(state.ContainerClusters))
	for _, cluster := range state.ContainerClusters {
		containerNames = append(containerNames, cluster.Metadata.Name)
	}
	sort.Strings(containerNames)
	if len(storageNames) == 0 && len(containerNames) == 0 {
		return
	}
	items := make([]output.Item, 0, len(storageNames)+len(containerNames))
	for _, name := range storageNames {
		providerOwnedOSDs := false
		if cluster, ok := storageClusterByName(state, name); ok {
			providerOwnedOSDs = len(workflow.StorageClusterProviderOwnedOSDMachines(state, cluster)) > 0
		}
		items = append(items, output.Item{Label: name + " (StorageCluster)", Detail: destroyStorageClusterDetail(runtimeLayer, machineLayer, providerOwnedOSDs)})
	}
	for _, name := range containerNames {
		items = append(items, output.Item{Label: name + " (ContainerCluster)", Detail: destroyContainerClusterDetail(state, clustersDir, name, runtimeLayer, machineLayer)})
	}
	p := output.NewContinuation(w)
	p.Section("Will destroy")
	p.List(items)
	if runtimeLayer && !machineLayer {
		p.Status(output.StatusInfo, "retained", "machine substrate is kept; remove it with destroy --stage infra")
	}
}

func printDestroyMachinePreview(w io.Writer, state v1alpha1.State, machines []string) {
	names := append([]string(nil), machines...)
	sort.Strings(names)
	items := make([]output.Item, 0, len(names))
	for _, name := range names {
		items = append(items, output.Item{Label: name + " (Machine)", Detail: destroyMachineDetail(state, name)})
	}
	p := output.NewContinuation(w)
	p.Section("Will destroy")
	p.List(items)
	p.Status(output.StatusInfo, "retained", "only the named machines' substrate is torn down; every cluster and every unnamed machine of this context is left standing")
}

func destroyMachineDetail(state v1alpha1.State, name string) string {
	for _, machine := range state.Machines {
		if machine.Metadata.Name != name {
			continue
		}
		if !v1alpha1.MachineRequiresSubstrate(machine) {
			return "bare-metal machine: its OS and disks are retained; Bootwright-local install state is released so the next apply may reinstall it"
		}
		return "provider-owned machine: the VM and its disks are deleted — irreversible for any data they hold"
	}
	return "machine substrate is torn down"
}

func destroyStorageClusterDetail(runtimeLayer, machineLayer, providerOwnedOSDs bool) string {
	var parts []string
	if runtimeLayer {
		parts = append(parts, "cephadm rm-cluster --zap-osds: ALL OSD DATA destroyed; declared devices wiped (wipefs + sgdisk --zap-all) — irreversible")
	}
	switch {
	case machineLayer && providerOwnedOSDs:
		parts = append(parts, "provider-owned machines deleted with their disks — ALL OSD DATA on this storage cluster is destroyed")
	case machineLayer:
		parts = append(parts, "Bootwright-local install state released; the OSD hosts are hardware Bootwright retains, so their disks are not wiped here")
	}
	return strings.Join(parts, "; ")
}

func destroyContainerClusterDetail(state v1alpha1.State, clustersDir, name string, runtimeLayer, machineLayer bool) string {
	var parts []string
	if runtimeLayer {
		parts = append(parts, "runtime dir "+filepath.Join(clustersDir, name, "runtime")+" and generated add-on records")
	}
	if machineLayer {
		if cluster, ok := containerClusterByName(state, name); ok {
			ci, _ := stateview.ClusterInstallForContainerCluster(state, cluster)
			detail := fmt.Sprintf("substrate: %d machine(s)", len(ci.Machines))
			if services := destroyManagedServices(state, ci); services != "" {
				detail += "; managed " + services
			}
			parts = append(parts, detail)
		}
	}
	return strings.Join(parts, "; ")
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

func storageClusterByName(state v1alpha1.State, name string) (v1alpha1.StorageCluster, bool) {
	for _, cluster := range state.StorageClusters {
		if cluster.Metadata.Name == name {
			return cluster, true
		}
	}
	return v1alpha1.StorageCluster{}, false
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
