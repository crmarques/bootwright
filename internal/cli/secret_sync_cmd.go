package cli

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/crmarques/bootwright/api/v1alpha1"
	cliout "github.com/crmarques/bootwright/internal/cli/output"
	"github.com/crmarques/bootwright/internal/secrets"
)

// newSecretSyncCmd converges local secret material toward the declared
// Environment.spec.secrets: it creates missing "generated:" secrets, copies
// "file:"-sourced material into the encrypted context store when
// secretStorage.mode is context, and reports the declared secrets that still
// need `secret set`. It fails when declared material remains missing so
// automation can gate on a complete secret set.
func newSecretSyncCmd(stdout io.Writer, _ io.Writer) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Converge declared secret material and report what is still missing",
		Args:  cobra.NoArgs,
		Example: `  # Create "generated:" secrets, copy file: sources (context mode), report gaps
  bootwright secret sync`,
	}
	cf := addCommonFlags()
	cmd.RunE = func(c *cobra.Command, _ []string) error {
		ctx, err := cf.resolve()
		if err != nil {
			return failErr(1, err)
		}
		warnSecretsDirPerms(ctx.SecretsDir, c.ErrOrStderr())
		state, err := loadDesiredState(cf)
		if err != nil {
			return failErr(1, err)
		}
		results, err := secret.MaterializeForContext(ctx.Name, ctx.SecretsDir, state, secret.MaterializeOptions{
			Generated:   true,
			FileSources: true,
		})
		if err != nil {
			return failErr(1, err)
		}
		p := cliout.New(stdout)
		p.Command("secret sync")
		for _, result := range results {
			p.Status(cliout.StatusOK, result.Name, result.Action)
		}
		missing, err := missingDeclaredSecretEntries(ctx.Name, ctx.SecretsDir, state)
		if err != nil {
			return failErr(1, err)
		}
		if len(missing) > 0 {
			p.Section("Still missing")
			for _, entry := range missing {
				p.Status(cliout.StatusMissing, entry.Name, secretContextRemediation(entry))
			}
			p.Summary(cliout.StatusMissing, "secret sync", fmt.Sprintf("%d request(s) handled; %d declared secret(s) still missing", len(results), len(missing)))
			printNextStatusHint(stdout)
			return failf(1, "secret sync: %d declared secret(s) still missing", len(missing))
		}
		p.Summary(cliout.StatusOK, "secret sync", fmt.Sprintf("%d request(s) handled; all declared secrets present", len(results)))
		printNextStatusHint(stdout)
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
