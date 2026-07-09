package cli

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/host/execution"
	secret "github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/sshtrust"
	stateview "github.com/crmarques/bootwright/internal/state/view"
	"github.com/crmarques/bootwright/internal/workspace"
)

type machineSSHDeps struct {
	lookPath func(string) (string, error)
	exec     func(argv0 string, argv []string, env []string) error
}

var defaultMachineSSHDeps = machineSSHDeps{
	lookPath: execution.LookPath,
	exec:     syscall.Exec,
}

type sshInvocation struct {
	Path string
	Args []string
	Env  []string
}

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
	cmd.RunE = func(_ *cobra.Command, args []string) error {
		if len(args) > 0 {
			return failErr(2, fmt.Errorf("machine rsh opens an interactive shell and takes no command; run one with 'machine exec --name %s -- %s'", name, strings.Join(args, " ")))
		}
		return runSSHToMachine(cf, name, nil)
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
	cmd.RunE = func(_ *cobra.Command, args []string) error {
		if len(args) == 0 {
			return failErr(2, errors.New("machine exec requires a command after --, e.g. 'machine exec --name <machine> -- systemctl status ceph.target'"))
		}
		return runSSHToMachine(cf, name, args)
	}
	return cmd
}

func runSSHToMachine(cf *commonFlags, machineName string, cmdArgs []string) error {
	state, err := loadDesiredState(cf)
	if err != nil {
		return failErr(1, err)
	}
	return execSSHToMachine(cf.ctx, state, machineName, cmdArgs)
}

func execSSHToMachine(ctx workspace.Context, state v1alpha1.State, machineName string, cmdArgs []string) error {
	sshPath, err := defaultMachineSSHDeps.lookPath("ssh")
	if err != nil {
		return failErr(1, fmt.Errorf("an ssh client is required to reach a Machine over SSH: %w", err))
	}
	invocation, err := buildMachineSSHInvocation(state, ctx, machineName, cmdArgs, sshPath)
	if err != nil {
		return failErr(1, err)
	}
	if err := defaultMachineSSHDeps.exec(invocation.Path, invocation.Args, invocation.Env); err != nil {
		return failErr(1, fmt.Errorf("exec ssh: %w", err))
	}
	return nil
}

func buildMachineSSHInvocation(state v1alpha1.State, ctx workspace.Context, name string, extraArgs []string, sshPath string) (sshInvocation, error) {
	machine, ok := stateview.Machine(state, name)
	if !ok {
		return sshInvocation{}, fmt.Errorf("unknown Machine %q", name)
	}
	if machine.Spec.Access.SSH == nil {
		return sshInvocation{}, fmt.Errorf("machine %q declares no SSH access (spec.access.ssh)", name)
	}
	address := v1alpha1.MachineSSHAddress(machine)
	if address == "" {
		return sshInvocation{}, fmt.Errorf("machine %q has no resolvable SSH address; check spec.access.ssh.addressRef", name)
	}
	idx := secret.NewIndex(state)
	args := []string{"ssh"}
	if keyPath := secret.ResolveSSHPrivateKeyPath(machine.Spec.Access.SSH.KeyRef.Name, idx, ctx.SecretsDir); keyPath != "" {
		args = append(args, "-i", keyPath, "-o", "IdentitiesOnly=yes")
	}
	if knownHosts := machineSSHKnownHostsPath(machine, idx, ctx); knownHosts != "" {
		args = append(args, "-o", "UserKnownHostsFile="+knownHosts, "-o", "StrictHostKeyChecking=accept-new")
	}
	target := address
	if user := machineSSHUser(machine); user != "" {
		target = user + "@" + address
	}
	args = append(args, target)
	args = append(args, extraArgs...)
	return sshInvocation{Path: sshPath, Args: args, Env: os.Environ()}, nil
}

func machineSSHUser(machine v1alpha1.Machine) string {
	if machine.Spec.Access.SSH != nil && machine.Spec.Access.SSH.User != "" {
		return machine.Spec.Access.SSH.User
	}
	return "root"
}

func machineSSHKnownHostsPath(machine v1alpha1.Machine, idx secret.Index, ctx workspace.Context) string {
	return sshtrust.MachineKnownHostsPath(machine, idx, ctx.SecretsDir, sshtrust.KnownHostsPathForContext(ctx.BaseDir))
}
