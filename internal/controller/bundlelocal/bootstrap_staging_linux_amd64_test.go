//go:build linux && amd64

package bundlelocal

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"

	"github.com/crmarques/bootwright/internal/controller/nativelocal"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

// recordingStaging hands out the stages of the staging it wraps and records
// the kind and path of each.
type recordingStaging struct {
	staging prerequisites.Staging
	kinds   []string
	paths   []string
}

func (r *recordingStaging) Stage(ctx context.Context, kind string) (prerequisites.Stage, error) {
	stage, err := r.staging.Stage(ctx, kind)
	if err == nil {
		r.kinds = append(r.kinds, kind)
		r.paths = append(r.paths, stage.Path)
	}
	return stage, err
}

// The bootstrap resolver's chroot is a stage beneath the staging parent,
// never the shared temporary directory, and nothing of it remains once the
// stage is released, whether the host's provided libraries qualified it or
// refused it.
func TestTheBootstrapResolverStagesBeneathTheStagingRoot(t *testing.T) {
	parent := filepath.Join(t.TempDir(), "staging")
	staging := &recordingStaging{staging: nativelocal.NewStaging(parent)}
	requirement := compiledExecution(t, fedoraPlatform)
	requirement.PythonExecutable = "python/bin/python3.13"
	projected := newProjection()
	if err := projected.add(requirement.PythonExecutable, []byte("interpreter"), true); err != nil {
		t.Fatal(err)
	}
	root, stage, err := stageBootstrap(t.Context(), staging, projected, prerequisites.BootstrapDefinition{PythonExecutable: requirement.PythonExecutable, Execution: requirement}, []byte("trust"))
	if !slices.Equal(staging.kinds, []string{"resolver"}) || !strings.HasPrefix(staging.paths[0], parent+"/") {
		t.Fatalf("the bootstrap stage is not beneath %s: %q %q", parent, staging.kinds, staging.paths)
	}
	if err == nil {
		if root != filepath.Join(staging.paths[0], "root") || stage.Path != staging.paths[0] {
			t.Fatalf("the chroot %s is not the stage's own", root)
		}
		if _, statErr := os.Stat(filepath.Join(root, requirement.PythonExecutable)); statErr != nil {
			t.Fatalf("the projection was not staged: %v", statErr)
		}
		stage.Release()
	}
	entries, readErr := os.ReadDir(parent)
	if readErr != nil || len(entries) != 0 {
		t.Fatalf("the bootstrap stage outlived its use: %v %v", entries, readErr)
	}
}

// An execution the kernel denies on a stage whose filesystem is mounted
// noexec names that mount and the remount that settles it, in setup's scope
// and in a stage's. Any other start failure, and a denial on an exec-capable
// filesystem, keeps the resolution's own requirements.
func TestAnExecDenialOnANoexecStageNamesTheMount(t *testing.T) {
	stage := prerequisites.Stage{Path: nativelocal.StagingParent + "/resolver-0123456789abcdef", Noexec: true}
	for _, errno := range []syscall.Errno{syscall.EACCES, syscall.EPERM} {
		denied := &os.PathError{Op: "fork/exec", Path: "/usr/lib64/ld-linux-x86-64.so.2", Err: errno}
		err := bootstrapStartFailure(denied, stage)
		found := diagnostics.Of(err)
		if len(found) != 1 || found[0].Code != "controller.setup" || !strings.Contains(found[0].Message, "noexec") || !strings.Contains(found[0].Message, nativelocal.StagingParent) {
			t.Fatalf("%v on a noexec stage: %+v", errno, found)
		}
		if found[0].Remediation != "Mount the filesystem holding "+nativelocal.StagingParent+" with exec, then rerun bootwright setup." {
			t.Fatalf("%v on a noexec stage: remedy %q", errno, found[0].Remediation)
		}
		staged := diagnostics.Of(prerequisites.InStage(err, "lab"))
		if len(staged) != 1 || staged[0].Code != "controller.setup" || !strings.Contains(staged[0].Remediation, "with exec, then run bootwright apply --stage controller --context lab.") {
			t.Fatalf("%v on a noexec stage in a controller stage: %+v", errno, staged)
		}
		capable := stage
		capable.Noexec = false
		requirement := diagnostics.Of(bootstrapStartFailure(denied, capable))
		if len(requirement) != 1 || strings.Contains(requirement[0].Message, "noexec") || !strings.Contains(requirement[0].Message, "unprivileged Linux namespaces") || !strings.Contains(requirement[0].Message, "fapolicyd") {
			t.Fatalf("%v on an exec-capable stage: %+v", errno, requirement)
		}
	}
	other := diagnostics.Of(bootstrapStartFailure(&os.PathError{Op: "fork/exec", Path: "/usr/lib64/ld-linux-x86-64.so.2", Err: syscall.ENOENT}, stage))
	if len(other) != 1 || strings.Contains(other[0].Message, "noexec") || !strings.Contains(other[0].Message, "unprivileged Linux namespaces") {
		t.Fatalf("a missing loader on a noexec stage: %+v", other)
	}
}

