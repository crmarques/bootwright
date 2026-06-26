package cli

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/crmarques/bootwright/internal/cli/output"
	"github.com/crmarques/bootwright/internal/converge/bundle"
	"github.com/crmarques/bootwright/internal/state/desired"
	"github.com/crmarques/bootwright/internal/workspace"
)

const (
	bootwrightContextEnv = "BOOTWRIGHT_CONTEXT"
)

func newContextCmd(stdin io.Reader, stdout io.Writer, stderr io.Writer) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "context <command>",
		Short: "Manage Bootwright contexts",
	}
	cmd.AddCommand(
		newContextInitCmd(stdin, stdout, stderr),
		newContextUpdateCmd(stdin, stdout, stderr),
		newContextUseCmd(stdout),
		newContextListCmd(stdout),
		newContextCurrentCmd(stdout),
		newContextDeleteCmd(stdin, stdout, stderr),
	)
	requireSubcommand(cmd)
	return cmd
}

func newContextInitCmd(stdin io.Reader, stdout io.Writer, stderr io.Writer) *cobra.Command {
	var name string
	var files []string
	var yes bool
	cmd := &cobra.Command{
		Use:   "init --name <ctx-name> -f <dir>",
		Short: "Create a context by copying a source directory into it",
		Long: `Create a context that owns a copy of a source directory.

The contents of the source directory are copied into the context, so the
context is self-contained: it keeps working even if the source is later moved
or deleted, and editing the source has no effect until ` + "`context update`" + `. Init
fails if the context already exists; rerun with --yes to drop the existing
context entirely and recreate it from the source.`,
		Args: cobra.NoArgs,
		Example: `  bootwright context init --name lab -f ./examples/sno-libvirt-redfish
  bootwright context init --name lab -f ~/lab-input --yes`,
	}
	cmd.Flags().StringVar(&name, "name", "", "context name (required)")
	cmd.Flags().StringArrayVarP(&files, "file", "f", nil, "source directory with Bootwright YAML; its contents are copied into the context (required)")
	cmd.Flags().BoolVar(&yes, "yes", false, "drop an existing context and recreate it from the source directory")
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		if err := workspace.ValidateName(name); err != nil {
			return failErr(2, err)
		}
		if len(files) != 1 {
			return failf(2, "context init copies exactly one source directory; pass a single -f <dir>")
		}
		// Resolve before any sudo re-exec so relative paths and ~ expand
		// against the caller's environment, not root's.
		source, err := workspace.ResolveWorkspaceDir(files[0])
		if err != nil {
			return failErr(2, err)
		}
		if shouldRunContextRootChild() {
			rootArgs := []string{"context", "init", "--name", name, "-f", source}
			if yes {
				rootArgs = append(rootArgs, "--yes")
			}
			code, err := runWithLocalRoot(cmd.Context(), rootArgs, stdin, stdout, stderr, true)
			if err != nil {
				return failErr(1, err)
			}
			if code != 0 {
				return silentExit(code)
			}
			return nil
		}
		ctx, err := workspace.NewContext(name)
		if err != nil {
			return failErr(2, err)
		}
		registry, store, err := workspace.LoadDefaultStore()
		if err != nil {
			return failErr(1, err)
		}
		exists, err := workspace.ContextExists(name)
		if err != nil {
			return failErr(1, err)
		}
		if exists && !yes {
			return failf(1, "context %q already exists; rerun with --yes to drop it and recreate it from %s", name, source)
		}
		// Validate the source before any destructive step so an invalid -f never
		// drops an existing context.
		state, err := desiredstate.LoadNormalizeValidateInputFiles([]string{source})
		if err != nil {
			return failErr(1, fmt.Errorf("validate source input files: %w", err))
		}
		if err := enforceControllerLocality(state); err != nil {
			return failErr(1, err)
		}
		if exists && yes {
			if err := workspace.SafePurgeBaseDir(ctx); err != nil {
				return failErr(1, err)
			}
		}
		if err := workspace.EnsureDirs(ctx); err != nil {
			return failErr(1, err)
		}
		if err := workspace.ReplaceInputDir(ctx, source); err != nil {
			return failErr(1, err)
		}
		bundle, bundleSkipped, err := prepareInitialBundle()
		if err != nil {
			return failErr(1, err)
		}
		store.Current = name
		if err := workspace.Save(registry, store); err != nil {
			return failErr(1, err)
		}
		p := output.New(stdout)
		p.Command("context init")
		p.Section("Context")
		p.Status(output.StatusOK, name, "created at "+ctx.BaseDir)
		p.Status(output.StatusOK, "input", "copied from "+source)
		p.Status(output.StatusOK, "input dir", ctx.InputDir)
		printBundleStatus(p, bundle, bundleSkipped)
		p.Summary(output.StatusOK, "context "+name, "initialized and set as the current context")
		printNextStatusHint(stdout)
		return nil
	}
	return cmd
}

