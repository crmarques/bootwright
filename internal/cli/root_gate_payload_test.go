package cli

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestGateClassifiesOnBootwrightFlagsNotForwardedOnes(t *testing.T) {
	cases := []struct {
		args []string
		want bool
	}{
		{args: []string{"cluster", "exec", "--name", "ceph-prd-01", "--", "sudo", "cephadm", "shell", "--", "ceph", "auth", "get-or-create", "-h"}, want: true},
		{args: []string{"cluster", "exec", "--name", "ceph-prd-01", "--", "ceph", "auth", "get-or-create", "--help"}, want: true},
		{args: []string{"cluster", "exec", "--name", "ceph-prd-01", "--", "systemctl", "help"}, want: true},
		{args: []string{"cluster", "exec", "--name", "ceph-prd-01", "--", "ssh", "--ssh-user", "root@evil"}, want: true},
		{args: []string{"cluster", "rsh", "--name", "ceph-prd-01"}, want: true},
		{args: []string{"machine", "exec", "--name", "ceph-0", "--", "podman", "run", "-h"}, want: true},
		{args: []string{"machine", "exec", "--name", "ceph-0", "--", "--help"}, want: true},
		{args: []string{"cluster", "exec", "--name", "ceph-prd-01", "-h", "--", "uptime"}, want: false},
		{args: []string{"cluster", "exec", "--help"}, want: false},
		{args: []string{"cluster", "exec", "--", "uptime"}, want: false},
		{args: []string{"container-cluster", "oc", "--name", "managed-01", "get", "pods", "-h"}, want: true},
		{args: []string{"container-cluster", "oc", "--name", "managed-01", "explain", "pod", "--help"}, want: true},
		{args: []string{"container-cluster", "kubectl", "--name", "managed-01", "get", "pods", "--help"}, want: true},
		{args: []string{"container-cluster", "kubectl", "--name=managed-01", "help"}, want: true},
		{args: []string{"container-cluster", "oc", "--name", "managed-01", "--", "get", "pods", "-h"}, want: true},
		{args: []string{"container-cluster", "oc", "--name", "managed-01", "--help"}, want: false},
		{args: []string{"container-cluster", "oc", "-h"}, want: false},
		{args: []string{"container-cluster", "oc", "get", "nodes"}, want: false},
		{args: []string{"container-cluster", "kubeconfig", "--name", "managed-01"}, want: true},
		{args: []string{"container-cluster", "kubeconfig"}, want: false},
		{args: []string{"storage-cluster", "replace-arbiter", "--name", "ceph-prd-01", "--yes"}, want: true},
		{args: []string{"storage-cluster", "replace-arbiter", "--yes"}, want: false},
		{args: []string{"storage-cluster", "rebalance"}, want: false},
		{args: []string{"cluster", "kubeconfig", "--name", "managed-01"}, want: false},
		{args: []string{"cluster", "oc", "--name", "managed-01", "get", "nodes"}, want: false},
	}
	for _, tc := range cases {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			if got := argsNeedLocalRoot(tc.args); got != tc.want {
				t.Fatalf("argsNeedLocalRoot(%v) = %v, want %v", tc.args, got, tc.want)
			}
		})
	}
}

func TestGateBecomeClassificationIgnoresForwardedArguments(t *testing.T) {
	cases := []struct {
		args []string
		want bool
	}{
		{args: []string{"cluster", "exec", "--name", "ceph-prd-01", "--", "apply", "-f", "manifest.yaml"}, want: false},
		{args: []string{"machine", "exec", "--name", "ceph-0", "--", "destroy"}, want: false},
		{args: []string{"storage-cluster", "replace-arbiter", "--name", "ceph-prd-01"}, want: true},
		{args: []string{"storage-cluster", "replace-arbiter"}, want: true},
		{args: []string{"container-cluster", "oc", "--name", "managed-01", "apply", "-f", "manifest.yaml"}, want: false},
	}
	for _, tc := range cases {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			if got := argsMayUseBecome(tc.args); got != tc.want {
				t.Fatalf("argsMayUseBecome(%v) = %v, want %v", tc.args, got, tc.want)
			}
		})
	}
}

func TestEnsureLocalRootEscalatesForwardedHelpRequests(t *testing.T) {
	setTestHomeAndRoot(t)
	previous := localRootGate
	defer func() { localRootGate = previous }()

	called := false
	localRootGate = localRootGateDeps{
		enabled:    true,
		geteuid:    func() int { return 1000 },
		executable: func() (string, error) { return "/usr/local/bin/bootwright", nil },
		commandContext: func(ctx context.Context, _ string, _ ...string) *exec.Cmd {
			called = true
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=TestLocalRootGateHelperProcess", "--")
			cmd.Env = append(os.Environ(), "BOOTWRIGHT_ROOT_GATE_HELPER=1")
			return cmd
		},
	}

	args := []string{"cluster", "exec", "--name", "ceph-prd-01", "--", "sudo", "cephadm", "shell", "--", "ceph", "auth", "get-or-create", "-h"}
	code, handled, err := ensureLocalRootForArgs(context.Background(), args, strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("ensureLocalRootForArgs: %v", err)
	}
	if !handled || code != 0 || !called {
		t.Fatalf("ensureLocalRootForArgs(%v) handled=%v code=%d called=%t, want the sudo re-exec", args, handled, code, called)
	}
}
