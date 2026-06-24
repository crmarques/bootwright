package cli

import (
	"io"

	"github.com/spf13/cobra"
)

func newSecretCmd(stdin io.Reader, stdout io.Writer, stderr io.Writer) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "secret",
		Short: "Manage local install secret material",
	}
	cmd.AddCommand(
		newSecretSetCmd(stdin, stdout),
		newSecretGenerateCmd(stdout, stderr),
		newSecretCheckCmd(stdout),
		newSecretListCmd(stdout),
		newSecretShowCmd(stdout),
		newSecretDeleteCmd(stdin, stdout),
		newSecretEncryptionCmd(stdin, stdout),
	)
	requireSubcommand(cmd)
	return cmd
}
