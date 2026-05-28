package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/crmarques/bootwright/internal/desiredstate"
	"github.com/crmarques/bootwright/internal/safefs"
)

func newSecretShowCmd(stdout io.Writer) *cobra.Command {
	var name string
	part := "primary"
	cmd := &cobra.Command{
		Use:   "show --name <secret-name>",
		Short: "Print context-local secret material",
		Args:  cobra.NoArgs,
	}
	cmd.Flags().StringVar(&name, "name", "", "context-local secret name to print")
	cmd.Flags().StringVar(&part, "part", part, "secret material part: primary|private|public|tls-key")
	_ = cmd.MarkFlagRequired("name")
	cf := addCommonFlags()
	cmd.RunE = func(_ *cobra.Command, _ []string) error {
		ctx, err := cf.resolve()
		if err != nil {
			return failErr(1, err)
		}
		if !desiredstate.IsDNSLabel(name) {
			return failf(2, "--name must be a lowercase DNS label")
		}
		path, err := localSecretPartPath(ctx.SecretsDir, name, part)
		if err != nil {
			return failErr(2, err)
		}
		exists, err := safefs.RegularFileExists(path)
		if err != nil {
			return failErr(1, err)
		}
		if !exists {
			return failf(1, "secret %q not found in %s", name, ctx.SecretsDir)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return failErr(1, fmt.Errorf("read secret %s: %w", path, err))
		}
		if _, err := stdout.Write(data); err != nil {
			return failErr(1, fmt.Errorf("write secret %s: %w", name, err))
		}
		return nil
	}
	return cmd
}

func localSecretPartPath(secretsDir, name, part string) (string, error) {
	switch part {
	case "primary", "private":
		return filepath.Join(secretsDir, name), nil
	case "public":
		return filepath.Join(secretsDir, name+".pub"), nil
	case "tls-key":
		return filepath.Join(secretsDir, name+".key"), nil
	default:
		return "", fmt.Errorf("--part must be one of primary, private, public, tls-key")
	}
}
