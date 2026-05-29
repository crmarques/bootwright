package cli

import (
	"fmt"
	"io"

	"github.com/crmarques/bootwright/internal/runtime/context"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// preserves AddCommand order in --help so workflow commands render in usage order
func init() { cobra.EnableCommandSorting = false }

const (
	groupWorkflow = "workflow"
	groupGeneral  = "general"
)

func newRootCmd(stdin io.Reader, stdout io.Writer, stderr io.Writer) *cobra.Command {
	root := &cobra.Command{
		Use:   "bootwright",
		Short: "Desired-state OpenShift fleet provisioning",
		Long: "Bootwright renders, validates, and converges versioned desired-state YAML\n" +
			"to drive OpenShift cluster lifecycle.",
		Example: `  bootwright example init lab --output ./lab-input
  bootwright context init lab -f ./lab-input
  bootwright context validate
  bootwright secret set openshift-pull-secret --pull-secret ~/openshift-pull-secret.json
  bootwright secret generate
  bootwright secret materialize
  bootwright check syntax
  bootwright check bastion
  bootwright apply bastion --yes
  bootwright check infra
  bootwright apply infra --dry-run
  bootwright check cluster
  bootwright apply cluster --dry-run
  bootwright status --watch`,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.SetFlagErrorFunc(func(_ *cobra.Command, err error) error {
		return failErr(2, err)
	})
	root.SetIn(stdin)
	root.SetOut(stdout)
	root.SetErr(stderr)

	root.AddGroup(
		&cobra.Group{ID: groupWorkflow, Title: "Workflow Commands:"},
		&cobra.Group{ID: groupGeneral, Title: "General Commands:"},
	)
	root.SetHelpCommandGroupID(groupGeneral)
	root.SetCompletionCommandGroupID(groupGeneral)

	addWorkflow(root,
		newContextCmd(stdin, stdout, stderr),
		newExampleCmd(stdout),
		newPrintEnvCmd(stdout),
		newSecretCmd(stdin, stdout, stderr),
		newCheckCmd(stdout, stderr),
		newStatusCmd(stdout),
		newRenderCmd(stdout, stderr),
		newApplyCmd(stdin, stdout, stderr),
		newDestroyCmd(stdin, stdout, stderr),
		newVersionCmd(stdout),
	)
	return root
}

func addWorkflow(parent *cobra.Command, cmds ...*cobra.Command) {
	for _, c := range cmds {
		c.GroupID = groupWorkflow
		parent.AddCommand(c)
	}
}

type commonFlags struct {
	ctx contextstore.Context
}

func addCommonFlags() *commonFlags {
	return &commonFlags{}
}

func (cf *commonFlags) resolve() (contextstore.Context, error) {
	return cf.resolveWithLocality(true)
}

func (cf *commonFlags) resolveLocalOnly() (contextstore.Context, error) {
	return cf.resolveWithLocality(false)
}

func (cf *commonFlags) resolveWithLocality(checkLocality bool) (contextstore.Context, error) {
	if cf.ctx.Name != "" {
		return cf.ctx, nil
	}
	ctx, err := currentContext()
	if err != nil {
		return contextstore.Context{}, err
	}
	if err := ensureContextReady(ctx); err != nil {
		return contextstore.Context{}, err
	}
	if checkLocality {
		if err := enforceContextLocality(ctx); err != nil {
			return contextstore.Context{}, err
		}
	}
	cf.ctx = ctx
	return ctx, nil
}

// requireSubcommand configures cmd as a pure dispatcher whose only valid
// positionals are the names of its already-registered subcommands. Wiring:
//
//   - FParseErrWhitelist.UnknownFlags lets the args validator run even when
//     the user typed a subcommand-shaped typo followed by a child's flag
//     (e.g. `destroy cluster -f X --yes`). Without it, pflag bails on the
//     unknown `-f` before Cobra ever sees `cluster`.
//   - ValidArgs is derived from the subcommands so OnlyValidArgs can emit
//     Cobra's built-in "Did you mean ...?" suggestion.
//   - RunE = Help is needed because Cobra skips ValidateArgs on commands
//     with no Run* defined, falling through to a silent help dump instead.
//
// Call after AddCommand so c.Commands() is populated.
func requireSubcommand(cmd *cobra.Command) {
	cmd.FParseErrWhitelist = cobra.FParseErrWhitelist{UnknownFlags: true}
	names := make([]string, 0, len(cmd.Commands()))
	for _, sub := range cmd.Commands() {
		names = append(names, sub.Name())
	}
	cmd.ValidArgs = names
	cmd.Args = cobra.OnlyValidArgs
	cmd.RunE = func(c *cobra.Command, _ []string) error { return c.Help() }
}

func showSubcommandFlagsInHelp(cmd *cobra.Command) {
	defaultHelp := cmd.HelpFunc()
	cmd.SetHelpFunc(func(c *cobra.Command, args []string) {
		defaultHelp(c, args)
		if c != cmd {
			return
		}
		merged := pflag.NewFlagSet("subcommand", pflag.ContinueOnError)
		for _, sub := range c.Commands() {
			if !sub.IsAvailableCommand() {
				continue
			}
			sub.LocalFlags().VisitAll(func(f *pflag.Flag) {
				if f.Name == "help" || c.LocalFlags().Lookup(f.Name) != nil || merged.Lookup(f.Name) != nil {
					return
				}
				merged.AddFlag(f)
			})
		}
		usages := merged.FlagUsages()
		if usages == "" {
			return
		}
		fmt.Fprintf(c.OutOrStdout(), "\nSubcommand Flags:\n%s", usages)
	})
}
