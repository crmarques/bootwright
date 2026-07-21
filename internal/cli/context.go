package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/crmarques/bootwright/internal/cli/output"
	"github.com/crmarques/bootwright/internal/workspace"
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

func newContextUseCmd(stdout io.Writer) *cobra.Command {
	var name string
	cmd := &cobra.Command{
		Use:     "use --name <ctx-name>",
		Short:   "Set the current context",
		Args:    cobra.NoArgs,
		Example: `  bootwright context use --name lab`,
	}
	cmd.Flags().StringVar(&name, "name", "", "context name (required)")
	registerContextNameCompletion(cmd, "name")
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
