package cli

import (
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/sshtrust"
	"github.com/crmarques/bootwright/internal/workspace"
)

func machineSSHTestState() v1alpha1.State {
	return v1alpha1.State{Machines: []v1alpha1.Machine{{
		Metadata: v1alpha1.Metadata{Name: "ceph-0"},
		Spec: v1alpha1.MachineSpec{
			OS:        v1alpha1.MachineOSSpec{Provided: v1alpha1.BoolPtr(false)},
			Addresses: []v1alpha1.MachineAddress{{Name: "ssh", Address: "10.0.0.10"}},
			Access: v1alpha1.MachineAccess{SSH: &v1alpha1.MachineSSHSpec{
				AddressRef: v1alpha1.LocalObjectReference{Name: "ssh"},
				KeyRef:     v1alpha1.SecretRef{Name: "ceph-key"},
			}},
		},
	}}}
}

func indexOf(args []string, want string) int {
	for i, a := range args {
		if a == want {
			return i
		}
	}
	return -1
}

func TestBuildMachineSSHInvocation(t *testing.T) {
	state := machineSSHTestState()
	ctx := workspace.Context{Name: "lab", BaseDir: "/base", SecretsDir: "/base/secrets"}

	inv, err := buildMachineSSHInvocation(state, ctx, "ceph-0", []string{"uptime"}, "/usr/bin/ssh")
	if err != nil {
		t.Fatalf("buildMachineSSHInvocation: %v", err)
	}
	if inv.Path != "/usr/bin/ssh" {
		t.Fatalf("path = %q", inv.Path)
	}
	if inv.Args[0] != "ssh" {
		t.Fatalf("argv[0] = %q, want ssh", inv.Args[0])
	}
	if i := indexOf(inv.Args, "-i"); i < 0 || inv.Args[i+1] != "/base/secrets/ceph-key" {
		t.Fatalf("key arg missing/wrong in %v", inv.Args)
	}
	wantKH := "UserKnownHostsFile=" + sshtrust.KnownHostsPathForContext("/base")
	if indexOf(inv.Args, wantKH) < 0 {
		t.Fatalf("known-hosts arg %q missing in %v", wantKH, inv.Args)
	}
	if indexOf(inv.Args, "StrictHostKeyChecking=accept-new") < 0 {
		t.Fatalf("accept-new arg missing in %v", inv.Args)
	}
	// BatchMode would suppress interactive prompts; an operator shell must not set it.
	if indexOf(inv.Args, "BatchMode=yes") >= 0 {
		t.Fatalf("BatchMode must not be set for an interactive session: %v", inv.Args)
	}
	// The target and the trailing command are the last args, in order.
	last := inv.Args[len(inv.Args)-2:]
	if last[0] != "root@10.0.0.10" || last[1] != "uptime" {
		t.Fatalf("tail = %v, want [root@10.0.0.10 uptime]", last)
	}
}

func TestBuildMachineSSHInvocationUserOverride(t *testing.T) {
	state := machineSSHTestState()
	state.Machines[0].Spec.Access.SSH.User = "core"
	ctx := workspace.Context{Name: "lab", BaseDir: "/base", SecretsDir: "/base/secrets"}

	inv, err := buildMachineSSHInvocation(state, ctx, "ceph-0", nil, "/usr/bin/ssh")
	if err != nil {
		t.Fatalf("buildMachineSSHInvocation: %v", err)
	}
	if inv.Args[len(inv.Args)-1] != "core@10.0.0.10" {
		t.Fatalf("target = %q, want core@10.0.0.10", inv.Args[len(inv.Args)-1])
	}
}

