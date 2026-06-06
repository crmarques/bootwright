package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/crmarques/bootwright/internal/cli/output"
	"github.com/crmarques/bootwright/internal/runtime/context"
	"github.com/crmarques/bootwright/internal/state/desired"
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
		newContextValidateCmd(stdout),
	)
	requireSubcommand(cmd)
	return cmd
}

func newContextInitCmd(stdin io.Reader, stdout io.Writer, stderr io.Writer) *cobra.Command {
	var files []string
	var yes bool
	cmd := &cobra.Command{
		Use:   "init <ctx-name>",
		Short: "Create a context from desired-state input files",
		Args:  cobra.ExactArgs(1),
		Example: `  bootwright context init lab -f ./examples/sno-libvirt-redfish
  bootwright context init lab -f ./input --yes`,
	}
	cmd.Flags().StringArrayVarP(&files, "file", "f", nil, "Bootwright YAML file or directory to import; may be repeated")
	cmd.Flags().BoolVar(&yes, "yes", false, "replace an existing context directory")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		name := args[0]
		if shouldRunContextRootChild() {
			code, err := runContextImportWithLocalRoot(cmd.Context(), []string{"context", "init", name}, files, yes, stdin, stdout, stderr)
			if err != nil {
				return failErr(1, err)
			}
			if code != 0 {
				return silentExit(code)
			}
			return nil
		}
		ctx, err := contextstore.NewContext(name)
		if err != nil {
			return failErr(2, err)
		}
		registry, store, err := loadContextStore()
		if err != nil {
			return failErr(1, err)
		}
		exists, err := contextstore.ContextExists(name)
		if err != nil {
			return failErr(1, err)
		}
		if exists && !yes {
			return failf(1, "context %q already exists; rerun with --yes to replace it or `bootwright context update %s -f <path> --yes` to refresh inputs", name, name)
		}
		prepared, err := contextstore.PrepareContextImport(name, files)
		if err != nil {
			return failErr(1, err)
		}
		defer prepared.Cancel()
		state, err := desiredstate.LoadNormalizeValidateInputFiles(prepared.ValidationPaths())
		if err != nil {
			return failErr(1, fmt.Errorf("validate imported input files: %w", err))
		}
		if err := enforceControllerLocality(state); err != nil {
			return failErr(1, err)
		}
		copied, err := prepared.Commit(yes)
		if err != nil {
			return failErr(1, err)
		}
		bundle, bundleSkipped, err := prepareInitialBundle()
		if err != nil {
			return failErr(1, err)
		}
		store.Current = name
		if err := contextstore.Save(registry, store); err != nil {
			return failErr(1, err)
		}
		p := output.New(stdout)
		p.Command("context init")
		p.Section("Context")
		p.Fields(contextFields(ctx))
		p.Section("Imported inputs")
		p.Artifacts([]output.ArtifactGroup{{Paths: copied}})
		p.Section("Runtime")
		if bundleSkipped {
			p.Status(output.StatusSkip, "Ansible bundle", "embedded bundle not synced in this build")
		} else if bundle.Reused {
			p.Status(output.StatusOK, "Ansible bundle", "cache current at "+bundle.Dir)
		} else {
			p.Status(output.StatusOK, "Ansible bundle", fmt.Sprintf("extracted %d file(s) to %s", bundle.Files, bundle.Dir))
		}
		p.Summary(output.StatusOK, name, "current context")
		printNextStatusHint(stdout)
		return nil
	}
	return cmd
}

func newContextUpdateCmd(stdin io.Reader, stdout io.Writer, stderr io.Writer) *cobra.Command {
	var files []string
	var yes bool
	cmd := &cobra.Command{
		Use:   "update <ctx-name>",
		Short: "Update desired-state input files for an existing context",
		Args:  cobra.ExactArgs(1),
		Example: `  bootwright context update lab -f ./input --yes
  bootwright context update lab -f ./environment.yaml -f ./service-machines.yaml`,
	}
	cmd.Flags().StringArrayVarP(&files, "file", "f", nil, "Bootwright YAML file or directory to import; may be repeated")
	cmd.Flags().BoolVar(&yes, "yes", false, "skip the update confirmation prompt")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		name := args[0]
		if shouldRunContextRootChild() {
			code, err := runContextImportWithLocalRoot(cmd.Context(), []string{"context", "update", name}, files, yes, stdin, stdout, stderr)
			if err != nil {
				return failErr(1, err)
			}
			if code != 0 {
				return silentExit(code)
			}
			return nil
		}
		if err := contextstore.ValidateName(name); err != nil {
			return failErr(2, err)
		}
		ctx, err := contextstore.RequireExistingContext(name)
		if err != nil {
			return failErr(1, err)
		}
		info, err := os.Stat(ctx.BaseDir)
		if err != nil {
			return failErr(1, fmt.Errorf("context %q directory is not ready at %s: %w", name, ctx.BaseDir, err))
		}
		if !info.IsDir() {
			return failErr(1, fmt.Errorf("context %q path is not a directory: %s", name, ctx.BaseDir))
		}
		prepared, err := contextstore.PrepareInputImport(files, ctx.InputDir, true)
		if err != nil {
			return failErr(1, err)
		}
		defer prepared.Cancel()
		state, err := desiredstate.LoadNormalizeValidateInputFiles(prepared.ValidationPaths())
		if err != nil {
			return failErr(1, fmt.Errorf("validate imported input files: %w", err))
		}
		if err := enforceControllerLocality(state); err != nil {
			return failErr(1, err)
		}
		if !yes && !confirm(stdin, stdout, fmt.Sprintf("Replace input files for context %s under %s? [y/N] (default: no): ", name, ctx.InputDir)) {
			return failErr(1, errors.New("context update aborted"))
		}
		if err := contextstore.EnsureDirs(ctx); err != nil {
			return failErr(1, err)
		}
		copied, err := prepared.Commit()
		if err != nil {
			return failErr(1, err)
		}
		bundle, bundleSkipped, err := prepareInitialBundle()
		if err != nil {
			return failErr(1, err)
		}
		p := output.New(stdout)
		p.Command("context update")
		p.Section("Context")
		p.Fields(contextFields(ctx))
		p.Section("Imported inputs")
		p.Artifacts([]output.ArtifactGroup{{Paths: copied}})
		p.Section("Runtime")
		if bundleSkipped {
			p.Status(output.StatusSkip, "Ansible bundle", "embedded bundle not synced in this build")
		} else if bundle.Reused {
			p.Status(output.StatusOK, "Ansible bundle", "cache current at "+bundle.Dir)
		} else {
			p.Status(output.StatusOK, "Ansible bundle", fmt.Sprintf("extracted %d file(s) to %s", bundle.Files, bundle.Dir))
		}
		p.Summary(output.StatusOK, name, "context input files updated")
		printNextStatusHint(stdout)
		return nil
	}
	return cmd
}

