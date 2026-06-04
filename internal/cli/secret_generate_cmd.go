package cli

import (
	"io"

	"github.com/spf13/cobra"

	cliout "github.com/crmarques/bootwright/internal/cli/output"
)

func newSecretGenerateCmd(stdout io.Writer, _ io.Writer) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "generate",
		Short: "Generate local install secret material requested by desired state",
		Args:  cobra.NoArgs,
		Example: `  # Materialize every "generated:" secret declared by the current context
  bootwright secret generate`,
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
		return runSecretMaterialize(stdout, "secret generate", ctx.Name, ctx.SecretsDir, state, secretMaterializeOptions{Generated: true}, cliout.StatusSkip)
	}
	return cmd
}
