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
	"github.com/crmarques/bootwright/internal/artifactpub"
	"github.com/crmarques/bootwright/internal/cli/output"
	"github.com/crmarques/bootwright/internal/stategraph"
)

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
			p.Warning("Root phases", "Bootwright will ask once for the BECOME password and reuse it for this workflow")
		case os.Geteuid() == 0:
			p.Warning("Root phases", "bootwright is running as root, no BECOME password prompt needed")
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
			p.Warning("Sudo", "Bootwright will ask once for the BECOME password for this workflow")
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
		p.Warning("Sudo", "Bootwright will ask once for the BECOME password for this phase")
		return
	}
	p.List([]output.Item{{Label: phase.Name, Detail: phase.Description}})
}

// printWorkflowEnd writes a single-line completion banner after a
// successful Ansible playbook run. Failures already surface via the
// streamed Ansible output and the non-zero exit; this banner gives the
// operator a clean "done" marker between a long stream of Ansible
// output and the rendered-artifact summary that follows.
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
// `clusters` phase. Used to gate ResolveInstaller: the install_agent role
// consumes the secret-inlined install-config.yaml/agent-config.yaml under
// the per-cluster runtime work dir, so apply paths that drive that role
// must inline secrets before handing off to Ansible.
func selectedTargetsClusters(selected []Phase) bool {
	for _, p := range selected {
		if p.Name == "clusters" {
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

var askBecomePassDefault = func() bool { return os.Geteuid() != 0 }

// printDestroyPreview lists the user-visible resources `destroy` will
// remove for the current scope, before the confirmation prompt. The
// preview is concise on purpose: the user can read the YAML for full
// detail. The output differs by scope because the two destroy targets
// remove very different things: `destroy cluster` removes only the
// per-cluster installer work dir on the controller; `destroy infra`
// tears down VMs, networks, and provider services on provider hosts.
func printDestroyPreview(w io.Writer, scope scopeSpec, stateDir string, state v1alpha1.State) {
	switch scope.name {
	case "cluster":
		printDestroyClustersPreview(w, stateDir, state)
	case "infra":
		printDestroyInfraPreview(w, state)
	}
}

func printDestroyClustersPreview(w io.Writer, stateDir string, state v1alpha1.State) {
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
	runtimeRoot := filepath.Join(stateDir, "runtime")
	var items []output.Item
	for _, name := range names {
		items = append(items, output.Item{Label: "cluster " + name, Detail: "installer dir " + filepath.Join(runtimeRoot, name, "installer")})
	}
	p.List(items)
	p.Warning("destroy cluster", "does not power off VMs, undefine networks, or remove provider services; run destroy infra for that")
}

func printDestroyInfraPreview(w io.Writer, state v1alpha1.State) {
	if len(state.ClusterInfras) == 0 && len(state.ContainerClusters) == 0 {
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

	infraNames := make([]string, 0, len(state.ClusterInfras))
	infraByName := map[string]v1alpha1.ClusterInfra{}
	for _, ci := range state.ClusterInfras {
		infraNames = append(infraNames, ci.Metadata.Name)
		infraByName[ci.Metadata.Name] = ci
	}
	sort.Strings(infraNames)
	for _, name := range infraNames {
		ci := infraByName[name]
		machines := len(ci.Spec.Components.Machines)
		services := destroyManagedServices(state, ci)
		detail := fmt.Sprintf("%d machine(s)", machines)
		if services != "" {
			detail += "; managed " + services
		}
		items = append(items, output.Item{Label: "infra " + name, Detail: detail})
	}
	p.List(items)
}

func printDestroyHTTPServerPreview(w io.Writer, state v1alpha1.State) {
	publisher, ok := artifactpub.Select(state)
	if !ok || publisher.Capability.HTTP == nil {
		return
	}
	clusters := artifactPublisherClusterConsumers(state)
	if len(clusters) == 0 {
		return
	}
	p := output.NewContinuation(w)
	p.Section("Will destroy")
	p.List([]output.Item{{
		Label:  "http-server " + publisher.ProviderName + "/" + publisher.Capability.Name,
		Detail: "host " + publisher.Capability.HTTP.HostRef.Name + "; BMC ISO fetches for " + strings.Join(clusters, ", "),
	}})
}

func artifactPublisherClusterConsumers(state v1alpha1.State) []string {
	var clusters []string
	for _, ci := range state.ClusterInfras {
		ocp, ok := stategraph.SelectedClusterForInfra(state.ContainerClusters, ci.Metadata.Name)
		if !ok || !artifactpub.ClusterNeedsPublication(state, ci, ocp) {
			continue
		}
		clusters = append(clusters, ocp.Metadata.Name)
	}
	sort.Strings(clusters)
	return clusters
}

func destroyManagedServices(state v1alpha1.State, ci v1alpha1.ClusterInfra) string {
	var parts []string
	c := ci.Spec.Components
	if len(c.LoadBalancers) > 0 {
		parts = append(parts, "loadBalancers")
	}
	if c.NameResolution != nil {
		parts = append(parts, "nameResolution")
	}
	if c.Proxy != nil {
		parts = append(parts, "proxy")
	}
	if c.Registry != nil {
		parts = append(parts, "registry")
	}
	if ocp, ok := stategraph.SelectedClusterForInfra(state.ContainerClusters, ci.Metadata.Name); ok && artifactpub.ClusterNeedsPublication(state, ci, ocp) {
		parts = append(parts, "artifacts")
	}
	return strings.Join(parts, ", ")
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
