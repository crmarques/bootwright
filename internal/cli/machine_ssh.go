package cli

import (
	"fmt"
	"io"
	"os"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/host/execution"
	secret "github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/sshtrust"
	stateview "github.com/crmarques/bootwright/internal/state/view"
	"github.com/crmarques/bootwright/internal/workspace"
)

// machineSSHDeps isolates the process-boundary calls `machine ssh` makes — the
// ssh lookup and the exec that replaces this process — so tests exercise the
// argv construction without spawning a real ssh client.
type machineSSHDeps struct {
	lookPath func(string) (string, error)
	exec     func(argv0 string, argv []string, env []string) error
}

var defaultMachineSSHDeps = machineSSHDeps{
	lookPath: execution.LookPath,
	exec:     syscall.Exec,
}

// sshInvocation is the fully resolved ssh command line: the client path, the
// argv (argv[0] == "ssh"), and the environment to run it with.
type sshInvocation struct {
	Path string
	Args []string
	Env  []string
}

func newMachineSSHCmd(_ io.Reader, _ io.Writer, _ io.Writer) *cobra.Command {
	name := ""
	cmd := &cobra.Command{
		Use:   "ssh --name <machine> [-- <command>...]",
		Short: "Open an SSH session to a declared Machine using its recorded key",
		Long: `Connect to a declared Machine over SSH using the identity Bootwright already
knows for it: the Machine's ssh address, login user, private key, and the
context host-key trust store recorded by 'bootwright machine trust'. With no
trailing command this drops you into an interactive shell; a trailing command
(after --) is run on the Machine and its output returned.

    bootwright machine ssh --name ceph-dc1-0
    bootwright machine ssh --name ceph-dc1-0 -- systemctl status ceph.target`,
		Args: cobra.ArbitraryArgs,
		Example: `  # Interactive shell on a Machine
  bootwright machine ssh --name ceph-dc1-0

  # Run one command on a Machine
  bootwright machine ssh --name ceph-dc1-0 -- systemctl status ceph.target`,
	}
	cmd.Flags().StringVar(&name, "name", "", "Machine name to connect to (required)")
	_ = cmd.MarkFlagRequired("name")
	cf := addCommonFlags()
	cmd.RunE = func(_ *cobra.Command, args []string) error {
		state, err := loadDesiredState(cf)
		if err != nil {
			return failErr(1, err)
		}
		sshPath, err := defaultMachineSSHDeps.lookPath("ssh")
		if err != nil {
			return failErr(1, fmt.Errorf("an ssh client is required for machine ssh: %w", err))
		}
		invocation, err := buildMachineSSHInvocation(state, cf.ctx, name, args, sshPath)
		if err != nil {
			return failErr(1, err)
		}
		// exec replaces this process image with ssh, so on success it never
		// returns; the caller's terminal becomes ssh's directly.
		if err := defaultMachineSSHDeps.exec(invocation.Path, invocation.Args, invocation.Env); err != nil {
			return failErr(1, fmt.Errorf("exec ssh: %w", err))
		}
		return nil
	}
	return cmd
}

// buildMachineSSHInvocation assembles the ssh command line for a Machine from
// desired state and the context, resolving the same address, user, key, and
// host-key trust store the Ansible inventory uses — so `machine ssh` reaches a
// Machine exactly the way an apply does. Unlike the inventory it omits
// BatchMode: an operator shell needs interactive prompts. StrictHostKeyChecking
// is accept-new so a Machine not yet recorded by `machine trust` still connects
// (and is recorded) while a changed key is still refused.
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
	env := stateview.Environment(state)
	args := []string{"ssh"}
	if keyPath := secret.ResolveSSHPrivateKeyPath(machine.Spec.Access.SSH.KeyRef.Name, env, ctx.SecretsDir); keyPath != "" {
		args = append(args, "-i", keyPath, "-o", "IdentitiesOnly=yes")
	}
	if knownHosts := machineSSHKnownHostsPath(machine, env, ctx); knownHosts != "" {
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

// machineSSHUser is the login user for a Machine's ssh session: the declared
// user, or root by default — matching the managed-OS install path's default so
// `machine ssh` and the installer agree on who they log in as.
func machineSSHUser(machine v1alpha1.Machine) string {
	if machine.Spec.Access.SSH != nil && machine.Spec.Access.SSH.User != "" {
		return machine.Spec.Access.SSH.User
	}
	return "root"
}

// machineSSHKnownHostsPath resolves the host-key trust file for a Machine: its
// per-Machine knownHostsRef secret when set, else the context-wide managed trust
// store that `machine trust` writes. Mirrors the inventory's resolution so both
// verify against the same recorded keys.
func machineSSHKnownHostsPath(machine v1alpha1.Machine, env *v1alpha1.Environment, ctx workspace.Context) string {
	if machine.Spec.Access.SSH == nil {
		return ""
	}
	if machine.Spec.Access.SSH.KnownHostsRef.Name != "" {
		return secret.ResolvePath(machine.Spec.Access.SSH.KnownHostsRef.Name, env, ctx.SecretsDir)
	}
	return sshtrust.KnownHostsPathForContext(ctx.BaseDir)
}
