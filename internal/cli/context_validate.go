package cli

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/cli/output"
	"github.com/crmarques/bootwright/internal/infra/locality"
	"github.com/crmarques/bootwright/internal/runtime/context"
	"github.com/crmarques/bootwright/internal/state/desired"
)

// The context setup checks below back `bootwright status`: the status spine
// reports them inline on a healthy context and falls back to them when the
// current context is missing or not ready (the surface that used to be
// `context validate`).

// runStatusSetup reports the context setup checks when the current context is
// missing or not ready, instead of failing with a bare error. It shows what is
// wrong and the remediation for each failing check.
func runStatusSetup(stdout io.Writer, resolveErr error) error {
	p := output.New(stdout)
	p.Command("status")
	ctx, checks := currentContextValidation()
	if ctx.Name != "" {
		p.Section("Context")
		p.Fields(contextFields(ctx))
	}
	p.Checks(checks)
	printContextValidateNextSteps(p, checks)
	p.Summary(output.StatusMissing, "status", resolveErr.Error())
	return failf(1, "%v", resolveErr)
}

type contextValidateCheck struct {
	Group       string `json:"group"`
	Name        string `json:"name"`
	Status      string `json:"status"`
	Evidence    string `json:"evidence,omitempty"`
	Impact      string `json:"impact,omitempty"`
	Remediation string `json:"remediation,omitempty"`
}

func contextValidateChecks(checks []output.Check) []contextValidateCheck {
	out := make([]contextValidateCheck, 0, len(checks))
	for _, check := range checks {
		out = append(out, contextValidateCheck{
			Group:       check.Group,
			Name:        check.Name,
			Status:      string(check.Status),
			Evidence:    check.Evidence,
			Impact:      check.Impact,
			Remediation: check.Remediation,
		})
	}
	return out
}

func currentContextValidation() (contextstore.Context, []output.Check) {
	registry, err := contextstore.DefaultRegistryPath()
	if err != nil {
		return contextstore.Context{}, []output.Check{missingContextCheck("registry", err.Error(), "fix HOME and rerun")}
	}
	checks := []output.Check{fileContextCheck("registry", registry)}
	store, err := contextstore.Load(registry)
	if err != nil {
		checks[0] = missingContextCheck("registry", err.Error(), "fix or remove "+registry)
		return contextstore.Context{}, append(checks, missingContextCheck("current", "registry cannot be loaded", "bootwright context init <name> -f <path>"))
	}
	ctx, err := contextstore.Current(store)
	if err != nil {
		return contextstore.Context{}, append(checks, missingContextCheck("current", err.Error(), "bootwright context init <name> -f <path>"))
	}
	checks = append(checks, okContextCheck("current", ctx.Name))
	checks = append(checks, validateContextChecks(ctx)...)
	return ctx, checks
}

func ensureContextReady(ctx contextstore.Context) error {
	// The recorded workspace is the single owner of authored YAML; a missing,
	// moved, or unreadable workspace is a hard, named failure with no
	// fallback, so surface its own error instead of the generic one.
	if err := contextstore.ValidateInputSource(ctx); err != nil {
		return err
	}
	if missingCheckCount(contextReadinessChecks(ctx)) > 0 {
		return fmt.Errorf("context %q is not ready; run `bootwright status`", ctx.Name)
	}
	return nil
}

func validateContextChecks(ctx contextstore.Context) []output.Check {
	checks := contextReadinessChecks(ctx)
	state, err := desiredstate.LoadNormalizeValidate(ctx.InputPaths)
	if err != nil {
		checks = append(checks, missingContextCheck("desired state", err.Error(), "fix context input files and rerun bootwright validate"))
	} else {
		checks = append(checks, okContextCheck("desired state", "loads, normalizes, and validates"))
		checks = append(checks, declaredSecretContextChecks(ctx.Name, ctx.SecretsDir, state)...)
		checks = append(checks, contextHostTrustChecks(ctx.BaseDir, state)...)
		checks = append(checks, bastionLocalityCheck(state))
	}
	return checks
}

// bastionLocalityCheck reports whether bootwright runs on the declared bastion
// for the loaded state; status surfaces it both inline and in the setup
// fallback.
func bastionLocalityCheck(state v1alpha1.State) output.Check {
	result := locality.CheckController(state, controllerLocalityPolicy)
	if result.OK {
		return okContextCheck("bastion locality", result.Evidence)
	}
	return missingContextCheck("bastion locality", result.Evidence, "run bootwright from the local bastion context")
}

func declaredSecretContextChecks(contextName, secretsDir string, state v1alpha1.State) []output.Check {
	entries, err := declaredSecretEntriesForContext(contextName, secretsDir, state)
	if err != nil {
		return []output.Check{missingContextCheck("declared secrets", err.Error(), "fix Environment.spec.secrets and rerun bootwright validate")}
	}
	if len(entries) == 0 {
		return []output.Check{okContextCheck("declared secrets", "none declared")}
	}
	checks := make([]output.Check, 0, len(entries))
	for _, entry := range entries {
		status := output.StatusOK
		remediation := ""
		evidence := entry.Type + " " + strings.Join(entry.Paths, ", ")
		if entry.Detail != "" {
			evidence += " (" + entry.Detail + ")"
		}
		if !entry.Present {
			status = output.StatusWarn
			remediation = secretContextRemediation(entry)
		}
		checks = append(checks, output.Check{
			Group:       "Declared secrets",
			Name:        entry.Name,
			Status:      status,
			Evidence:    evidence,
			Remediation: remediation,
		})
	}
	return checks
}

