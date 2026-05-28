package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/crmarques/bootwright/internal/cli/output"
	"github.com/crmarques/bootwright/internal/desiredstate"
	"github.com/crmarques/bootwright/internal/safefs"
)

func newSecretDeleteCmd(stdin io.Reader, stdout io.Writer) *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "delete <name>",
		Short: "Delete local secret material from the current context",
		Args:  cobra.ExactArgs(1),
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "skip the delete confirmation prompt")
	cf := addCommonFlags()
	cmd.RunE = func(_ *cobra.Command, args []string) error {
		ctx, err := cf.resolveLocalOnly()
		if err != nil {
			return failErr(1, err)
		}
		name := args[0]
		if !desiredstate.IsDNSLabel(name) {
			return failf(2, "<name> must be a lowercase DNS label")
		}
		paths, err := existingSecretPaths(ctx.SecretsDir, name)
		if err != nil {
			return failErr(1, err)
		}
		if len(paths) == 0 {
			return failf(1, "secret %q not found in %s", name, ctx.SecretsDir)
		}
		if !yes && !confirm(stdin, stdout, fmt.Sprintf("Delete secret %s from %s? [y/N] (default: no): ", name, ctx.SecretsDir)) {
			return failErr(1, errors.New("secret delete aborted"))
		}
		for _, path := range paths {
			if err := os.Remove(path); err != nil {
				return failErr(1, fmt.Errorf("delete %s: %w", path, err))
			}
		}
		output.New(stdout).Summary(output.StatusOK, name, "deleted "+strings.Join(paths, ", "))
		return nil
	}
	return cmd
}

func existingSecretPaths(secretsDir, name string) ([]string, error) {
	paths := []string{
		filepath.Join(secretsDir, name),
		filepath.Join(secretsDir, name+".key"),
		filepath.Join(secretsDir, name+".pub"),
	}
	var out []string
	for _, path := range paths {
		exists, err := safefs.RegularFileExists(path)
		if err != nil {
			return nil, err
		}
		if exists {
			out = append(out, path)
		}
	}
	return out, nil
}