// printBundleStatus reports how the embedded Ansible bundle was prepared during
// context init: skipped (build without an embedded bundle), reused from cache,
// or freshly extracted.
func printBundleStatus(p *output.Printer, result bundle.AnsibleBundleResult, skipped bool) {
	switch {
	case skipped:
		p.Status(output.StatusSkip, "Ansible bundle", "embedded bundle not synced in this build")
	case result.Reused:
		p.Status(output.StatusOK, "Ansible bundle", "cache current at "+result.Dir)
	default:
		p.Status(output.StatusOK, "Ansible bundle", fmt.Sprintf("extracted %d file(s)", result.Files))
	}
}

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
	cmd.Flags().StringArrayVarP(&files, "file", "f", nil, "source directory whose contents replace the context input (required)")
	cmd.Flags().BoolVar(&yes, "yes", false, "replace the input without the interactive confirmation prompt")
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		if err := workspace.ValidateName(name); err != nil {
			return failErr(2, err)
		}
		if len(files) != 1 {
			return failf(2, "context update copies exactly one source directory; pass a single -f <dir>")
		}
		// Resolve before any sudo re-exec so relative paths and ~ expand
		// against the caller's environment, not root's.
		source, err := workspace.ResolveWorkspaceDir(files[0])
		if err != nil {
			return failErr(2, err)
		}
		if shouldRunContextRootChild() {
			// Confirm here, as the calling user, before re-exec under root so the
			// prompt reaches a real terminal; the child runs with --yes.
			if !yes && !confirm(stdin, stdout, contextUpdateConfirmPrompt(name, source)) {
				return failErr(1, errors.New("context update aborted"))
			}
			// update keeps the current pointer, so the registry is not synced back.
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
		// Validate the source before prompting so a bad -f fails fast instead of
		// asking the user to confirm a replacement that cannot happen.
		if !yes && !confirm(stdin, stdout, contextUpdateConfirmPrompt(ctx.Name, source)) {
			return failErr(1, errors.New("context update aborted"))
		}
		if err := workspace.EnsureDirs(ctx); err != nil {
			return failErr(1, err)
		}
		if err := workspace.ReplaceInputDir(ctx, source); err != nil {
			return failErr(1, err)
		}
		p := output.New(stdout)
		p.Command("context update")
		p.Section("Context")
		p.Status(output.StatusOK, ctx.Name, "input replaced")
		p.Status(output.StatusOK, "input", "copied from "+source)
		p.Status(output.StatusOK, "input dir", ctx.InputDir)
		p.Status(output.StatusOK, "state", "preserved (secrets, runs, rendered, clusters, ownership)")
		p.Summary(output.StatusOK, "context "+ctx.Name, "input replaced from "+source)
		printNextStatusHint(stdout)
		return nil
	}
	return cmd
}

// contextUpdateConfirmPrompt is the interactive confirmation shown before
// update discards a context's current input and replaces it from source.
func contextUpdateConfirmPrompt(name, source string) string {
	return fmt.Sprintf("Replace the input of context %q from %s? The current input is discarded (secrets, runs, rendered output, and clusters are preserved). [y/N] (default: no): ", name, source)
}

func newContextUseCmd(stdout io.Writer) *cobra.Command {
	var name string
	cmd := &cobra.Command{
		Use:     "use --name <ctx-name>",
		Short:   "Set the current context",
		Args:    cobra.NoArgs,
		Example: `  bootwright context use --name lab`,
	}
	cmd.Flags().StringVar(&name, "name", "", "context name (required)")
	cmd.RunE = func(_ *cobra.Command, _ []string) error {
		if err := workspace.ValidateName(name); err != nil {
			return failErr(2, err)
		}
		registry, store, err := workspace.LoadDefaultStore()
		if err != nil {
			return failErr(1, err)
		}
		if _, err := workspace.RequireExistingContext(name); err != nil {
			return failErr(1, err)
		}
		store.Current = name
		if err := workspace.Save(registry, store); err != nil {
			return failErr(1, err)
		}
		p := output.New(stdout)
		p.Command("context use")
		p.Summary(output.StatusOK, "current context", name)
		return nil
	}
	return cmd
}

func newContextListCmd(stdout io.Writer) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List contexts",
		Args:  cobra.NoArgs,
	}
	cmd.RunE = func(_ *cobra.Command, _ []string) error {
		_, store, err := workspace.LoadDefaultStore()
		if err != nil {
			return failErr(1, err)
		}
		contexts, err := workspace.ListContexts()
		if err != nil {
			return failErr(1, err)
		}
		var items []output.Item
		foundCurrent := false
		current := strings.TrimSpace(store.Current)
		for _, ctx := range contexts {
			label := ctx.Name
			if ctx.Name == current {
				label = "* " + label
				foundCurrent = true
			} else {
				label = "  " + label
			}
			items = append(items, output.Item{Label: label, Detail: ctx.BaseDir})
		}
		p := output.New(stdout)
		p.Command("context list")
		if len(items) == 0 {
			p.Status(output.StatusWarn, "contexts", "none")
		} else {
			p.List(items)
		}
		if current != "" && !foundCurrent {
			contextsDir, dirErr := workspace.ContextsDir()
			if dirErr != nil {
				return failErr(1, dirErr)
			}
			p.Status(output.StatusWarn, "current context", current+" not found under "+contextsDir)
		}
		p.Summary(output.StatusOK, "contexts", fmt.Sprintf("%d registered", len(items)))
		return nil
	}
	return cmd
}

func newContextCurrentCmd(stdout io.Writer) *cobra.Command {
	var short bool
	cmd := &cobra.Command{
		Use:   "current",
		Short: "Show the current context",
		Args:  cobra.NoArgs,
	}
	cmd.Flags().BoolVar(&short, "short", false, "print only the context name")
	cmd.RunE = func(_ *cobra.Command, _ []string) error {
		ctx, err := workspace.CurrentContext()
		if err != nil {
			return failErr(1, err)
		}
		if short {
			fmt.Fprintln(stdout, ctx.Name)
			return nil
		}
		p := output.New(stdout)
		p.Command("context current")
		p.Fields(contextFields(ctx))
		p.Summary(output.StatusOK, ctx.Name, "current context")
		return nil
	}
	return cmd
}

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