func secretContextRemediation(entry secretListEntry) string {
	switch {
	case strings.HasPrefix(entry.Type, "generated:"):
		return "run bootwright secret sync or bootwright secret set " + entry.Name
	case entry.Type == "file" || strings.HasPrefix(entry.Type, "file:"):
		return "create the referenced file or update Environment.spec.secrets entry " + entry.Name
	case strings.Contains(entry.Type, "tls"):
		return "run bootwright secret set " + entry.Name + " --tls-cert <path> --tls-key <path>"
	default:
		return "run bootwright secret set " + entry.Name
	}
}

func contextReadinessChecks(ctx contextstore.Context) []output.Check {
	checks := []output.Check{}
	if err := contextstore.ValidateName(ctx.Name); err != nil {
		checks = append(checks, missingContextCheck("name", err.Error(), "bootwright context init <name> -f <path>"))
	} else {
		checks = append(checks, okContextCheck("name", ctx.Name))
	}
	if err := contextstore.ValidateContext(ctx); err != nil {
		checks = append(checks, missingContextCheck("path layout", err.Error(), "bootwright context init <name> -f <path> --yes"))
		return checks
	}
	checks = append(checks,
		dirContextCheck("context-dir", ctx.BaseDir),
		workspaceContextCheck(ctx),
		dirContextCheck("rendered-dir", ctx.RenderedDir),
		dirContextCheck("secrets-dir", ctx.SecretsDir),
		dirContextCheck("clusters-dir", ctx.ClustersDir),
		dirContextCheck("runs-dir", ctx.RunsDir),
		dirContextCheck("managed-services-dir", ctx.ManagedServicesDir),
		dirContextCheck("provider-state-dir", ctx.ProviderStateDir),
		dirContextCheck("ownership-dir", ctx.OwnershipDir),
		secretsDirModeCheck(ctx.SecretsDir),
	)
	return checks
}

// workspaceContextCheck reports whether the workspace directory recorded at
// `context init` still exists and is readable. Evidence names the recorded
// path; remediation is re-running context init -f to re-point it.
func workspaceContextCheck(ctx contextstore.Context) output.Check {
	if err := contextstore.ValidateInputSource(ctx); err != nil {
		return missingContextCheck("workspace", err.Error(), fmt.Sprintf("bootwright context init %s -f <dir>", ctx.Name))
	}
	return okContextCheck("workspace", ctx.InputDir)
}

func fileContextCheck(name, path string) output.Check {
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return missingContextCheck(name, path+" missing", "bootwright context init <name> -f <path>")
	}
	if err != nil {
		return missingContextCheck(name, err.Error(), "fix "+path)
	}
	if info.IsDir() {
		return missingContextCheck(name, path+" is a directory", "replace it with a file")
	}
	return okContextCheck(name, path)
}

func dirContextCheck(name, path string) output.Check {
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return missingContextCheck(name, path+" missing", "bootwright context init <name> -f <path> --yes")
	}
	if err != nil {
		return missingContextCheck(name, err.Error(), "fix "+path)
	}
	if !info.IsDir() {
		return missingContextCheck(name, path+" is not a directory", "replace it with a directory")
	}
	return okContextCheck(name, path)
}

func secretsDirModeCheck(path string) output.Check {
	info, err := os.Stat(path)
	if err != nil {
		return missingContextCheck("secrets-dir mode", path+" cannot be checked", "bootwright context init <name> -f <path> --yes")
	}
	if !info.IsDir() {
		return missingContextCheck("secrets-dir mode", path+" is not a directory", "replace it with a directory")
	}
	if mode := info.Mode().Perm(); mode != 0o700 {
		return missingContextCheck("secrets-dir mode", fmt.Sprintf("%s mode %04o; expected 0700", path, mode), "chmod 0700 "+path)
	}
	return okContextCheck("secrets-dir mode", path+" mode 0700")
}

func okContextCheck(name, evidence string) output.Check {
	return output.Check{Group: "Context", Name: name, Status: output.StatusOK, Evidence: evidence}
}

func missingContextCheck(name, evidence, remediation string) output.Check {
	return output.Check{Group: "Context", Name: name, Status: output.StatusMissing, Evidence: evidence, Remediation: remediation}
}

func missingCheckCount(checks []output.Check) int {
	return blockingCheckCount(checks)
}

func blockingCheckCount(checks []output.Check) int {
	count := 0
	for _, check := range checks {
		if check.Status == output.StatusMissing || check.Status == output.StatusFail {
			count++
		}
	}
	return count
}

func contextValidateNextSteps(checks []output.Check) []string {
	seen := map[string]bool{}
	var steps []string
	for _, check := range checks {
		if check.Status == output.StatusOK || check.Remediation == "" {
			continue
		}
		if seen[check.Remediation] {
			continue
		}
		seen[check.Remediation] = true
		steps = append(steps, check.Remediation)
	}
	if len(steps) == 0 {
		return []string{"bootwright bastion setup --yes", "bootwright preflight all"}
	}
	return steps
}

func printContextValidateNextSteps(p *output.Printer, checks []output.Check) {
	steps := contextValidateNextSteps(checks)
	if len(steps) == 0 {
		return
	}
	items := make([]output.Item, 0, len(steps))
	for _, step := range steps {
		items = append(items, output.Item{Label: step})
	}
	p.Section("Next steps")
	p.List(items)
}
