package cli

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/crmarques/bootwright/internal/converge"
	"github.com/crmarques/bootwright/internal/workspace"
	"github.com/spf13/cobra"
)

func init() { cobra.EnableCommandSorting = false }

const (
	groupSetup     = "setup"
	groupResource  = "resource"
	groupInspect   = "inspect"
	groupLifecycle = "lifecycle"
	groupGeneral   = "general"
)

var (
	contextOverride       string
	sshIDFile             string
	sshUserOverride       string
	sshAskSudoPassword    bool
	sshUserForProvisioned bool
)

func newRootCmd(stdin io.Reader, stdout io.Writer, stderr io.Writer) *cobra.Command {
	root := &cobra.Command{
		Use:   "bootwright",
		Short: "Desired-state OpenShift, OKD, and Ceph fleet provisioning",
		Long: "Bootwright renders, validates, and converges versioned desired-state YAML\n" +
			"to drive OpenShift, OKD, and Ceph cluster lifecycle.",
		Example: `  bootwright example init --name lab --output-dir ./lab-input
  bootwright validate -f ./lab-input
  bootwright context init --name lab -f ./lab-input
  bootwright secret set --name openshift-pull-secret --pull-secret ~/openshift-pull-secret.json
  bootwright secret generate
  bootwright machine trust
  bootwright bastion setup --yes
  bootwright preflight all
  bootwright render effective
  bootwright plan
  bootwright apply --yes
  bootwright status --watch
  bootwright cluster info`,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.SetFlagErrorFunc(func(_ *cobra.Command, err error) error {
		return failErr(2, err)
	})
	root.SetIn(stdin)
	root.SetOut(stdout)
	root.SetErr(stderr)
	root.PersistentFlags().StringVar(&contextOverride, "context", "", flagContextUsage)
	registerContextNameCompletion(root, "context")
	root.PersistentFlags().StringVar(&sshIDFile, "ssh-id-file", "", flagSSHIDFileUsage)
	root.PersistentFlags().StringVar(&sshUserOverride, "ssh-user", "", flagSSHUserUsage)
	registerFlagCompletion(root, "ssh-user", nil)
	root.PersistentFlags().BoolVar(&sshAskSudoPassword, "ssh-ask-sudo-password", false, flagSSHAskSudoPasswordUsage)
	root.PersistentFlags().BoolVar(&sshUserForProvisioned, "ssh-user-for-provisioned", false, flagSSHUserForProvisionedUsage)
	root.PersistentPreRunE = func(cmd *cobra.Command, _ []string) error {
		path, err := resolveSSHIDFile()
		if err != nil {
			return failErr(2, err)
		}
		user, err := resolveSSHUser()
		if err != nil {
			return failErr(2, err)
		}
		password, err := resolveSSHSudoPassword(cmd.InOrStdin(), cmd.ErrOrStderr())
		if err != nil {
			return failErr(2, err)
		}
		converge.SetPreferredIdentityFile(path)
		converge.SetSSHUser(user)
		converge.SetSSHUserForProvisioned(sshUserForProvisioned)
		converge.SetSSHSudoPassword(password)
		return nil
	}

	root.AddGroup(
		&cobra.Group{ID: groupSetup, Title: "Setup Commands:"},
		&cobra.Group{ID: groupInspect, Title: "Inspect Commands:"},
		&cobra.Group{ID: groupLifecycle, Title: "Lifecycle Commands:"},
		&cobra.Group{ID: groupResource, Title: "Resource Commands:"},
		&cobra.Group{ID: groupGeneral, Title: "General Commands:"},
	)
	root.SetHelpCommandGroupID(groupGeneral)
	root.SetCompletionCommandGroupID(groupGeneral)

	addGroup(root, groupSetup,
		newContextCmd(stdin, stdout, stderr),
		newExampleCmd(stdout),
		newAddonsCatalogCmd(stdin, stdout),
		newSecretCmd(stdin, stdout, stderr),
		newMediaCmd(stdin, stdout),
	)
	addGroup(root, groupInspect,
		newValidateCmd(stdout),
		newPreflightCmd(stdin, stdout, stderr),
		newPlanCmd(stdin, stdout, stderr),
		newDiffCmd(stdout, stderr),
		newStatusCmd(stdout),
		newRenderCmd(stdout, stderr),
	)
	addGroup(root, groupLifecycle,
		newApplyCmd(stdin, stdout, stderr),
		newDestroyCmd(stdin, stdout, stderr),
	)
	addGroup(root, groupResource,
		newMachineCmd(stdin, stdout, stderr),
		newBastionCmd(stdin, stdout, stderr),
		newClusterCmd(stdout),
		newContainerClusterCmd(stdout),
		newStorageClusterCmd(stdin, stdout, stderr),
	)
	addGroup(root, groupGeneral,
		newVersionCmd(stdout),
	)
	return root
}

func addGroup(parent *cobra.Command, group string, cmds ...*cobra.Command) {
	for _, c := range cmds {
		c.GroupID = group
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

func (cf *commonFlags) resolveTolerantInput() (workspace.Context, []error, error) {
	if cf.ctx.Name != "" {
		return cf.ctx, nil, nil
	}
	ctx, err := resolveTargetContext()
	if err != nil {
		return workspace.Context{}, nil, err
	}
	if err := ensureContextReady(ctx); err != nil {
		return workspace.Context{}, nil, err
	}
	skipped, err := enforceContextLocalityTolerant(ctx)
	if err != nil {
		return workspace.Context{}, nil, err
	}
	cf.ctx = ctx
	return ctx, skipped, nil
}

func (cf *commonFlags) resolveWithLocality(checkLocality bool) (workspace.Context, error) {
	if cf.ctx.Name != "" {
		return cf.ctx, nil
	}
	ctx, err := resolveTargetContext()
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

func resolveTargetContext() (workspace.Context, error) {
	if name := strings.TrimSpace(contextOverride); name != "" {
		return workspace.RequireExistingContext(name)
	}
	return workspace.CurrentContext()
}

func registerContextNameCompletion(cmd *cobra.Command, flag string) {
	_ = cmd.RegisterFlagCompletionFunc(flag, func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
		contexts, err := workspace.ListContexts()
		if err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		names := make([]string, 0, len(contexts))
		for _, c := range contexts {
			names = append(names, c.Name)
		}
		return names, cobra.ShellCompDirectiveNoFileComp
	})
}

func requireSubcommand(cmd *cobra.Command) {
	cmd.FParseErrWhitelist = cobra.FParseErrWhitelist{UnknownFlags: true}
	cmd.Args = rejectUnknownSubcommandArgs
	cmd.RunE = func(c *cobra.Command, _ []string) error {
		_ = c.Help()
		return failErr(2, fmt.Errorf("%s requires a subcommand", c.CommandPath()))
	}
}

func rejectUnknownSubcommandArgs(c *cobra.Command, args []string) error {
	if len(args) == 0 {
		return nil
	}
	return failErr(2, unknownSubcommandError(c, args[0]))
}

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