func TestBuildMachineSSHInvocationErrors(t *testing.T) {
	ctx := workspace.Context{Name: "lab", BaseDir: "/base", SecretsDir: "/base/secrets"}

	if _, err := buildMachineSSHInvocation(machineSSHTestState(), ctx, "ghost", nil, "/usr/bin/ssh"); err == nil || !strings.Contains(err.Error(), "unknown Machine") {
		t.Fatalf("unknown machine err = %v", err)
	}

	noSSH := v1alpha1.State{Machines: []v1alpha1.Machine{{
		Metadata: v1alpha1.Metadata{Name: "m"},
		Spec:     v1alpha1.MachineSpec{OS: v1alpha1.MachineOSSpec{Provided: v1alpha1.BoolPtr(true)}},
	}}}
	if _, err := buildMachineSSHInvocation(noSSH, ctx, "m", nil, "/usr/bin/ssh"); err == nil || !strings.Contains(err.Error(), "no SSH access") {
		t.Fatalf("no-ssh err = %v", err)
	}

	noAddr := machineSSHTestState()
	noAddr.Machines[0].Spec.Access.SSH.AddressRef = v1alpha1.LocalObjectReference{Name: "missing"}
	if _, err := buildMachineSSHInvocation(noAddr, ctx, "ceph-0", nil, "/usr/bin/ssh"); err == nil || !strings.Contains(err.Error(), "no resolvable SSH address") {
		t.Fatalf("no-address err = %v", err)
	}
}

func TestMachineSSHCLIExecsClient(t *testing.T) {
	initHostTrustTestContext(t)

	var gotPath string
	var gotArgs []string
	previous := defaultMachineSSHDeps
	defaultMachineSSHDeps = machineSSHDeps{
		lookPath: func(string) (string, error) { return "/usr/bin/ssh", nil },
		exec: func(path string, argv []string, _ []string) error {
			gotPath = path
			gotArgs = argv
			return nil
		},
	}
	t.Cleanup(func() { defaultMachineSSHDeps = previous })

	_, stderr, code := runCLI(t, "machine", "exec", "--name", "provider-01", "--", "uptime")
	if code != 0 {
		t.Fatalf("machine exec exited %d, stderr=%q", code, stderr)
	}
	if gotPath != "/usr/bin/ssh" {
		t.Fatalf("exec path = %q", gotPath)
	}
	if indexOf(gotArgs, "core@provider-01.example.test") < 0 {
		t.Fatalf("target missing in argv %v", gotArgs)
	}
	if gotArgs[len(gotArgs)-1] != "uptime" {
		t.Fatalf("trailing command = %q, want uptime", gotArgs[len(gotArgs)-1])
	}
}

func TestMachineRshExecsInteractiveShell(t *testing.T) {
	initHostTrustTestContext(t)

	var gotArgs []string
	previous := defaultMachineSSHDeps
	defaultMachineSSHDeps = machineSSHDeps{
		lookPath: func(string) (string, error) { return "/usr/bin/ssh", nil },
		exec: func(_ string, argv []string, _ []string) error {
			gotArgs = argv
			return nil
		},
	}
	t.Cleanup(func() { defaultMachineSSHDeps = previous })

	_, stderr, code := runCLI(t, "machine", "rsh", "--name", "provider-01")
	if code != 0 {
		t.Fatalf("machine rsh exited %d, stderr=%q", code, stderr)
	}
	// An interactive shell ends at the target, with no trailing command.
	if gotArgs[len(gotArgs)-1] != "core@provider-01.example.test" {
		t.Fatalf("rsh argv should end at the target, got %v", gotArgs)
	}
}

func TestMachineRshRejectsTrailingCommand(t *testing.T) {
	initHostTrustTestContext(t)
	_, stderr, code := runCLI(t, "machine", "rsh", "--name", "provider-01", "--", "uptime")
	if code != 2 {
		t.Fatalf("machine rsh with a command should exit 2, got %d", code)
	}
	if !strings.Contains(stderr, "machine exec") {
		t.Fatalf("rsh error should point at machine exec: %q", stderr)
	}
}

func TestMachineExecRequiresCommand(t *testing.T) {
	initHostTrustTestContext(t)
	_, stderr, code := runCLI(t, "machine", "exec", "--name", "provider-01")
	if code != 2 {
		t.Fatalf("machine exec without a command should exit 2, got %d", code)
	}
	if !strings.Contains(stderr, "requires a command after --") {
		t.Fatalf("exec error should explain the missing command: %q", stderr)
	}
}

func TestMachineRshRequiresName(t *testing.T) {
	initHostTrustTestContext(t)
	if _, _, code := runCLI(t, "machine", "rsh"); code == 0 {
		t.Fatal("machine rsh without --name should fail")
	}
}
