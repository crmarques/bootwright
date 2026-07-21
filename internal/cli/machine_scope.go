package cli

import (
	"errors"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/clusteraccess"
	"github.com/crmarques/bootwright/internal/converge"
)

const flagMachinesUsage = "comma-separated Machine names to provision (default: apply whole clusters); mutually exclusive with --clusters, runs only the fabric and machines phases"

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

func resolveScopeSelection(state v1alpha1.State, targetName, clusterScope, machinesScope string) (clusteraccess.Selection, error) {
	if machinesScope != "" {
		return clusteraccess.ResolveMachines(state, machinesScope)
	}
	return clusteraccess.Resolve(state, targetName, clusterScope)
}
