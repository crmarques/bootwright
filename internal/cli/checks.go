package cli

import (
	"path/filepath"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/cli/output"
	"github.com/crmarques/bootwright/internal/converge"
	"github.com/crmarques/bootwright/internal/preflight"
)

type preflightCheck = output.Check

// preflightChecksToOutput adapts plain preflight check data to the CLI output
// vocabulary at the presentation boundary.
func preflightChecksToOutput(checks []preflight.Check) []output.Check {
	out := make([]output.Check, 0, len(checks))
	for _, check := range checks {
		out = append(out, output.Check{
			Group:       check.Group,
			Name:        check.Name,
			Status:      output.Status(check.Status),
			Evidence:    check.Evidence,
			Impact:      check.Impact,
			Remediation: check.Remediation,
		})
	}
	return out
}

// preflightPhases narrows the CLI phase selection to the names preflight
// scoping consumes.
func preflightPhases(selected []converge.Phase) []preflight.Phase {
	out := make([]preflight.Phase, 0, len(selected))
	for _, phase := range selected {
		out = append(out, preflight.Phase{Name: phase.Name})
	}
	return out
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
