package cli

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/cli/output"
	"github.com/crmarques/bootwright/internal/converge"
	"github.com/crmarques/bootwright/internal/state/graph"
)

var currentEUID = os.Geteuid

func printApplySummary(w io.Writer, selected []converge.Phase, askBecomePass bool, dryRun bool, noRemoteWork bool) {
	printWorkflowSummary(w, "Apply plan", selected, askBecomePass, dryRun, noRemoteWork)
}

func printDestroySummary(w io.Writer, selected []converge.Phase, askBecomePass bool, dryRun bool, noRemoteWork bool) {
	printWorkflowSummary(w, "Destroy plan", selected, askBecomePass, dryRun, noRemoteWork)
}

func printWorkflowSummary(w io.Writer, title string, selected []converge.Phase, askBecomePass bool, dryRun bool, noRemoteWork bool) {
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

// printWorkflowEnd writes a single-line completion banner after a successful
// workflow run. Failure details are reported by the caller that owns the
// command's output mode.
func printWorkflowEnd(w io.Writer, workflowName string) {
	output.NewContinuation(w).Summary(output.StatusOK, workflowName, "complete")
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
		if entry.Management == v1alpha1.EnvironmentComponentManaged {
			managed[entry.Name] = true
		}
	}
	for _, network := range state.NetworkConfigs {
		if !clusterConsumesNetwork(ci, network.Metadata.Name) {
			continue
		}
		for _, ref := range network.Spec.NameResolutionRefs {
			if managed[ref.Name] {
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
		if entry.Management == v1alpha1.EnvironmentComponentManaged && entry.Default {
			return true
		}
	}
	return len(env.Spec.InfraComponents.Registries) == 1 && env.Spec.InfraComponents.Registries[0].Management == v1alpha1.EnvironmentComponentManaged
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
		if selected[entry.Name] && entry.Management == v1alpha1.EnvironmentComponentManaged {
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
	for _, entry := range env.Spec.InfraComponents.NTP {
		if entry.Management == v1alpha1.EnvironmentComponentManaged {
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
