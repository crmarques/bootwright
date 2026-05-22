package cli

import (
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/crmarques/bootwright/internal/cli/output"
	"github.com/crmarques/bootwright/internal/contextstore"
	"github.com/crmarques/bootwright/internal/desiredstate"
)

func newContextValidateCmd(stdout io.Writer) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "validate",
		Aliases: []string{"validade"},
		Short:   "Validate the current context setup",
		Args:    cobra.NoArgs,
		Example: `  bootwright context validate`,
	}
	cmd.RunE = func(_ *cobra.Command, _ []string) error {
		ctx, checks := currentContextValidation()
		missing := missingCheckCount(checks)
		p := output.New(stdout)
		p.Command("context validate")
		if ctx.Name != "" {
			p.Section("Context")
			p.Fields(contextFields(ctx))
		}
		p.Checks(checks)
		if missing > 0 {
			p.Summary(output.StatusMissing, "context validate", fmt.Sprintf("%d missing check(s)", missing))
			return failf(1, "context validate failed: %d missing check(s)", missing)
		}
		p.Summary(output.StatusOK, "context validate", fmt.Sprintf("all %d check(s) passed", len(checks)))
		return nil
	}
	return cmd
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
	if _, err := desiredstate.LoadNormalizeValidate(ctx.InputPaths); err != nil {
		checks = append(checks, missingContextCheck("desired state", err.Error(), "fix context input files and rerun bootwright context validate"))
	} else {
		checks = append(checks, okContextCheck("desired state", "loads, normalizes, and validates"))
	}
	return checks
}

func contextReadinessChecks(ctx contextstore.Context) []output.Check {
	checks := []output.Check{}
	if err := contextstore.ValidateName(ctx.Name); err != nil {
		checks = append(checks, missingContextCheck("name", err.Error(), "bootwright context init <name> -f <path>"))
	} else {
		checks = append(checks, okContextCheck("name", ctx.Name))
	}
	checks = append(checks,
		dirContextCheck("base-dir", ctx.BaseDir),
		dirContextCheck("input-dir", ctx.InputDir),
		dirContextCheck("state-dir", ctx.StateDir),
		dirContextCheck("secrets-dir", ctx.SecretsDir),
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
	count := 0
	for _, check := range checks {
		if check.Status != output.StatusOK {
			count++
		}
	}
	return count
}
