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
'bootwright machine trust'. Override the login account for this invocation with
--ssh-user. Run a single command instead with 'machine exec'.

    bootwright machine rsh --name ceph-dc1-0`,
		Args: cobra.ArbitraryArgs,
		Example: `  # Interactive shell on a Machine
  bootwright machine rsh --name ceph-dc1-0

  # Log in as a different account with your own key
  bootwright machine rsh --name ceph-dc1-0 --ssh-user operator --preferred-ssh-id-key ~/.ssh/id_ed25519`,
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
using the identity Bootwright already knows for the Machine. Override the login
account for this invocation with --ssh-user. Drop into an interactive shell
instead with 'machine rsh'.

    bootwright machine exec --name ceph-dc1-0 -- systemctl status ceph.target`,
		Args: cobra.ArbitraryArgs,
		Example: `  # Run one command on a Machine
  bootwright machine exec --name ceph-dc1-0 -- systemctl status ceph.target

  # Run it as a different account with your own key
  bootwright machine exec --name ceph-dc1-0 --ssh-user operator --preferred-ssh-id-key ~/.ssh/id_ed25519 -- id`,
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
	user, err := resolveSSHUser()
	if err != nil {
		return failErr(2, err)
	}
	state, err := loadDesiredState(cf)
	if err != nil {
		return failErr(1, err)
	}
	return execSSHToMachine(commandCtx, cf.ctx, state, machineName, user, cmdArgs)
}

func execSSHToMachine(commandCtx context.Context, ctx workspace.Context, state v1alpha1.State, machineName, sshUser string, cmdArgs []string) error {
	target, err := machineSSHTarget(state, machineName)
	if err != nil {
		return failErr(1, err)
	}
	return execSSHTarget(commandCtx, ctx, state, overrideSSHUser(target, sshUser), cmdArgs)
}

func overrideSSHUser(target sshTarget, sshUser string) sshTarget {
	if sshUser != "" {
		target.User = sshUser
	}
	return target
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
		User:          machineLoginUser(state, machine),
		Port:          machine.Spec.Access.SSH.Port,
		KeyRef:        v1alpha1.MachineSSHKeyRef(machine),
		KnownHostsRef: machine.Spec.Access.SSH.KnownHostsRef,
	}, nil
}

func machineSSHUser(machine v1alpha1.Machine) string {
	return v1alpha1.MachineSSHUser(machine)
}

func machineLoginUser(state v1alpha1.State, machine v1alpha1.Machine) string {
	if v1alpha1.MachineUsesControllerIdentity(machine) {
		return ""
	}
	if user, ok := storageNodeLoginUser(state, machine.Metadata.Name); ok {
		return user
	}
	return v1alpha1.MachineSSHUser(machine)
}

func storageNodeLoginUser(state v1alpha1.State, machineName string) (string, bool) {
	for _, cluster := range state.StorageClusters {
		if !v1alpha1.StorageClusterManaged(cluster) || cluster.Spec.Ceph == nil {
			continue
		}
		if !v1alpha1.StorageClusterManagesNodeAccount(cluster) {
			continue
		}
		for _, node := range cluster.Spec.Ceph.Topology.Nodes {
			if node.MachineRef.Name == machineName {
				return v1alpha1.StorageClusterCephadmSSHUser(cluster), true
			}
		}
	}
	return "", false
}
