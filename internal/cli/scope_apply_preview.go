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
)

func checkKubeVirtTenantRebuildScope(state v1alpha1.State, clustersDir, clusterScope string, rebuiltHosts []string) error {
	if strings.TrimSpace(clusterScope) == "" || len(rebuiltHosts) == 0 {
		return nil
	}
	selected, _, err := clusteraccess.ClusterRootNamesForTarget(state, clusterScope)
	if err != nil {
		return err
	}
	conflicts := converge.KubeVirtTenantCollateral(state, clustersDir, rebuiltHosts, selected)
	if len(conflicts) == 0 {
		return nil
	}
	var b strings.Builder
	b.WriteString("apply --converge-drifted would reinstall KubeVirt host cluster(s) whose nested cluster(s) are left out of scope and would be annihilated:\n")
	for _, c := range conflicts {
		b.WriteString(fmt.Sprintf("  - ContainerCluster %s hosts installed %s\n", c.Host, strings.Join(c.Tenants, ", ")))
	}
	b.WriteString("include the nested cluster(s) in --clusters to rebuild them in the same run, or run bootwright destroy --stage clusters --clusters " + strings.Join(kubeVirtTenantNames(conflicts), ",") + " --yes first")
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
	if mode != workflow.ApplyModeOverride {
		return
	}
	if len(workflow.InstalledRecordedClusters(clustersDir, tasks)) == 0 {
		return
	}
	cliout.NewContinuation(stdout).Warning("converge-drifted", "plan mode does not probe cluster availability; a real run additionally reinstalls (disk wipe) any installed cluster that does not report Available=True — gated by --confirm-data-loss")
}

func forecastReinstallDescriptors(names []string) []string {
	var out []string
	for _, name := range names {
		out = append(out, fmt.Sprintf("reinstall ContainerCluster/%s (recorded install inputs drifted — --converge-drifted reinstalls it and wipes its node disks)", name))
	}
	return out
}

func printApplyGateForecast(stdout io.Writer, fullState, planState v1alpha1.State, tasks []workflow.ApplyTask, runsDir, clustersDir string, mode workflow.ApplyMode, reclaimDevices, clusterScope string, reinstallDrift []string) {
	objects, err := workflow.ClassifyApplyObjects(tasks, runsDir)
	if err != nil {
		return
	}
	var refusals []error
	if err := converge.CheckApplyRenameOrphan(fullState, objects, clustersDir); err != nil {
		refusals = append(refusals, err)
	}
	if mode == workflow.ApplyModeOverride {
		if err := converge.CheckApplyOverrideDestroyProtection(planState, objects, forecastReinstallDescriptors(reinstallDrift)); err != nil {
			refusals = append(refusals, err)
		}
		if err := checkKubeVirtTenantRebuildScope(fullState, clustersDir, clusterScope, reinstallDrift); err != nil {
			refusals = append(refusals, err)
		}
	}
	if reclaimDevices != "" {
		if err := converge.CheckReclaimDestroyProtection(planState, converge.OwnedStorageClusters(objects), mode == workflow.ApplyModeOverride); err != nil {
			refusals = append(refusals, err)
		}
	}
	for _, e := range refusals {
		cliout.NewContinuation(stdout).Warning("change plan", "a real run refuses before any prompt: "+e.Error())
	}
}
