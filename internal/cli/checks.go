package cli

import (
	"fmt"
	"path/filepath"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/cli/output"
	"github.com/crmarques/bootwright/internal/converge"
	"github.com/crmarques/bootwright/internal/converge/remedy"
	"github.com/crmarques/bootwright/internal/converge/workflow"
	"github.com/crmarques/bootwright/internal/preflight"
)

type preflightCheck = output.Check

func preflightChecksToOutput(checks []preflight.Check) []output.Check {
	return preflightChecksToOutputWithFormatter(checks, nil)
}

func preflightChecksToOutputForApply(checks []preflight.Check, invocation resolvedInvocation) []output.Check {
	return preflightChecksToOutputWithFormatter(checks, func(request remedy.Request) (string, error) {
		return applyRemedialGuidance(request, invocation)
	})
}

func preflightChecksToOutputForPreflight(checks []preflight.Check, invocation resolvedInvocation) []output.Check {
	return preflightChecksToOutputWithFormatter(checks, func(request remedy.Request) (string, error) {
		if request.Action != remedy.ActionReconcileContainerClusterThenRetrySameSelection {
			return "", fmt.Errorf("typed remedy action %q cannot be inferred from a read-only preflight", request.Action)
		}
		cluster, command, err := containerClusterReconcileCommand(request, invocation)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("reconcile ContainerCluster/%s interactively with `%s`, then rerun this preflight with its original flags", cluster, command.String()), nil
	})
}

type preflightRemedyFormatter func(remedy.Request) (string, error)

func preflightChecksToOutputWithFormatter(checks []preflight.Check, formatter preflightRemedyFormatter) []output.Check {
	out := make([]output.Check, 0, len(checks))
	for _, check := range checks {
		remediation := check.Remediation
		if check.Remedy.Action != "" {
			switch {
			case formatter == nil:
				remediation += "; no runnable command is suggested because the CLI has no resolved apply invocation for this check"
			default:
				guidance, err := formatter(check.Remedy)
				if err != nil {
					remediation += "; no runnable command is suggested: " + err.Error()
				} else {
					remediation += "; " + guidance
				}
			}
		}
		out = append(out, output.Check{
			Group:       check.Group,
			Name:        check.Name,
			Status:      output.Status(check.Status),
			Evidence:    check.Evidence,
			Impact:      check.Impact,
			Remediation: remediation,
		})
	}
	return out
}

func preflightPhases(selected []converge.Phase) []preflight.Phase {
	out := make([]preflight.Phase, 0, len(selected))
	for _, phase := range selected {
		out = append(out, preflight.Phase{Name: phase.Name})
	}
	return out
}

func preflightRemedyInvocation(contextName string, verbose, trustOnFirstUse bool) (resolvedInvocation, error) {
	return newResolvedInvocation(invocationApply, contextName, invocationFlags{
		mode:            workflow.ApplyModeReconcile,
		askBecomePass:   askBecomePassDefault(),
		trustOnFirstUse: trustOnFirstUse,
		verbose:         verbose,
	})
}

func contextHostTrustChecks(ctxBaseDir string, state v1alpha1.State) []preflightCheck {
	checks := preflightChecksToOutput(preflight.ManagedHostTrustChecks(state, sshtrustKnownSecretsDir(ctxBaseDir), preflight.DefaultDeps, controllerLocalityPolicy, preflight.StatusWarn, nil))
	for i := range checks {
		if checks[i].Status == output.StatusWarn {
			checks[i].Group = "SSH host trust"
		}
	}
	return checks
}

func sshtrustKnownSecretsDir(ctxBaseDir string) string {
	if ctxBaseDir == "" {
		return ""
	}
	return filepath.Join(ctxBaseDir, "secrets")
}

func okCheck(group, name, evidence string) preflightCheck {
	return preflightCheck{
		Group:    group,
		Name:     name,
		Status:   output.StatusOK,
		Evidence: evidence,
	}
}

func infoCheck(group, name, evidence string) preflightCheck {
	return preflightCheck{
		Group:    group,
		Name:     name,
		Status:   output.StatusInfo,
		Evidence: evidence,
	}
}

func warnCheck(group, name, evidence, impact, remediation string) preflightCheck {
	return preflightCheck{
		Group:       group,
		Name:        name,
		Status:      output.StatusWarn,
		Evidence:    evidence,
		Impact:      impact,
		Remediation: remediation,
	}
}

func failCheck(group, name, evidence, impact, remediation string) preflightCheck {
	return preflightCheck{
		Group:       group,
		Name:        name,
		Status:      output.StatusFail,
		Evidence:    evidence,
		Impact:      impact,
		Remediation: remediation,
	}
}
