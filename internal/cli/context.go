package cli

import (
	"errors"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/crmarques/bootwright/internal/cli/output"
	"github.com/crmarques/bootwright/internal/contextstore"
	"github.com/crmarques/bootwright/internal/desiredstate"
)

const (
	bootwrightContextEnv  = "BOOTWRIGHT_CONTEXT"
	bootwrightInputDirEnv = "BOOTWRIGHT_INPUT_DIR"
)

func newContextCmd(stdin io.Reader, stdout io.Writer) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "context <command>",
		Short: "Manage Bootwright contexts",
	}
	cmd.AddCommand(
		newContextInitCmd(stdout),
		newContextUseCmd(stdout),
		newContextListCmd(stdout),
		newContextCurrentCmd(stdout),
		newContextDeleteCmd(stdin, stdout),
		newContextValidateCmd(stdout),
	)
	requireSubcommand(cmd)
	showSubcommandFlagsInHelp(cmd)
	return cmd
}

func newContextInitCmd(stdout io.Writer) *cobra.Command {
	var files []string
	var baseDir string
	var yes bool
	cmd := &cobra.Command{
		Use:   "init <ctx-name>",
		Short: "Create a context from desired-state input files",
		Args:  cobra.ExactArgs(1),
		Example: `  bootwright context init lab -f ./test/e2e/001-sno-libvirt
  bootwright context init lab -f ./input --base-dir /srv/bootwright/lab --yes`,
	}
	cmd.Flags().StringArrayVarP(&files, "file", "f", nil, "Bootwright YAML file or directory to import; may be repeated")
	cmd.Flags().StringVar(&baseDir, "base-dir", "", "context base directory (default: ~/bootwright/<ctx-name>)")
	cmd.Flags().BoolVar(&yes, "yes", false, "replace imported input files and update an existing context")
	cmd.RunE = func(_ *cobra.Command, args []string) error {
		name := args[0]
		ctx, err := contextstore.NewContext(name, baseDir)
		if err != nil {
			return failErr(2, err)
		}
		registry, store, err := loadContextStore()
		if err != nil {
			return failErr(1, err)
		}
		if _, exists := store.Contexts[name]; exists && !yes {
			return failf(1, "context %q already exists; rerun with --yes to update it", name)
		}
		copied, err := contextstore.ImportInputs(files, ctx.InputDir, yes)
		if err != nil {
			return failErr(1, err)
		}
		if _, err := desiredstate.LoadNormalizeValidate(ctx.InputPaths); err != nil {
			return failErr(1, fmt.Errorf("validate imported input files: %w", err))
		}
		if err := contextstore.EnsureDirs(ctx); err != nil {
			return failErr(1, err)
		}
		bundle, bundleSkipped, err := prepareInitialBundle(ctx.StateDir)
		if err != nil {
			return failErr(1, err)
		}
		store.Contexts[name] = ctx
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
		if _, ok := store.Contexts[name]; !ok {
			return failf(1, "context %q not found", name)
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
		var items []output.Item
		for _, name := range contextstore.Names(store) {
			label := name
			if name == store.Current {
				label = "* " + label
			} else {
				label = "  " + label
			}
			items = append(items, output.Item{Label: label, Detail: store.Contexts[name].BaseDir})
		}
		p := output.New(stdout)
		p.Command("context list")
		if len(items) == 0 {
			p.Status(output.StatusWarn, "contexts", "none")
			return nil
		}
		p.List(items)
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

func newContextDeleteCmd(stdin io.Reader, stdout io.Writer) *cobra.Command {
	var purge bool
	var yes bool
	cmd := &cobra.Command{
		Use:   "delete <ctx-name>",
		Short: "Delete a context",
		Args:  cobra.ExactArgs(1),
	}
	cmd.Flags().BoolVar(&purge, "purge", false, "also delete the context base directory")
	cmd.Flags().BoolVar(&yes, "yes", false, "skip confirmation when purging context files")
	cmd.RunE = func(_ *cobra.Command, args []string) error {
		name := args[0]
		if err := contextstore.ValidateName(name); err != nil {
			return failErr(2, err)
		}
		registry, store, err := loadContextStore()
		if err != nil {
			return failErr(1, err)
		}
		ctx, ok := store.Contexts[name]
		if !ok {
			return failf(1, "context %q not found", name)
		}
		ctx.Name = name
		if purge && !yes && !confirm(stdin, stdout, fmt.Sprintf("Delete %s and all files under %s? [y/N] (default: no): ", name, ctx.BaseDir)) {
			return failErr(1, errors.New("context delete aborted"))
		}
		delete(store.Contexts, name)
		if store.Current == name {
			store.Current = ""
		}
		if err := contextstore.Save(registry, store); err != nil {
			return failErr(1, err)
		}
		if purge {
			if err := contextstore.SafePurgeBaseDir(ctx); err != nil {
				return failErr(1, err)
			}
		}
		detail := "registry entry removed"
		if purge {
			detail = "registry entry and base directory removed"
		}
		output.New(stdout).Status(output.StatusOK, "context "+name, detail)
		return nil
	}
	return cmd
}

func currentContext() (contextstore.Context, error) {
	_, store, err := loadContextStore()
	if err != nil {
		return contextstore.Context{}, err
	}
	return contextstore.Current(store)
}

func loadContextStore() (string, contextstore.Store, error) {
	registry, err := contextstore.DefaultRegistryPath()
	if err != nil {
		return "", contextstore.Store{}, err
	}
	store, err := contextstore.Load(registry)
	if err != nil {
		return "", contextstore.Store{}, err
	}
	return registry, store, nil
}

func contextFields(ctx contextstore.Context) []output.Field {
	return []output.Field{
		{Key: "name", Value: ctx.Name},
		{Key: "base-dir", Value: ctx.BaseDir},
		{Key: "input-dir", Value: ctx.InputDir},
		{Key: "state-dir", Value: ctx.StateDir},
		{Key: "secrets-dir", Value: ctx.SecretsDir},
	}
}
