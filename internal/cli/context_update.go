package cli

import (
	"errors"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/crmarques/bootwright/internal/addons/nativecatalog"
	"github.com/crmarques/bootwright/internal/cli/output"
	"github.com/crmarques/bootwright/internal/state/desired"
	"github.com/crmarques/bootwright/internal/workspace"
)

func newContextUpdateCmd(stdin io.Reader, stdout io.Writer, stderr io.Writer) *cobra.Command {
	var name string
	var files []string
	var yes bool
	cmd := &cobra.Command{
		Use:   "update --name <ctx-name> -f <dir>",
		Short: "Replace a context's input from a source directory",
		Long: `Replace the input of the named context by copying a source directory into
it. The copied input fully replaces the previous input; the rest of the
context — secrets, runs, rendered output, clusters, ownership, and provider
state — is preserved.

Replacing the input discards the context's current input, so update asks for
confirmation before proceeding. Pass --yes to skip the prompt in scripts.`,
		Args: cobra.NoArgs,
		Example: `  bootwright context update --name lab -f ./examples/sno-libvirt-redfish
  bootwright context update --name lab -f ~/lab-input --yes`,
	}
	cmd.Flags().StringVar(&name, "name", "", "context name (required)")
	registerContextNameCompletion(cmd, "name")
	cmd.Flags().StringArrayVarP(&files, "file", "f", nil, "source directory whose contents replace the context input (required)")
	addYesFlag(cmd, &yes, "replace")
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		if err := workspace.ValidateName(name); err != nil {
			return failErr(2, err)
		}
		if len(files) != 1 {
			return failf(2, "context update copies exactly one source directory; pass a single -f <dir>")
		}
		source, err := workspace.ResolveWorkspaceDir(files[0])
		if err != nil {
			return failErr(2, err)
		}
		if shouldRunContextRootChild() {
			if !yes && !confirm(stdin, stdout, contextUpdateConfirmPrompt(name, source)) {
				return failErr(1, errors.New("context update aborted"))
			}
			code, err := runWithLocalRoot(cmd.Context(), []string{"context", "update", "--name", name, "-f", source, "--yes"}, stdin, stdout, stderr, false)
			if err != nil {
				return failErr(1, err)
			}
			if code != 0 {
				return silentExit(code)
			}
			return nil
		}
		ctx, err := workspace.RequireExistingContext(name)
		if err != nil {
			return failErr(1, err)
		}
		state, err := desiredstate.LoadNormalizeValidateInputFiles([]string{source})
		if err != nil {
			return failErr(1, fmt.Errorf("validate source input files: %w", err))
		}
		if err := enforceControllerLocality(state); err != nil {
			return failErr(1, err)
		}
		if !yes && !confirm(stdin, stdout, contextUpdateConfirmPrompt(ctx.Name, source)) {
			return failErr(1, errors.New("context update aborted"))
		}
		if err := workspace.EnsureDirs(ctx); err != nil {
			return failErr(1, err)
		}
		snapshot, err := workspace.ReplaceInput(ctx, source, "context update", nativecatalog.ReferencedStoreAddons(state))
		if err != nil {
			return failErr(1, err)
		}
		p := output.New(stdout)
		p.Command("context update")
		p.Section("Context")
		p.Status(output.StatusOK, ctx.Name, "input replaced")
		p.Status(output.StatusOK, "input", "copied from "+source)
		p.Status(output.StatusOK, "input dir", ctx.InputDir)
		if snapshot != "" {
			p.Status(output.StatusOK, "previous input", "saved to history: "+snapshot)
		}
		p.Status(output.StatusOK, "state", "preserved (secrets, runs, rendered, clusters, ownership)")
		p.Summary(output.StatusOK, "context "+ctx.Name, "input replaced from "+source)
		printNextStatusHint(stdout)
		return nil
	}
	return cmd
}

func contextUpdateConfirmPrompt(name, source string) string {
	return fmt.Sprintf("Replace the input of context %q from %s? The current input is discarded (secrets, runs, rendered output, and clusters are preserved). [y/N] (default: no): ", name, source)
}
