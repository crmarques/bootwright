package cli

import (
	"io"

	"github.com/spf13/cobra"

	cliout "github.com/crmarques/bootwright/internal/cli/output"
)

func newSecretMaterializeCmd(stdout io.Writer, _ io.Writer) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "materialize",
		Short: "Materialize generated and context-storage secret material",
		Args:  cobra.NoArgs,
		Example: `  # Generate declared generated secrets and copy file-sourced secrets when enabled
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
		return runSecretMaterialize(stdout, "secret materialize", ctx.SecretsDir, state, secretMaterializeOptions{
			Generated:   true,
			FileSources: true,
		}, cliout.StatusSkip)
	}
	return cmd
}
