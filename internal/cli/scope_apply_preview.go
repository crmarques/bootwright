package cli

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	cliout "github.com/crmarques/bootwright/internal/cli/output"
	"github.com/crmarques/bootwright/internal/clusteraccess"
	"github.com/crmarques/bootwright/internal/converge"
	"github.com/crmarques/bootwright/internal/converge/workflow"
	"github.com/crmarques/bootwright/internal/ownership"
)

func checkKubeVirtTenantRebuildScope(state v1alpha1.State, clustersDir string, sel clusteraccess.Selection, rebuiltHosts []string, provisionedStorage map[string]bool, invocation *resolvedInvocation) error {
	if !sel.Active || len(rebuiltHosts) == 0 {
		return nil
	}
	conflicts := converge.KubeVirtTenantCollateral(state, clustersDir, rebuiltHosts, sel.AllRoots, provisionedStorage)
	if len(conflicts) == 0 {
		return nil
	}
	var b strings.Builder
	b.WriteString("apply --mode rebuild would reinstall KubeVirt host cluster(s) whose nested cluster(s) are left out of scope and would be annihilated:\n")
	for _, c := range conflicts {
		b.WriteString(fmt.Sprintf("  - ContainerCluster %s hosts installed %s\n", c.Host, strings.Join(c.Tenants, ", ")))
	}
	b.WriteString("include the nested cluster(s) in the run's selection (--clusters, or --machines covering their nodes) to rebuild them in the same run")
	if invocation != nil {
		retry, err := invocation.destroyClustersRetry(kubeVirtTenantNames(conflicts))
		if err != nil {
			return err
		}
		b.WriteString(", or destroy them first with `" + retry.String() + "`")
	}
	b.WriteString("; no --authorize token widens the selected work set")
	return errors.New(b.String())
}

func kubeVirtTenantNames(conflicts []converge.KubeVirtTenantConflict) []string {
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
	return out
}

func printArtifactServerReclaimNotice(stdout io.Writer, names []string) {
	if len(names) == 0 {
		return
	}
	cliout.NewContinuation(stdout).Warning("artifact-server retention", "end of run will reclaim install-only artifact server(s) "+strings.Join(names, ", ")+" (all referencing clusters installed) — its service, ports, and published ISOs are torn down and its ownership record removed")
}

func printApplyAvailabilityCaveat(stdout io.Writer, mode workflow.ApplyMode, clustersDir string, tasks []workflow.ApplyTask) {
	if mode != workflow.ApplyModeRebuild {
		return
	}
	if len(workflow.InstalledRecordedClusters(clustersDir, tasks)) == 0 {
		return
	}
	cliout.NewContinuation(stdout).Warning("mode rebuild", "plan mode does not probe cluster availability; a real run additionally reinstalls (disk wipe) any installed cluster that does not report Available=True — gated by --authorize "+authorizeDataLoss)
}

func forecastReinstallDescriptors(names []string) []string {
	var out []string
	for _, name := range names {
		out = append(out, fmt.Sprintf("reinstall ContainerCluster/%s (recorded install inputs drifted — --mode rebuild reinstalls it and wipes its node disks)", name))
	}
	return out
}

