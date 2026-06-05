package cli

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/cli/output"
	"github.com/crmarques/bootwright/internal/infra/artifacts"
	"github.com/crmarques/bootwright/internal/state/graph"
	"github.com/crmarques/bootwright/internal/state/view"
)

var currentEUID = os.Geteuid

func printApplySummary(w io.Writer, selected []Phase, askBecomePass bool, dryRun bool, noRemoteWork bool) {
	printWorkflowSummary(w, "Apply plan", selected, askBecomePass, dryRun, noRemoteWork)
}

func printDestroySummary(w io.Writer, selected []Phase, askBecomePass bool, dryRun bool, noRemoteWork bool) {
	printWorkflowSummary(w, "Destroy plan", selected, askBecomePass, dryRun, noRemoteWork)
}

func printWorkflowSummary(w io.Writer, title string, selected []Phase, askBecomePass bool, dryRun bool, noRemoteWork bool) {
	p := output.NewContinuation(w)
	p.Section(title)
	var items []output.PlanItem
	rootPhases := 0
	for _, p := range selected {
		if p.NeedsRoot {
			rootPhases++
		}
		items = append(items, output.PlanItem{Name: p.Name, Description: p.Description, Root: p.NeedsRoot})
	}
	p.Plan(items)
	if noRemoteWork {
		return
	}
	if rootPhases > 0 {
		switch {
		case dryRun:
			p.Warning("Root phases", "sudo escalation is required; this is a dry run, no commands execute")
		case askBecomePass:
			p.Warning("Root phases", becomePasswordSummary("workflow"))
		case currentEUID() == 0:
			p.Status(output.StatusInfo, "Root phases", "bootwright is running as root, no BECOME password prompt needed")
		default:
			p.Warning("Root phases", "--ask-become-pass=false requires passwordless sudo or an already-root connection user")
		}
	}
}

func printWorkflowStart(w io.Writer, workflowName string, selected []Phase, askBecomePass bool) {
	if len(selected) == 1 {
		printPhaseStart(w, selected[0], askBecomePass)
		return
	}
	p := output.NewContinuation(w)
	p.Section("Run")
	if rootPhaseCount(selected) > 0 {
		p.List([]output.Item{{Label: workflowName + " [root]", Detail: "phases: " + phaseList(selected)}})
		if askBecomePass {
			p.Warning("Sudo", becomePasswordSummary("workflow"))
		}
		return
	}
	p.List([]output.Item{{Label: workflowName, Detail: "phases: " + phaseList(selected)}})
}

func printPhaseStart(w io.Writer, phase Phase, askBecomePass bool) {
	p := output.NewContinuation(w)
	p.Section("Run")
	if phase.NeedsRoot && askBecomePass {
		p.List([]output.Item{{Label: phase.Name + " [root]", Detail: phase.Description}})
		p.Warning("Sudo", becomePasswordSummary("phase"))
		return
	}
	p.List([]output.Item{{Label: phase.Name, Detail: phase.Description}})
}

// printWorkflowEnd writes a single-line completion banner after a successful
// workflow run. Failure details are reported by the caller that owns the
// command's output mode.
func printWorkflowEnd(w io.Writer, workflowName string) {
	output.NewContinuation(w).Summary(output.StatusOK, workflowName, "complete")
}

func rootPhaseCount(selected []Phase) int {
	count := 0
	for _, p := range selected {
		if p.NeedsRoot {
			count++
		}
	}
	return count
}

func useControllingTTYForWorkflow(selected []Phase, askBecomePass bool) bool {
	return !askBecomePass && rootPhaseCount(selected) > 0
}

// selectedTargetsClusters reports whether the selected phases include the
// `container-cluster` phase. Used to gate ResolveInstaller: the install_agent role
// consumes secret-inlined installer inputs under the per-cluster runtime work
// dir, so apply paths that drive that role must inline secrets before handing
// off to Ansible.
func selectedTargetsClusters(selected []Phase) bool {
	for _, p := range selected {
		if p.Name == "container-cluster" {
			return true
		}
	}
	return false
}

func phaseList(selected []Phase) string {
	names := make([]string, 0, len(selected))
	for _, p := range selected {
		names = append(names, p.Name)
	}
	return strings.Join(names, ", ")
}

// printDestroyPreview lists the user-visible resources `destroy` will
// remove for the current scope, before the confirmation prompt. The
// preview is concise on purpose: the user can read the YAML for full
// detail. The output differs by scope because the two destroy stages remove
// very different things: cluster destroy removes only the root-managed
// per-cluster runtime dir on the controller; infra destroy tears down VMs,
// networks, provider services, and infra component services.
func printDestroyPreview(w io.Writer, scope scopeSpec, clustersDir string, state v1alpha1.State) {
	switch scope.name {
	case "container-cluster":
		printDestroyClustersPreview(w, clustersDir, state)
	case "infra":
		printDestroyInfraPreview(w, state)
	}
}

