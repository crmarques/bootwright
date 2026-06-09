package cli

import (
	"io"

	"github.com/crmarques/bootwright/internal/runtime/context"
	"github.com/spf13/cobra"
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
  bootwright validate -f ./lab-input
  bootwright context init lab -f ./lab-input
  bootwright secret set openshift-pull-secret --pull-secret ~/openshift-pull-secret.json
  bootwright secret sync
  bootwright host trust
  bootwright bastion setup --yes
  bootwright preflight all
  bootwright render effective
  bootwright plan
  bootwright apply --yes
  bootwright status --watch
  bootwright cluster access`,
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
		newValidateCmd(stdout),
		newContextCmd(stdin, stdout, stderr),
		newHostCmd(stdin, stdout, stderr),
		newBastionCmd(stdin, stdout, stderr),
		newClusterCmd(stdout),
		newExampleCmd(stdout),
		newPrintEnvCmd(stdout),
		newMediaCmd(stdin, stdout),
		newSecretCmd(stdin, stdout, stderr),
		newPreflightCmd(stdout, stderr),
		newStatusCmd(stdout),
		newStateCheckCmd(stdout),
		newPlanCmd(stdin, stdout, stderr),
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
//     (e.g. `cluster acces --cluster X`). Without it, pflag bails on the
//     unknown `--cluster` before Cobra ever sees `acces`.
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
