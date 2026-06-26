package cli

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/crmarques/bootwright/internal/cli/output"
	"github.com/crmarques/bootwright/internal/workspace"
)

func newContextDeleteCmd(stdin io.Reader, stdout io.Writer, stderr io.Writer) *cobra.Command {
	var name string
	var purge bool
	var yes bool
	cmd := &cobra.Command{
		Use:     "delete --name <ctx-name>",
		Short:   "Delete a context",
		Args:    cobra.NoArgs,
		Example: `  bootwright context delete --name lab --purge --yes`,
	}
	cmd.Flags().StringVar(&name, "name", "", "context name (required)")
	cmd.Flags().BoolVar(&purge, "purge", false, "also delete the context base directory")
	cmd.Flags().BoolVar(&yes, "yes", false, "skip confirmation when purging context files")
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		if err := workspace.ValidateName(name); err != nil {
			return failErr(2, err)
		}
		if !purge {
			return failf(1, "context %q is stored in shared root state; rerun with --purge --yes to delete it", name)
		}
		if purge && shouldRunContextRootChild() {
			ctx, ctxErr := workspace.NewContext(name)
			if ctxErr != nil {
				return failErr(2, ctxErr)
			}
			if !yes && !confirm(stdin, stdout, fmt.Sprintf("Delete %s and all files under %s? [y/N] (default: no): ", name, ctx.BaseDir)) {
				return failErr(1, errors.New("context delete aborted"))
			}
			code, err := runWithLocalRoot(cmd.Context(), []string{"context", "delete", "--name", name, "--purge", "--yes"}, stdin, stdout, stderr, true)
			if err != nil {
				return failErr(1, err)
			}
			if code != 0 {
				return silentExit(code)
			}
			return nil
		}
		registry, store, err := workspace.LoadDefaultStore()
		if err != nil {
			return failErr(1, err)
		}
		present, err := workspace.ContextBaseDirPresent(name)
		if err != nil {
			return failErr(1, err)
		}
		if present {
			ctx, err := workspace.RequireExistingContext(name)
			if err != nil {
				return failErr(1, err)
			}
			if !yes && !confirm(stdin, stdout, fmt.Sprintf("Delete %s and all files under %s? [y/N] (default: no): ", name, ctx.BaseDir)) {
				return failErr(1, errors.New("context delete aborted"))
			}
			if err := workspace.SafePurgeBaseDir(ctx); err != nil {
				return failErr(1, err)
			}
		}
		clearedRegistry := false
		if strings.TrimSpace(store.Current) == name {
			store.Current = ""
			if err := workspace.Save(registry, store); err != nil {
				return failErr(1, err)
			}
			clearedRegistry = true
		}
		p := output.New(stdout)
		p.Command("context delete")
		switch {
		case present:
			p.Summary(output.StatusOK, "context "+name, "base directory removed")
		case clearedRegistry:
			p.Summary(output.StatusOK, "context "+name, "not in shared storage; cleared local registry selection")
		default:
			p.Summary(output.StatusOK, "context "+name, "not in shared storage; nothing to remove")
		}
		return nil
	}
	return cmd
}
