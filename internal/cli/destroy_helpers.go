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

func destroyAuthorizesUnownedDevices(auth *authorizations, runScope converge.Scope) bool {
	return converge.ScopeTearsClusterLayer(runScope) && auth.allows(authorizeUnownedDevices)
}

func resolvedPostDestroyRetry(invocation resolvedInvocation, skipUnreachable bool) (retryCommand, error) {
	if skipUnreachable {
		return invocation.retry(retryIntent{excludedAuthorization: authorizeUnreachableNodes})
	}
	return invocation.retry(retryIntent{})
}

func selectedInfraComponentServiceRefs(state v1alpha1.State, artifactServerOnly, degradingOnly bool, hosts map[string]bool) []converge.InfraComponentServiceRef {
	var out []converge.InfraComponentServiceRef
	for _, service := range stategraph.ResolveMachineServices(state).Services {
		if !service.IsInfraComponentService() || strings.TrimSpace(service.MachineRef) == "" {
			continue
		}
		if artifactServerOnly && service.Identity.Kind != v1alpha1.ComponentSlotArtifactServer {
			continue
		}
		if degradingOnly && !stategraph.SharedServiceDegradesUnderScope(service.Identity.Kind) {
			continue
		}
		if hosts != nil && !hosts[service.MachineRef] {
			continue
		}
		out = append(out, converge.InfraComponentServiceRef{
			Name: strings.TrimSpace(service.Identity.ProviderName) + "-" + strings.TrimSpace(service.Identity.Name),
			Kind: service.Identity.Kind,
			Host: service.MachineRef,
		})
	}
	return out
}

func selectedControllerNameResolutionServiceRefs(state v1alpha1.State, hosts map[string]bool) []converge.InfraComponentServiceRef {
	services := selectedInfraComponentServiceRefs(state, false, false, hosts)
	out := make([]converge.InfraComponentServiceRef, 0, len(services))
	for _, service := range services {
		if service.Kind == v1alpha1.ComponentSlotNameResolution {
			out = append(out, service)
		}
	}
	return out
}

func partialStorageDestroyNodes(partial converge.PartialStorageDestroy) string {
	if len(partial.Reasons) > 0 {
		return " Skipped node(s), with what each refusal reported: " + strings.Join(partial.Reasons, "; ") + "."
	}
	if partial.Skipped == "" {
		return ""
	}
	return " Skipped node(s): " + strings.ReplaceAll(partial.Skipped, ",", ", ") + "."
}

func printPartialStorageDestroyWarning(stdout io.Writer, partial converge.PartialStorageDestroy, err error, retry retryCommand) {
	if len(partial.Recorded) > 0 {
		cliout.NewContinuation(stdout).Warning("partial destroy", fmt.Sprintf(
			"storage cluster(s) %s left partially destroyed: unreachable node(s) were skipped, so their OSD devices were not wiped, their Ceph daemons are still running and local Ceph state remains.%s Once every selected node answers, re-run `%s` before reusing the hardware; bootwright status flags the retained evidence. A skipped node keeps serving the cluster this run reported destroyed.",
			strings.Join(partial.Recorded, ", "), partialStorageDestroyNodes(partial), retry.String()))
	}
	if len(partial.Unrecorded) > 0 {
		cliout.NewContinuation(stdout).Warning("partial destroy", fmt.Sprintf(
			"storage cluster(s) %s had unreachable node(s) but no Bootwright ownership record — treated as never provisioned, so nothing was wiped and no durable partial-destroy marker exists.%s If these clusters ever held data, verify the nodes manually before reusing the hardware.",
			strings.Join(partial.Unrecorded, ", "), partialStorageDestroyNodes(partial)))
	}
	if err != nil {
		cliout.NewContinuation(stdout).Warning("partial destroy", "could not fully record partial-destroy state: "+err.Error()+"; their converge records are kept so the next apply/destroy fails closed. Repair the reported context state and re-run `"+retry.String()+"`")
	}
}

func printConvergeRecordResetProblems(stdout io.Writer, problems []error, retry retryCommand) error {
	if len(problems) == 0 {
		return nil
	}
	details := make([]string, 0, len(problems))
	for _, problem := range problems {
		cliout.NewContinuation(stdout).Warning("stale records", problem.Error()+"; run records may still claim this resource is converged — remove the reported record or re-run `"+retry.String()+"` before the next apply")
		details = append(details, problem.Error())
	}
	return fmt.Errorf("teardown finished but %d post-destroy record cleanup(s) failed: %s; a surviving converge record makes the next apply classify the destroyed resource as already converged and skip re-provisioning it, and a missing substrate-release record leaves its reinstall unauthorized — remove the reported file(s) under the context's runs/ tree, or re-run `%s`, before the next apply", len(problems), strings.Join(details, "; "), retry.String())
}