func applyGateForecastRefusals(fullState, planState v1alpha1.State, tasks []workflow.ApplyTask, runsDir, clustersDir string, mode workflow.ApplyMode, allowDestroy, unownedAuthorized bool, reclaimDevices string, sel clusteraccess.Selection, reinstallDrift []string, ownershipDir string, ownershipRecords []ownership.ResourceRecord, ownershipSkipped []error, invocation *resolvedInvocation) []string {
	var refusals []error
	if len(ownershipSkipped) > 0 {
		refusals = append(refusals, applyUnreadableOwnershipRefusal(ownershipDir, ownershipSkipped, nil))
	}
	if releases, err := workflow.ConsumableSubstrateReleases(runsDir, tasks); err == nil {
		if refusal := installedContainerClusterMachineReleaseRefusal(clustersDir, releases, invocation); refusal != nil {
			refusals = append(refusals, refusal)
		}
	}
	objects, err := workflow.ClassifyApplyObjects(tasks, runsDir)
	if err != nil {
		return applyGateRefusalMessages(refusals)
	}
	if err := converge.CheckApplyRenameOrphan(fullState, objects, clustersDir, ownershipRecords); err != nil {
		refusals = append(refusals, err)
	}
	rebuiltHosts := forecastRebuiltHosts(objects, tasks, runsDir, mode, reinstallDrift)
	if mode == workflow.ApplyModeRebuild {
		if err := converge.CheckApplyOverrideDestroyProtection(planState, objects, forecastReinstallDescriptors(reinstallDrift)); err != nil {
			refusals = append(refusals, err)
		}
	}
	if err := checkKubeVirtTenantRebuildScope(fullState, clustersDir, sel, rebuiltHosts, converge.ProvisionedStorageTenants(ownershipRecords), invocation); err != nil {
		refusals = append(refusals, err)
	}
	if reclaimDevices != "" {
		clusters := converge.OwnedStorageClusters(objects)
		if unownedAuthorized {
			clusters = converge.SelectedCephStorageClusters(planState, objects)
		}
		if err := converge.CheckReclaimDestroyProtection(planState, clusters, allowDestroy); err != nil {
			refusals = append(refusals, err)
		}
		if resolved, resolveErr := converge.ResolveReclaimDevices(planState, clusters, reclaimDevices); resolveErr != nil {
			refusals = append(refusals, reclaimResolutionRefusal(resolveErr, invocation))
		} else if len(clusters) > 0 {
			if unmatched, declared := converge.UnmatchedReclaimDevices(planState, clusters, resolved); len(unmatched) > 0 {
				refusals = append(refusals, reclaimUnmatchedError(unmatched, clusters, declared))
			}
		}
	}
	return applyGateRefusalMessages(refusals)
}

func forecastRebuiltHosts(objects []workflow.ObjectClassification, tasks []workflow.ApplyTask, runsDir string, mode workflow.ApplyMode, reinstallDrift []string) []string {
	released, err := workflow.ConsumableSubstrateReleases(runsDir, tasks)
	if err != nil {
		released = nil
	}
	hosts := workflow.UnionClusterNames(reinstallDrift, workflow.SubstrateReleaseClusterNames(released))
	if mode == workflow.ApplyModeRebuild {
		_, substrateReset := workflow.OverrideDestructiveMachineSubstrate(objects)
		hosts = workflow.UnionClusterNames(hosts, substrateReset)
	}
	return hosts
}

func applyGateRefusalMessages(refusals []error) []string {
	out := make([]string, 0, len(refusals))
	for _, e := range refusals {
		out = append(out, e.Error())
	}
	return out
}

func printApplyGateForecast(stdout io.Writer, fullState, planState v1alpha1.State, tasks []workflow.ApplyTask, runsDir, clustersDir string, mode workflow.ApplyMode, allowDestroy, unownedAuthorized bool, reclaimDevices string, sel clusteraccess.Selection, reinstallDrift []string, ownershipDir string, ownershipRecords []ownership.ResourceRecord, ownershipSkipped []error, invocation *resolvedInvocation) {
	printApplyGateRefusals(stdout, applyGateForecastRefusals(fullState, planState, tasks, runsDir, clustersDir, mode, allowDestroy, unownedAuthorized, reclaimDevices, sel, reinstallDrift, ownershipDir, ownershipRecords, ownershipSkipped, invocation))
}

func printApplyGateRefusals(stdout io.Writer, refusals []string) {
	for _, msg := range refusals {
		cliout.NewContinuation(stdout).Warning("change plan", "a real run refuses before any prompt: "+msg)
	}
}
