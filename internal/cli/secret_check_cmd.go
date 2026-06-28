package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/cli/output"
)

// newSecretCheckCmd reports whether every declared Environment.spec.secrets
// entry has present local material. It is read-only and exits non-zero when any
// declared secret is still missing, so automation can gate on a complete secret
// set. Run `bootwright secret generate` first to materialize generated and
// file:-sourced material.
func newSecretCheckCmd(stdout io.Writer) *cobra.Command {
	var outputFormat string
	cmd := &cobra.Command{
		Use:   "check",
		Short: "Check that every declared secret has present local material",
		Args:  cobra.NoArgs,
		Example: `  # Verify all declared secrets are present
  bootwright secret check

  # Machine-readable secret status
  bootwright secret check --output json`,
	}
	addOutputFlag(cmd, &outputFormat)
	cf := addCommonFlags()
	cmd.RunE = func(_ *cobra.Command, _ []string) error {
		if err := validateOutputFormat(outputFormat); err != nil {
			return failErr(2, err)
		}
		state, err := loadDesiredState(cf)
		if err != nil {
			return failErr(1, err)
		}
		entries, err := declaredSecretEntriesForContext(cf.ctx.Name, cf.ctx.SecretsDir, state)
		if err != nil {
			return failErr(1, err)
		}
		missing := 0
		for _, entry := range entries {
			if !entry.Present {
				missing++
			}
		}
		if outputFormat == outputJSON {
			if err := output.JSON(stdout, secretListReport{Context: cf.ctx.Name, Secrets: entries}); err != nil {
				return failErr(1, err)
			}
			if missing > 0 {
				return failf(1, "secret check: %d declared secret(s) missing", missing)
			}
			return nil
		}
		p := output.New(stdout)
		p.Command("secret check")
		if len(entries) == 0 {
			p.Summary(output.StatusOK, "secret check", "no secrets declared")
			return nil
		}
		var checks []output.Check
		for _, entry := range entries {
			status := output.StatusOK
			remediation := ""
			evidence := entry.Type + " " + strings.Join(entry.Paths, ", ")
			if entry.Detail != "" {
				evidence += " (" + entry.Detail + ")"
			}
			if !entry.Present {
				status = output.StatusFail
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
		p.Checks(checks)
		if missing > 0 {
			p.Summary(output.StatusFail, "secret check", fmt.Sprintf("%d of %d declared secret(s) missing", missing, len(entries)))
			return failf(1, "secret check: %d declared secret(s) missing", missing)
		}
		p.Summary(output.StatusOK, "secret check", fmt.Sprintf("all %d declared secret(s) present", len(entries)))
		return nil
	}
	return cmd
}

func missingDeclaredSecretEntries(contextName, secretsDir string, state v1alpha1.State) ([]secretListEntry, error) {
	entries, err := declaredSecretEntriesForContext(contextName, secretsDir, state)
	if err != nil {
		return nil, err
	}
	var missing []secretListEntry
	for _, entry := range entries {
		if !entry.Present {
			missing = append(missing, entry)
		}
	}
	return missing, nil
}
