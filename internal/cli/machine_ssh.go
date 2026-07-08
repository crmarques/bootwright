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

// machineSSHDeps isolates the process-boundary calls the ssh commands make — the
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

// newMachineRshCmd opens an interactive SSH shell on a declared Machine. It is
// the machine-first entrance to the shared ssh engine; `cluster rsh` is the
// cluster-first one. Running a single command instead is `machine exec`.
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

// newMachineExecCmd runs a single command on a declared Machine over SSH,
// returning its output. Its trailing command (after --) is the only difference
// from `machine rsh`; both share runSSHToMachine.
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

// runSSHToMachine is the machine-first entry to the shared ssh engine: it loads
// desired state, then hands off to execSSHToMachine. It never returns on success
// — exec replaces this process image.
func runSSHToMachine(cf *commonFlags, machineName string, cmdArgs []string) error {
	state, err := loadDesiredState(cf)
	if err != nil {
		return failErr(1, err)
	}
	return execSSHToMachine(cf.ctx, state, machineName, cmdArgs)
}

// execSSHToMachine is the one ssh resolve+exec engine behind every rsh/exec
// command (machine- and cluster-first alike): it finds the ssh client, builds
// the invocation for machineName from already-loaded state, and exec-replaces
// this process with ssh. cmdArgs empty ⇒ interactive shell; non-empty ⇒ that
// command runs on the Machine. On success it does not return.
func execSSHToMachine(ctx workspace.Context, state v1alpha1.State, machineName string, cmdArgs []string) error {
	sshPath, err := defaultMachineSSHDeps.lookPath("ssh")
	if err != nil {
		return failErr(1, fmt.Errorf("an ssh client is required to reach a Machine over SSH: %w", err))
	}
	invocation, err := buildMachineSSHInvocation(state, ctx, machineName, cmdArgs, sshPath)
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

// buildMachineSSHInvocation assembles the ssh command line for a Machine from
// desired state and the context, resolving the same address, user, key, and
// host-key trust store the Ansible inventory uses — so an operator ssh reaches a
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
// store that `machine trust` writes. The ref-vs-managed decision is shared with
// the inventory renderer through sshtrust.MachineKnownHostsPath so both verify
// against the same recorded keys; only the managed-store location differs (a live
// context here, render's PathOptions there).
func machineSSHKnownHostsPath(machine v1alpha1.Machine, idx secret.Index, ctx workspace.Context) string {
	return sshtrust.MachineKnownHostsPath(machine, idx, ctx.SecretsDir, sshtrust.KnownHostsPathForContext(ctx.BaseDir))
}
