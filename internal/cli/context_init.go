package cli

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/crmarques/bootwright/internal/addons/nativecatalog"
	"github.com/crmarques/bootwright/internal/cli/output"
	"github.com/crmarques/bootwright/internal/converge/bundle"
	"github.com/crmarques/bootwright/internal/state/desired"
	"github.com/crmarques/bootwright/internal/workspace"
)

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
		if err := workspace.ReplaceInputDirWithAddons(ctx, source, nativecatalog.ReferencedStoreAddons(state)); err != nil {
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
