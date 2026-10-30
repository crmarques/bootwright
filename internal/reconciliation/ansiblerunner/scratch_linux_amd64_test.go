//go:build linux && amd64

package ansiblerunner

import (
	"bytes"
	"context"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/crmarques/bootwright/internal/controller/stagelocal"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

// The production runner keeps its job, with the bound material in it, on the
// runtime filesystem and its scratch in a private directory beneath the
// staging parent, never in a directory every account may write.
func TestTheRunnerScratchesBeneathTheStagingParent(t *testing.T) {
	runner := New(nil, nil)
	if runner.jobParent != "/run" || runner.stagingParent != stagelocal.StagingParent ||
		runner.scratchParent != filepath.Join(stagelocal.StagingParent, "runs") {
		t.Fatalf("the runner's parents are job %s, staging %s and scratch %s", runner.jobParent, runner.stagingParent, runner.scratchParent)
	}
}

// Python's multiprocessing manager appends a private directory and socket
// name to TMPDIR. Exercise the longest runner name with that actual suffix:
// a failed bind is otherwise hidden by Ansible's RPC startup timeout.
func TestTheLongestScratchPathFitsTheAnsibleRPCSocket(t *testing.T) {
	base, err := os.MkdirTemp("/tmp", "bw-rpc-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(base) })
	padding := len(scratchParent) - len(base) - 1
	if padding < 1 {
		t.Fatal("test parent cannot match the production path length")
	}
	parent := filepath.Join(base, strings.Repeat("p", padding))
	directory := filepath.Join(parent, scratchPrefix+"4294967295-4294967295", "pymp-12345678")
	if err := os.MkdirAll(directory, 0700); err != nil {
		t.Fatal(err)
	}
	socket := filepath.Join(directory, "sock-0123456789ab")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatalf("Ansible RPC socket at %d bytes cannot bind: %v", len(socket), err)
	}
	listener.Close()
}

// A run creates the staging parent and the scratch parent beneath it when
// they are absent, private to the running identity, and scratches there.
func TestARunCreatesItsPrivateScratchParent(t *testing.T) {
	var output bytes.Buffer
	runner := sweepingRunner(t, completing)
	runner.stagingParent = filepath.Join(t.TempDir(), "staging")
	runner.scratchParent = filepath.Join(runner.stagingParent, "runs")
	if _, err := runner.Run(context.Background(), adapterRequest(t, &output)); err != nil {
		t.Fatalf("a run with no scratch parent yet refused: %+v", diagnostics.Of(err))
	}
	for path, mode := range map[string]os.FileMode{runner.stagingParent: 0711, runner.scratchParent: 0700} {
		info, err := os.Lstat(path)
		if err != nil || !info.IsDir() || info.Mode().Perm()&^0711 != 0 || info.Mode().Perm()&0700 != 0700 || path == runner.scratchParent && info.Mode().Perm() != mode {
			t.Fatalf("%s = %v (%v), want a directory of mode %o", path, info, err, mode)
		}
	}
}

// A scratch parent that is a link, is open to other accounts, or sits beneath
// a staging parent another account may write refuses before any job exists or
// the adapter runs, naming the directory and its required mode, and so does
// one on a noexec filesystem, naming the remount.
func TestAnUnsafeOrNoexecScratchParentRefusesBeforeTheRun(t *testing.T) {
	for name, prepare := range map[string]func(t *testing.T, runner *Runner) string{
		"a link": func(t *testing.T, runner *Runner) string {
			target := privateTempDir(t)
			if err := os.Remove(runner.scratchParent); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(target, runner.scratchParent); err != nil {
				t.Fatal(err)
			}
			return "is not a directory this user owns and alone may write"
		},
		"open to other accounts": func(t *testing.T, runner *Runner) string {
			if err := os.Chmod(runner.scratchParent, 0755); err != nil {
				t.Fatal(err)
			}
			return "with mode 0700"
		},
		"beneath a writable staging parent": func(t *testing.T, runner *Runner) string {
			runner.stagingParent = filepath.Dir(runner.scratchParent)
			if err := os.Chmod(runner.stagingParent, 0777); err != nil {
				t.Fatal(err)
			}
			return "with mode 0711"
		},
		"on a noexec filesystem": func(t *testing.T, runner *Runner) string {
			runner.statfs = func(path string, filesystem *syscall.Statfs_t) error {
				if err := syscall.Statfs(path, filesystem); err != nil {
					return err
				}
				filesystem.Flags |= statNoexec
				return nil
			}
			return "lies on a noexec filesystem"
		},
	} {
		t.Run(name, func(t *testing.T) {
			var output bytes.Buffer
			ran := false
			runner := sweepingRunner(t, completing)
			adapter := runner.command
			runner.command = func(name string, arguments ...string) *exec.Cmd {
				ran = true
				return adapter(name, arguments...)
			}
			runner.scratchParent = filepath.Join(privateTempDir(t), "runs")
			if err := os.Mkdir(runner.scratchParent, 0700); err != nil {
				t.Fatal(err)
			}
			want := prepare(t, &runner)
			_, err := runner.Run(context.Background(), adapterRequest(t, &output))
			reported := diagnostics.Of(err)
			if len(reported) != 1 || reported[0].Code != "lifecycle.state" || !strings.Contains(reported[0].Message+" "+reported[0].Remediation, want) {
				t.Fatalf("the run reported %+v (%v), want lifecycle.state naming %q", reported, err, want)
			}
			if ran || len(runNames(t, runner.jobParent)) != 0 {
				t.Fatalf("the refused run started its adapter (%t) or left jobs %v", ran, runNames(t, runner.jobParent))
			}
		})
	}
}