func printDestroyClustersPreview(w io.Writer, clustersDir string, state v1alpha1.State) {
	if len(state.ContainerClusters) == 0 {
		return
	}
	p := output.NewContinuation(w)
	p.Section("Will destroy")
	names := make([]string, 0, len(state.ContainerClusters))
	for _, c := range state.ContainerClusters {
		names = append(names, c.Metadata.Name)
	}
	sort.Strings(names)
	var items []output.Item
	for _, name := range names {
		items = append(items, output.Item{Label: "cluster " + name, Detail: "runtime dir " + filepath.Join(clustersDir, name, "runtime")})
	}
	p.List(items)
	p.Warning("destroy clusters", "does not power off VMs, undefine networks, or remove machine services; run destroy --stage infra for that")
}

func printDestroyInfraPreview(w io.Writer, state v1alpha1.State) {
	if len(state.Machines) == 0 && len(state.ContainerClusters) == 0 {
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

func clusterUsesLoadBalancerComponent(ci v1alpha1.ClusterInstall) bool {
	for _, endpoint := range ci.Endpoints {
		if endpoint.Source.Type == v1alpha1.EndpointSourceInfraComponent && endpoint.Source.ComponentRef.Name != "" {
			return true
		}
	}
	return false
}

func primaryEnvironment(state v1alpha1.State) *v1alpha1.Environment {
	if len(state.Environments) == 0 {
		return nil
	}
	return &state.Environments[0]
}

func clusterUsesManagedNameResolution(state v1alpha1.State, ci v1alpha1.ClusterInstall) bool {
	env := primaryEnvironment(state)
	if env == nil {
		return false
	}
	managed := map[string]bool{}
	for _, entry := range env.Spec.InfraComponents.NameResolution {
		if entry.Type == v1alpha1.EnvironmentComponentManaged {
			managed[entry.Name] = true
		}
	}
	for _, network := range state.NetworkConfigs {
		if !clusterConsumesNetwork(ci, network.Metadata.Name) {
			continue
		}
		for _, ref := range network.Spec.DNSRefs {
			if managed[ref] {
				return true
			}
		}
	}
	return false
}

func clusterUsesManagedRegistry(state v1alpha1.State, ci v1alpha1.ClusterInstall) bool {
	ocp, ok := stategraph.SelectedClusterForInstall(state.ContainerClusters, ci.Metadata.Name)
	if !ok || v1alpha1.InstallMode(ocp) != v1alpha1.InstallModeDisconnected {
		return false
	}
	env := primaryEnvironment(state)
	if env == nil {
		return false
	}
	for _, entry := range env.Spec.InfraComponents.Registries {
		if entry.Type == v1alpha1.EnvironmentComponentManaged && entry.Default {
			return true
		}
	}
	return len(env.Spec.InfraComponents.Registries) == 1 && env.Spec.InfraComponents.Registries[0].Type == v1alpha1.EnvironmentComponentManaged
}

func environmentUsesManagedProxy(state v1alpha1.State) bool {
	env := primaryEnvironment(state)
	if env == nil {
		return false
	}
	selected := map[string]bool{
		env.Spec.ProxyFor.Bootwright:              true,
		env.Spec.ProxyFor.ContainerClusterInstall: true,
	}
	for _, entry := range env.Spec.InfraComponents.Proxies {
		if selected[entry.Name] && entry.Type == v1alpha1.EnvironmentComponentManaged {
			return true
		}
	}
	return false
}

func environmentUsesManagedNTP(state v1alpha1.State) bool {
	env := primaryEnvironment(state)
	if env == nil {
		return false
	}
	for _, entry := range env.Spec.InfraComponents.NTPSources {
		if entry.Type == v1alpha1.EnvironmentComponentManaged {
			return true
		}
	}
	return false
}

func clusterConsumesNetwork(ci v1alpha1.ClusterInstall, networkName string) bool {
	for _, machine := range ci.Machines {
		if machine.Network.NetworkConfigRef.Name == networkName {
			return true
		}
	}
	return false
}

func confirm(in io.Reader, prompt io.Writer, message string) bool {
	if in == nil {
		return false
	}
	fmt.Fprint(prompt, message)
	line, err := bufio.NewReader(in).ReadString('\n')
	if err != nil && line == "" {
		return false
	}
	answer := strings.TrimSpace(strings.ToLower(line))
	return answer == "y" || answer == "yes"
}
