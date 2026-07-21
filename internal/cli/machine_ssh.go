package cli

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/crmarques/bootwright/api/v1alpha1"
	stateview "github.com/crmarques/bootwright/internal/state/view"
	"github.com/crmarques/bootwright/internal/workspace"
)

func newMachineRshCmd() *cobra.Command {
	name := ""
	cmd := &cobra.Command{
		Use:   "rsh --name <machine>",
		Short: "Open an interactive SSH shell on a declared Machine",
		Long: `Open an interactive remote shell on a declared Machine over SSH using the
identity Bootwright already knows for it: the Machine's ssh address, login user,
private key, and the context host-key trust store recorded by
'bootwright machine trust'. Run a single command instead with 'machine exec'.

    bootwright machine rsh --name ceph-dc1-0`,
		Args: cobra.ArbitraryArgs,
		Example: `  # Interactive shell on a Machine
  bootwright machine rsh --name ceph-dc1-0`,
	}
	cmd.Flags().StringVar(&name, "name", "", "Machine name to connect to (required)")
	_ = cmd.MarkFlagRequired("name")
	registerMachineNameCompletion(cmd)
	cf := addCommonFlags()
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if len(args) > 0 {
			return failErr(2, fmt.Errorf("machine rsh opens an interactive shell and takes no command; run one with 'machine exec --name %s -- %s'", name, strings.Join(args, " ")))
		}
		return runSSHToMachine(cmd.Context(), cf, name, nil)
	}
	return cmd
}

func newMachineExecCmd() *cobra.Command {
	name := ""
	cmd := &cobra.Command{
		Use:   "exec --name <machine> -- <command>...",
		Short: "Run a command on a declared Machine over SSH",
		Long: `Run a single command on a declared Machine over SSH and return its output,
using the identity Bootwright already knows for the Machine. Drop into an
interactive shell instead with 'machine rsh'.

    bootwright machine exec --name ceph-dc1-0 -- systemctl status ceph.target`,
		Args: cobra.ArbitraryArgs,
		Example: `  # Run one command on a Machine
  bootwright machine exec --name ceph-dc1-0 -- systemctl status ceph.target`,
	}
	cmd.Flags().StringVar(&name, "name", "", "Machine name to run the command on (required)")
	_ = cmd.MarkFlagRequired("name")
	registerMachineNameCompletion(cmd)
	cf := addCommonFlags()
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			return failErr(2, errors.New("machine exec requires a command after --, e.g. 'machine exec --name <machine> -- systemctl status ceph.target'"))
		}
		return runSSHToMachine(cmd.Context(), cf, name, args)
	}
	return cmd
}

func runSSHToMachine(commandCtx context.Context, cf *commonFlags, machineName string, cmdArgs []string) error {
	state, err := loadDesiredState(cf)
	if err != nil {
		return failErr(1, err)
	}
	return execSSHToMachine(commandCtx, cf.ctx, state, machineName, cmdArgs)
}

func execSSHToMachine(commandCtx context.Context, ctx workspace.Context, state v1alpha1.State, machineName string, cmdArgs []string) error {
	target, err := machineSSHTarget(state, machineName)
	if err != nil {
		return failErr(1, err)
	}
	return execSSHTarget(commandCtx, ctx, state, target, cmdArgs)
}

func machineSSHTarget(state v1alpha1.State, name string) (sshTarget, error) {
	machine, ok := stateview.Machine(state, name)
	if !ok {
		return sshTarget{}, fmt.Errorf("unknown Machine %q", name)
	}
	if machine.Spec.Access.SSH == nil {
		return sshTarget{}, fmt.Errorf("machine %q declares no SSH access (spec.access.ssh)", name)
	}
	address := stateview.MachineConnectionAddress(state, machine)
	if address == "" {
		return sshTarget{}, fmt.Errorf("machine %q has no resolvable SSH address; check spec.access.ssh.addressRef", name)
	}
	return sshTarget{
		Address:       address,
		User:          machineSSHUser(machine),
		KeyRef:        machine.Spec.Access.SSH.KeyRef,
		KnownHostsRef: machine.Spec.Access.SSH.KnownHostsRef,
	}, nil
}

func machineSSHUser(machine v1alpha1.Machine) string {
	if machine.Spec.Access.SSH != nil && machine.Spec.Access.SSH.User != "" {
		return machine.Spec.Access.SSH.User
	}
	return "root"
}
