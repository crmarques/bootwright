package cli

import (
	"io"

	"github.com/spf13/cobra"

	cliout "github.com/crmarques/bootwright/internal/cli/output"
)

func newSecretMaterializeCmd(stdout io.Writer, _ io.Writer) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "materialize",
		Short: `Create "generated:" secrets and copy file-sourced material into the context store`,
		Args:  cobra.NoArgs,
		Example: `  # Create declared "generated:" secrets and, when secretStorage.mode is context,
  # copy file:-sourced material into the encrypted context store
  bootwright secret materialize`,
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
		return runSecretMaterialize(stdout, "secret materialize", ctx.Name, ctx.SecretsDir, state, secretMaterializeOptions{
			Generated:   true,
			FileSources: true,
		}, cliout.StatusSkip)
	}
	return cmd
}
