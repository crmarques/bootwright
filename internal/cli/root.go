package cli

import (
	"errors"
	"fmt"
	"io"

	"github.com/crmarques/bootwright/internal/workspace"
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
		newPreflightCmd(stdin, stdout, stderr),
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
	ctx workspace.Context
}

func addCommonFlags() *commonFlags {
	return &commonFlags{}
}

func (cf *commonFlags) resolve() (workspace.Context, error) {
	return cf.resolveWithLocality(true)
}

func (cf *commonFlags) resolveLocalOnly() (workspace.Context, error) {
	return cf.resolveWithLocality(false)
}

func (cf *commonFlags) resolveWithLocality(checkLocality bool) (workspace.Context, error) {
	if cf.ctx.Name != "" {
		return cf.ctx, nil
	}
	ctx, err := workspace.CurrentContext()
	if err != nil {
		return workspace.Context{}, err
	}
	if err := ensureContextReady(ctx); err != nil {
		return workspace.Context{}, err
	}
	if checkLocality {
		if err := enforceContextLocality(ctx); err != nil {
			return workspace.Context{}, err
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
//   - Args rejects any positional that is not a registered subcommand and
//     reproduces Cobra's "Did you mean ...?" suggestion. It deliberately does
//     NOT set ValidArgs: a ValidArgs slice mirroring the subcommand names makes
//     shell completion list every subcommand twice — once described from the
//     subcommand walk and once bare from ValidArgs (see Cobra's getCompletions)
//     — so the dispatcher leaves completion to the subcommand walk alone.
//   - RunE = Help is needed because Cobra skips ValidateArgs on commands
//     with no Run* defined, falling through to a silent help dump instead.
//
// Call after AddCommand so c.Commands() is populated.
func requireSubcommand(cmd *cobra.Command) {
	cmd.FParseErrWhitelist = cobra.FParseErrWhitelist{UnknownFlags: true}
	cmd.Args = rejectUnknownSubcommandArgs
	cmd.RunE = func(c *cobra.Command, _ []string) error { return c.Help() }
}

// rejectUnknownSubcommandArgs is a cobra.PositionalArgs for dispatcher commands:
// no positional is fine (the command then prints help), and the first positional
// that did not match a subcommand is rejected with Cobra's own wording. Cobra
// only invokes a parent's Args when no child matched, so the rejected positional
// is always a genuine typo, never a valid subcommand.
func rejectUnknownSubcommandArgs(c *cobra.Command, args []string) error {
	if len(args) == 0 {
		return nil
	}
	return unknownSubcommandError(c, args[0])
}

// unknownSubcommandError formats the message cobra.OnlyValidArgs produces for an
// unknown positional, including the subcommand-derived "Did you mean this?"
// suggestion, so callers get that UX without populating ValidArgs (which would
// pollute shell completion with description-less duplicate entries).
func unknownSubcommandError(c *cobra.Command, arg string) error {
	msg := fmt.Sprintf("invalid argument %q for %q", arg, c.CommandPath())
	if !c.DisableSuggestions {
		if c.SuggestionsMinimumDistance <= 0 {
			c.SuggestionsMinimumDistance = 2
		}
		if suggestions := c.SuggestionsFor(arg); len(suggestions) > 0 {
			msg += "\n\nDid you mean this?\n"
			for _, s := range suggestions {
				msg += fmt.Sprintf("\t%v\n", s)
			}
		}
	}
	return errors.New(msg)
}
