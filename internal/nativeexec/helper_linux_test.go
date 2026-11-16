//go:build linux

package nativeexec

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"syscall"
	"testing"
	"time"
)

func TestAHelperIsTheProcfsPinnedProgramUnderTheAccount(t *testing.T) {
	groups := []uint32{60002, 60003}
	command := Helper("__mode", Account{UID: 60001, GID: 60002, Groups: groups})
	groups[0] = 0
	if command.Path != "/proc/self/exe" || !slices.Equal(command.Args, []string{"__mode"}) {
		t.Fatalf("helper runs %s %q", command.Path, command.Args)
	}
	if !slices.Equal(command.Env, []string{"LANG=C", "LC_ALL=C"}) || command.Dir != "/" || command.WaitDelay != time.Second || command.Foreground {
		t.Fatalf("helper environment %q in %q, wait %s", command.Env, command.Dir, command.WaitDelay)
	}
	credential := command.Credential
	if credential == nil || credential.Uid != 60001 || credential.Gid != 60002 || !slices.Equal(credential.Groups, []uint32{60002, 60003}) || credential.NoSetGroups {
		t.Fatalf("helper credential %+v", credential)
	}
	if command.ParentDeath != syscall.SIGKILL {
		t.Fatalf("helper parent-death signal %v", command.ParentDeath)
	}
}

func TestALauncherUsesItsSeamsAndOtherwiseThisPackage(t *testing.T) {
	ran, started := 0, 0
	seamed := Launcher{
		Runner:  func(context.Context, Command) Result { ran++; return Result{Outcome: Exited} },
		Starter: func(context.Context, Command) (*Process, error) { started++; return nil, errors.New("seam") },
	}
	if result := seamed.Run(context.Background(), Command{Path: "relative"}); !result.Succeeded() || ran != 1 {
		t.Fatalf("the run seam was not used: %+v", result)
	}
	if _, err := seamed.Start(context.Background(), Command{Path: "relative"}); err == nil || started != 1 {
		t.Fatalf("the start seam was not used: %v", err)
	}
	if result := (Launcher{}).Run(context.Background(), Command{Path: "relative"}); result.Outcome != StartFailed {
		t.Fatalf("the default run = %+v", result)
	}
	if _, err := (Launcher{}).Start(context.Background(), Command{Path: "relative"}); !errors.Is(err, errRelative) {
		t.Fatalf("the default start = %v", err)
	}
}

func TestAStartedProcessPassesItsExtraFilesAndReportsItsEnd(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	process, err := Start(context.Background(), Command{Path: shell, Args: []string{"-c", "echo held >&3; exit 4"}, ExtraFiles: []*os.File{writer}, ParentDeath: syscall.SIGKILL})
	writer.Close()
	if err != nil {
		t.Fatal(err)
	}
	data := make([]byte, 16)
	n, _ := reader.Read(data)
	if string(data[:n]) != "held\n" {
		t.Fatalf("the child wrote %q to its extra file", data[:n])
	}
	select {
	case <-process.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("the process never ended")
	}
	if result := process.Wait(); result.Outcome != Exited || result.ExitCode != 4 {
		t.Fatalf("Wait = %+v", result)
	}
}

func TestKillingAStartedProcessEndsItsGroup(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "grandchild")
	process, err := Start(context.Background(), Command{Path: shell, Args: []string{"-c", "sleep 30 & echo $! > " + marker + "; wait"}, ParentDeath: syscall.SIGKILL})
	if err != nil {
		t.Fatal(err)
	}
	grandchild := waitForPID(t, marker)
	process.Kill()
	if result := process.Wait(); result.Outcome != Cancelled {
		t.Fatalf("Wait = %+v, want cancelled", result)
	}
	waitGone(t, grandchild)
}

func TestAStartedProcessEndsWithItsContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	process, err := Start(ctx, Command{Path: shell, Args: []string{"-c", "sleep 30"}})
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	if result := process.Wait(); result.Outcome != Cancelled || !errors.Is(result.Err, context.Canceled) {
		t.Fatalf("Wait = %+v, want cancelled", result)
	}
}

func TestAProcessThatCannotStartReportsWhy(t *testing.T) {
	if _, err := Start(context.Background(), Command{Path: filepath.Join(t.TempDir(), "absent")}); err == nil {
		t.Fatal("an absent program started")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Start(ctx, Command{Path: shell, Args: []string{"-c", "exit 0"}}); err == nil {
		t.Fatal("a program started under a cancelled context")
	}
}
