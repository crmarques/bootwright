package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	cliout "github.com/crmarques/bootwright/internal/cli/output"
	"github.com/crmarques/bootwright/internal/converge"
	"github.com/crmarques/bootwright/internal/state/graph"
)

func infraComponentServiceRefs(state v1alpha1.State, artifactServerOnly bool) []converge.InfraComponentServiceRef {
	var out []converge.InfraComponentServiceRef
	for _, service := range stategraph.ResolveMachineServices(state).Services {
		if !service.IsInfraComponentService() {
			continue
		}
		if artifactServerOnly && service.Identity.Kind != v1alpha1.ComponentSlotArtifactServer {
			continue
		}
		out = append(out, converge.InfraComponentServiceRef{Name: service.Identity.Name, Kind: service.Identity.Kind})
	}
	return out
}

func printPartialStorageDestroyWarning(stdout io.Writer, partial converge.PartialStorageDestroy, err error) {
	if len(partial.Recorded) > 0 {
		cliout.NewContinuation(stdout).Warning("partial destroy", fmt.Sprintf(
			"storage cluster(s) %s left partially destroyed: unreachable node(s) were skipped, so their OSD devices were not wiped and local Ceph state remains. Re-run destroy once the nodes are back up, or wipe them manually, before reusing the hardware; bootwright status flags them.",
			strings.Join(partial.Recorded, ", ")))
	}
	if len(partial.Unrecorded) > 0 {
		cliout.NewContinuation(stdout).Warning("partial destroy", fmt.Sprintf(
			"storage cluster(s) %s had unreachable node(s) but no Bootwright ownership record — treated as never provisioned, so nothing was wiped and no durable partial-destroy marker exists. If these clusters ever held data, verify the nodes manually before reusing the hardware.",
			strings.Join(partial.Unrecorded, ", ")))
	}
	if err != nil {
		cliout.NewContinuation(stdout).Warning("partial destroy", "could not fully record partial-destroy state: "+err.Error()+"; their converge records are kept so the next apply/destroy fails closed")
	}
}

func printConvergeRecordResetProblems(stdout io.Writer, problems []error) error {
	if len(problems) == 0 {
		return nil
	}
	details := make([]string, 0, len(problems))
	for _, problem := range problems {
		cliout.NewContinuation(stdout).Warning("stale records", problem.Error()+"; run records may still claim this resource is converged — remove the reported record or re-run destroy before the next apply")
		details = append(details, problem.Error())
	}
	return fmt.Errorf("teardown finished but %d post-destroy record cleanup(s) failed: %s; a surviving converge record makes the next apply classify the destroyed resource as already converged and skip re-provisioning it, and a missing substrate-release record leaves its reinstall unauthorized — remove the reported file(s) under the context's runs/ tree, or re-run bootwright destroy for the same scope, before the next apply", len(problems), strings.Join(details, "; "))
}