// deniedStart is a resolver command the kernel refuses to start with errno,
// built afresh for each run.
type deniedStart struct {
	errno syscall.Errno
	build func() *exec.Cmd
}

// startFails proves the denial fires: the command's own start fails with the
// errno the test claims, before any assertion relies on it.
func (d deniedStart) startFails(t *testing.T) {
	t.Helper()
	command := d.build()
	err := command.Start()
	if err == nil {
		_ = command.Process.Kill()
		_ = command.Wait()
	}
	if !errors.Is(err, d.errno) {
		t.Fatalf("the start denial did not fire as %v: %v", d.errno, err)
	}
}

// A staged resolver the kernel will not start, with EACCES because the staged
// file may not execute or with EPERM before execution, names the noexec mount
// holding its stage, in setup's scope, and keeps the resolution's own
// requirements on an exec-capable stage. A resolver that starts and fails, or
// that succeeds, is judged by its run alone.
func TestAStagedResolverDeniedItsStartNamesTheNoexecMount(t *testing.T) {
	parent := filepath.Join(t.TempDir(), "staging")
	stage, err := nativelocal.NewStaging(parent).Stage(t.Context(), "resolver")
	if err != nil {
		t.Fatal(err)
	}
	defer stage.Release()
	loader := filepath.Join(stage.Path, "loader")
	if err := os.WriteFile(loader, []byte("#!/bin/sh\n"), 0644); err != nil {
		t.Fatal(err)
	}
	for _, denied := range []deniedStart{
		{errno: syscall.EACCES, build: func() *exec.Cmd { return exec.CommandContext(t.Context(), loader) }},
		{errno: syscall.EPERM, build: func() *exec.Cmd {
			command := exec.CommandContext(t.Context(), loader)
			// A session leader may not change its process group, so the
			// child fails before it executes anything.
			command.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setpgid: true}
			return command
		}},
	} {
		denied.startFails(t)
		noexec := stage
		noexec.Noexec = true
		_, err := runResolver(t.Context(), denied.build(), noexec)
		found := diagnostics.Of(err)
		if len(found) != 1 || found[0].Code != "controller.setup" || !strings.Contains(found[0].Message, parent+" is on a filesystem mounted noexec") {
			t.Fatalf("%v on a noexec stage: %+v", denied.errno, found)
		}
		if found[0].Remediation != "Mount the filesystem holding "+parent+" with exec, then rerun bootwright setup." {
			t.Fatalf("%v on a noexec stage: remedy %q", denied.errno, found[0].Remediation)
		}
		capable := stage
		capable.Noexec = false
		_, err = runResolver(t.Context(), denied.build(), capable)
		requirement := diagnostics.Of(err)
		if len(requirement) != 1 || strings.Contains(requirement[0].Message, "noexec") || !strings.Contains(requirement[0].Message, "unprivileged Linux namespaces") || !strings.Contains(requirement[0].Message, "fapolicyd") {
			t.Fatalf("%v on an exec-capable stage: %+v", denied.errno, requirement)
		}
	}
	shell, err := exec.LookPath("sh")
	if err != nil || !filepath.IsAbs(shell) {
		t.Skipf("no shell runs the started-resolver cases: %v", err)
	}
	noexec := stage
	noexec.Noexec = true
	report, err := runResolver(t.Context(), exec.CommandContext(t.Context(), shell, "-c", "printf report"), noexec)
	if err != nil || string(report) != "report" {
		t.Fatalf("a resolver that succeeded returned %q, %v", report, err)
	}
	_, err = runResolver(t.Context(), exec.CommandContext(t.Context(), shell, "-c", "exit 3"), noexec)
	failed := diagnostics.Of(err)
	if len(failed) != 1 || strings.Contains(failed[0].Message, "noexec") || !strings.Contains(failed[0].Message, "unprivileged Linux namespaces") {
		t.Fatalf("a resolver that started and failed on a noexec stage: %+v", failed)
	}
}
