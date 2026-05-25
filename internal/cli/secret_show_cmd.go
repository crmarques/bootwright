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
	cmd := &cobra.Command{
		Use:   "show --name <secret-name>",
		Short: "Print context-local secret material",
		Args:  cobra.NoArgs,
	}
	cmd.Flags().StringVar(&name, "name", "", "context-local secret name to print")
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
		path := filepath.Join(ctx.SecretsDir, name)
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
