package cli

import (
	"io"

	"github.com/spf13/cobra"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/runtime/secrets"
)

type generatedSelfSignedRequest struct {
	name        string
	certificate v1alpha1.SelfSignedCertificateSpec
}

func newSecretCmd(stdin io.Reader, stdout io.Writer, stderr io.Writer) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "secret",
		Short: "Manage local install secret material",
	}
	cmd.AddCommand(
		newSecretSetCmd(stdin, stdout),
		newSecretMaterializeCmd(stdout, stderr),
		newSecretGenerateCmd(stdout, stderr),
		newSecretListCmd(stdout),
		newSecretShowCmd(stdout),
		newSecretDeleteCmd(stdin, stdout),
		newSecretEncryptionCmd(stdin, stdout),
	)
	requireSubcommand(cmd)
	return cmd
}

func resolvedSecretPath(name string, env *v1alpha1.Environment, secretsDir string) string {
	return secret.ResolvePath(name, env, secretsDir)
}

func primaryEnvironmentForSync(state v1alpha1.State) *v1alpha1.Environment {
	if len(state.Environments) == 0 {
		return nil
	}
	return &state.Environments[0]
}
