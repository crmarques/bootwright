package cli

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"

	cliout "github.com/crmarques/bootwright/internal/cli/output"
	"github.com/crmarques/bootwright/internal/secrets"
)

func newSecretGenerateCmd(stdout io.Writer, _ io.Writer) *cobra.Command {
	var renew bool
	cmd := &cobra.Command{
		Use:   "generate",
		Short: "Generate declared secret material and copy file: sources into the context store",
		Args:  cobra.NoArgs,
		Example: `  # Create missing "generated:" secrets and copy file: sources (context mode)
  bootwright secret generate

  # Regenerate every secret declared as generated, replacing existing material
  bootwright secret generate --renew`,
	}
	cmd.Flags().BoolVar(&renew, "renew", false, "regenerate every secret declared as generated, replacing existing material")
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
			Renew:       renew,
		})
		if err != nil {
			return failErr(1, err)
		}
		p := cliout.New(stdout)
		p.Command("secret generate")
		for _, result := range results {
			p.Status(cliout.StatusOK, result.Name, result.Action)
		}
		missing, err := missingDeclaredSecretEntries(ctx.Name, ctx.SecretsDir, state)
		if err != nil {
			return failErr(1, err)
		}
		if len(missing) > 0 {
			p.Section("Needs secret set")
			for _, entry := range missing {
				p.Status(cliout.StatusMissing, entry.Name, secretContextRemediation(entry))
			}
			p.Summary(cliout.StatusWarn, "secret generate", fmt.Sprintf("%d request(s) handled; %d declared secret(s) still need secret set (run bootwright secret check)", len(results), len(missing)))
			printNextStatusHint(stdout)
			return nil
		}
		p.Summary(cliout.StatusOK, "secret generate", fmt.Sprintf("%d request(s) handled; all declared secrets present", len(results)))
		printNextStatusHint(stdout)
		return nil
	}
	return cmd
}