func newContextUseCmd(stdout io.Writer) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "use <ctx-name>",
		Short: "Set the current context",
		Args:  cobra.ExactArgs(1),
	}
	cmd.RunE = func(_ *cobra.Command, args []string) error {
		name := args[0]
		if err := contextstore.ValidateName(name); err != nil {
			return failErr(2, err)
		}
		registry, store, err := loadContextStore()
		if err != nil {
			return failErr(1, err)
		}
		if _, err := contextstore.RequireExistingContext(name); err != nil {
			return failErr(1, err)
		}
		store.Current = name
		if err := contextstore.Save(registry, store); err != nil {
			return failErr(1, err)
		}
		output.New(stdout).Status(output.StatusOK, "current context", name)
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
		_, store, err := loadContextStore()
		if err != nil {
			return failErr(1, err)
		}
		contexts, err := contextstore.ListContexts()
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
			contextsDir, dirErr := contextstore.ContextsDir()
			if dirErr != nil {
				return failErr(1, dirErr)
			}
			p.Status(output.StatusWarn, "current context", current+" not found under "+contextsDir)
		}
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
		ctx, err := currentContext()
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
		return nil
	}
	return cmd
}

func newContextDeleteCmd(stdin io.Reader, stdout io.Writer, stderr io.Writer) *cobra.Command {
	var purge bool
	var yes bool
	cmd := &cobra.Command{
		Use:   "delete <ctx-name>",
		Short: "Delete a context",
		Args:  cobra.ExactArgs(1),
	}
	cmd.Flags().BoolVar(&purge, "purge", false, "also delete the context base directory")
	cmd.Flags().BoolVar(&yes, "yes", false, "skip confirmation when purging context files")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		name := args[0]
		if err := contextstore.ValidateName(name); err != nil {
			return failErr(2, err)
		}
		if !purge {
			return failf(1, "context %q is stored in shared root state; rerun with --purge --yes to delete it", name)
		}
		if purge && shouldRunContextRootChild() {
			ctx, ctxErr := contextstore.NewContext(name)
			if ctxErr != nil {
				return failErr(2, ctxErr)
			}
			if !yes && !confirm(stdin, stdout, fmt.Sprintf("Delete %s and all files under %s? [y/N] (default: no): ", name, ctx.BaseDir)) {
				return failErr(1, errors.New("context delete aborted"))
			}
			code, err := runWithLocalRoot(cmd.Context(), []string{"context", "delete", name, "--purge", "--yes"}, stdin, stdout, stderr, true)
			if err != nil {
				return failErr(1, err)
			}
			if code != 0 {
				return silentExit(code)
			}
			return nil
		}
		registry, store, err := loadContextStore()
		if err != nil {
			return failErr(1, err)
		}
		ctx, err := contextstore.RequireExistingContext(name)
		if err != nil {
			return failErr(1, err)
		}
		if !yes && !confirm(stdin, stdout, fmt.Sprintf("Delete %s and all files under %s? [y/N] (default: no): ", name, ctx.BaseDir)) {
			return failErr(1, errors.New("context delete aborted"))
		}
		if err := contextstore.SafePurgeBaseDir(ctx); err != nil {
			return failErr(1, err)
		}
		if strings.TrimSpace(store.Current) == name {
			store.Current = ""
			if err := contextstore.Save(registry, store); err != nil {
				return failErr(1, err)
			}
		}
		output.New(stdout).Status(output.StatusOK, "context "+name, "base directory removed")
		return nil
	}
	return cmd
}
