package cli

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/cli/output"
	"github.com/crmarques/bootwright/internal/infra/locality"
	"github.com/crmarques/bootwright/internal/runtime/context"
	"github.com/crmarques/bootwright/internal/state/desired"
)

func newContextValidateCmd(stdout io.Writer) *cobra.Command {
	outputFormat := outputText
	cmd := &cobra.Command{
		Use:   "validate",
		Short: "Validate the current context setup",
		Args:  cobra.NoArgs,
		Example: `  bootwright context validate
  bootwright context validate --output json`,
	}
	cmd.Flags().StringVar(&outputFormat, "output", outputFormat, "output format: text|json")
	cmd.RunE = func(_ *cobra.Command, _ []string) error {
		if err := validateOutputFormat(outputFormat); err != nil {
			return failErr(2, err)
		}
		ctx, checks := currentContextValidation()
		blocking := blockingCheckCount(checks)
		warnings := warningCheckCount(checks)
		if outputFormat == outputJSON {
			if err := output.JSON(stdout, contextValidateReport{
				OK:        blocking == 0,
				Context:   contextValidateContextFrom(ctx),
				Checks:    contextValidateChecks(checks),
				NextSteps: contextValidateNextSteps(checks),
			}); err != nil {
				return failErr(1, err)
			}
			if blocking > 0 {
				return silentExit(1)
			}
			return nil
		}
		p := output.New(stdout)
		p.Command("context validate")
		if ctx.Name != "" {
			p.Section("Context")
			p.Fields(contextFields(ctx))
		}
		p.Checks(checks)
		printContextValidateNextSteps(p, checks)
		if blocking > 0 {
			p.Summary(output.StatusMissing, "context validate", fmt.Sprintf("%d blocking check(s)", blocking))
			return failf(1, "context validate failed: %d blocking check(s)", blocking)
		}
		if warnings > 0 {
			p.Summary(output.StatusWarn, "context validate", fmt.Sprintf("%d warning(s); context structure is ready", warnings))
			return nil
		}
		p.Summary(output.StatusOK, "context validate", fmt.Sprintf("all %d check(s) passed", len(checks)))
		return nil
	}
	return cmd
}

type contextValidateReport struct {
	OK        bool                   `json:"ok"`
	Context   contextValidateContext `json:"context,omitempty"`
	Checks    []contextValidateCheck `json:"checks"`
	NextSteps []string               `json:"nextSteps,omitempty"`
}

type contextValidateContext struct {
	Name               string `json:"name,omitempty"`
	ContextDir         string `json:"contextDir,omitempty"`
	InputDir           string `json:"inputDir,omitempty"`
	RenderedDir        string `json:"renderedDir,omitempty"`
	SecretsDir         string `json:"secretsDir,omitempty"`
	ClustersDir        string `json:"clustersDir,omitempty"`
	RunsDir            string `json:"runsDir,omitempty"`
	ManagedServicesDir string `json:"managedServicesDir,omitempty"`
	ProviderStateDir   string `json:"providerStateDir,omitempty"`
}

type contextValidateCheck struct {
	Group       string `json:"group"`
	Name        string `json:"name"`
	Status      string `json:"status"`
	Evidence    string `json:"evidence,omitempty"`
	Impact      string `json:"impact,omitempty"`
	Remediation string `json:"remediation,omitempty"`
}

func contextValidateContextFrom(ctx contextstore.Context) contextValidateContext {
	return contextValidateContext{
		Name:               ctx.Name,
		ContextDir:         ctx.BaseDir,
		InputDir:           ctx.InputDir,
		RenderedDir:        ctx.RenderedDir,
		SecretsDir:         ctx.SecretsDir,
		ClustersDir:        ctx.ClustersDir,
		RunsDir:            ctx.RunsDir,
		ManagedServicesDir: ctx.ManagedServicesDir,
		ProviderStateDir:   ctx.ProviderStateDir,
	}
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
	if missingCheckCount(contextReadinessChecks(ctx)) > 0 {
		return fmt.Errorf("context %q is not ready; run `bootwright context validate`", ctx.Name)
	}
	return nil
}

func validateContextChecks(ctx contextstore.Context) []output.Check {
	checks := contextReadinessChecks(ctx)
	state, err := desiredstate.LoadNormalizeValidate(ctx.InputPaths)
	if err != nil {
		checks = append(checks, missingContextCheck("desired state", err.Error(), "fix context input files and rerun bootwright context validate"))
	} else {
		checks = append(checks, okContextCheck("desired state", "loads, normalizes, and validates"))
		checks = append(checks, declaredSecretContextChecks(ctx.Name, ctx.SecretsDir, state)...)
		checks = append(checks, contextHostTrustChecks(ctx.BaseDir, state)...)
		result := locality.CheckController(state, controllerLocalityPolicy)
		if result.OK {
			checks = append(checks, okContextCheck("bastion locality", result.Evidence))
		} else {
			checks = append(checks, missingContextCheck("bastion locality", result.Evidence, "run bootwright from the local bastion context"))
		}
	}
	return checks
}

func declaredSecretContextChecks(contextName, secretsDir string, state v1alpha1.State) []output.Check {
	entries, err := declaredSecretEntriesForContext(contextName, secretsDir, state)
	if err != nil {
		return []output.Check{missingContextCheck("declared secrets", err.Error(), "fix Environment.spec.secrets and rerun bootwright context validate")}
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
		return "run bootwright secret generate or bootwright secret set " + entry.Name
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
		dirContextCheck("input-dir", ctx.InputDir),
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

func warningCheckCount(checks []output.Check) int {
	count := 0
	for _, check := range checks {
		if check.Status == output.StatusWarn {
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
		return []string{"bootwright bastion setup --yes", "bootwright check all"}
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
