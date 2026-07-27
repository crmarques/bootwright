package cli

import (
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/clusteraccess"
	"github.com/crmarques/bootwright/internal/converge"
	"github.com/crmarques/bootwright/internal/converge/workflow"
)

const flagMachinesUsage = "comma-separated Machine names to provision (default: apply whole clusters); mutually exclusive with --clusters, runs only the fabric and machines phases"

const flagMachinesDestroyUsage = "comma-separated Machine names to tear down (default: destroy whole clusters); mutually exclusive with --clusters, tears down only the machine substrate"

func machineApplyRunScope(machinesScope, clusterScope string, stageProvided bool, runScope converge.Scope) (converge.Scope, error) {
	if clusterScope != "" {
		return runScope, failErr(2, errors.New("--machines and --clusters are mutually exclusive: --machines provisions named machines, --clusters selects whole clusters"))
	}
	if !stageProvided {
		return converge.InfraScope, nil
	}
	if !converge.ScopeIsMachineLayerOnly(runScope) {
		return runScope, failErr(2, errors.New("--machines runs only the fabric and machines phases; use --stage fabric, --stage machines, --stage infra, or --through machines"))
	}
	return runScope, nil
}

func machineDestroyScope(clusterScope, stage string) (converge.Scope, string, error) {
	if clusterScope != "" {
		return converge.Scope{}, "", failErr(2, errors.New("--machines and --clusters are mutually exclusive: --machines destroys named machines, --clusters selects whole clusters"))
	}
	if s := strings.TrimSpace(stage); s != "" && s != "infra" {
		return converge.Scope{}, "", failErr(2, errors.New("--machines tears down only the machine substrate; drop --stage "+s+" or use --stage infra"))
	}
	return converge.InfraScope, "machines destroy", nil
}

func resolveScopeSelection(state v1alpha1.State, targetName, clusterScope, machinesScope string) (clusteraccess.Selection, error) {
	if machinesScope != "" {
		return clusteraccess.ResolveMachines(state, machinesScope)
	}
	return clusteraccess.Resolve(state, targetName, clusterScope)
}

func machineDestroyInstalledClusterGuard(clustersDir string, containerRoots []string) error {
	if len(containerRoots) == 0 {
		return nil
	}
	provisioned, err := workflow.RecordedProvisionedClusters(clustersDir)
	if err != nil {
		return err
	}
	provisionedSet := make(map[string]bool, len(provisioned))
	for _, name := range provisioned {
		provisionedSet[name] = true
	}
	var installed []string
	for _, name := range containerRoots {
		if provisionedSet[name] {
			installed = append(installed, name)
		}
	}
	if len(installed) == 0 {
		return nil
	}
	sort.Strings(installed)
	return fmt.Errorf("refusing to destroy machine(s) that are nodes of installed cluster(s) %s: tearing down their substrate would break the running cluster; destroy the cluster first (bootwright destroy --clusters %s), or re-run with --force to tear down the machine(s) anyway", strings.Join(installed, ", "), strings.Join(installed, ","))
}

func printDestroyRecordReset(stdout io.Writer, sel clusteraccess.Selection, runsDir, clustersDir, contextName string, runScope converge.Scope, plan converge.WorkflowPlan, resetPartial []string, succeeded map[string]bool, purgeHistory, skipUnreachable bool) error {
	if sel.MachineSelection {
		return printConvergeRecordResetProblems(stdout, converge.ResetMachineConvergeRecordsAfterDestroy(runsDir, plan.State, sel.MachineProvision, succeeded, purgeHistory, skipUnreachable))
	}
	return printConvergeRecordResetProblems(stdout, converge.ResetConvergeRecordsAfterDestroy(runsDir, clustersDir, contextName, runScope, plan.State, plan.StorageWorkNames, resetPartial, sel.WorkMachines, succeeded, purgeHistory, skipUnreachable))
}
