//go:build linux && amd64

package ansiblerunner

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// Releasing a job unlocks its holder before closing it, so a copy of the
// holder's descriptor a child forked meanwhile still holds never keeps the
// ended invocation alive to the next sweep.
func TestReleaseUnlocksTheHolderWhileACopyOfItsDescriptorLives(t *testing.T) {
	runner := sweepingRunner(t, completing)
	job, scratch, lock, holder := claimJob(t, runner, adapterRequest(t, &bytes.Buffer{}), 1)
	copied, err := syscall.Dup(int(holder.Fd()))
	if err != nil {
		t.Fatal(err)
	}
	defer syscall.Close(copied)
	probe, err := os.Open(filepath.Join(job, holderName))
	if err != nil {
		t.Fatal(err)
	}
	defer probe.Close()
	runner.release(job, scratch, lock, holder)
	if err := syscall.Flock(int(probe.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatalf("the released holder stays locked for a copy of its descriptor: %v", err)
	}
}

// A sweep that finds a job free unlocks what it took on the job before it
// closes it, so a copy of those descriptors a child forked meanwhile still
// holds never keeps the next sweep out.
func TestSeizedJobUnlocksWhileACopyOfItsDescriptorsLives(t *testing.T) {
	runner := sweepingRunner(t, completing)
	job, _, lock, holder := claimJob(t, runner, adapterRequest(t, &bytes.Buffer{}), 0)
	lock.Close()
	holder.Close()
	dirs, err := runner.openRunDirectories()
	if err != nil {
		t.Fatal(err)
	}
	defer dirs.close()
	seized, state, _, err := dirs.seize(filepath.Base(job), nil)
	if err != nil || seized == nil {
		t.Fatalf("a free job was not seized: %v %v", state, err)
	}
	copies := []int{}
	for _, handle := range seized.handles {
		copied, err := syscall.Dup(int(handle.Fd()))
		if err != nil {
			t.Fatal(err)
		}
		copies = append(copies, copied)
	}
	defer func() {
		for _, copied := range copies {
			syscall.Close(copied)
		}
	}()
	seized.close()
	for _, name := range []string{".", lockName} {
		probe, err := os.Open(filepath.Join(job, name))
		if err != nil {
			t.Fatal(err)
		}
		err = syscall.Flock(int(probe.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		probe.Close()
		if err != nil {
			t.Fatalf("%s stays locked for a copy of the sweep's descriptor: %v", name, err)
		}
	}
}

// A fork in flight holds syscall.ForkLock for writing until its child has
// executed, and the child holds a copy of the job lock until then. Releasing
// the job waits for that fork, so the job is reaped rather than left for a
// sibling sweep of the same invocation to refuse as still running.
func TestReleaseWaitsForAForkInFlightAndReapsTheJob(t *testing.T) {
	runner := sweepingRunner(t, completing)
	request := adapterRequest(t, &bytes.Buffer{})
	job, scratch, lock, holder := claimJob(t, runner, request, 1)
	syscall.ForkLock.Lock()
	child, err := syscall.Dup(int(lock.Fd()))
	if err != nil {
		syscall.ForkLock.Unlock()
		t.Fatal(err)
	}
	released := make(chan struct{})
	go func() {
		runner.release(job, scratch, lock, holder)
		close(released)
	}()
	select {
	case <-released:
		syscall.Close(child)
		syscall.ForkLock.Unlock()
		t.Fatal("the job was released while a fork that copied its lock was still in flight")
	case <-time.After(200 * time.Millisecond):
	}
	syscall.Close(child)
	syscall.ForkLock.Unlock()
	<-released
	for _, path := range []string{job, scratch} {
		if _, err := os.Lstat(path); !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("%s outlived its release once the fork ended: %v", path, err)
		}
	}
	if err := runner.sweep(request.Context); err != nil {
		t.Fatalf("a sibling sweep refused after the release: %v", err)
	}
}

// An adapter process still exiting when its run ends holds the job lock for a
// moment after its supervisor is reaped. Releasing the job waits for it, with
// its holder still held, so a sibling of the same invocation that sweeps
// meanwhile finds its own live job, and the job is reaped once the process is
// gone instead of being refused as left behind.
func TestReleaseWaitsForAnExitingDescendantAndReapsTheJob(t *testing.T) {
	runner := sweepingRunner(t, completing)
	runner.drain = 10 * time.Second
	ctx, end := runner.BeginInvocation(context.Background())
	defer end()
	runner.invocation = ctx.Value(invocationKey{}).(*invocationScope)
	request := adapterRequest(t, &bytes.Buffer{})
	job, lock, holder, err := runner.claimRun(request)
	if err != nil {
		t.Fatal(err)
	}
	fd, _, errno := syscall.Syscall(syscall.SYS_FCNTL, lock.Fd(), syscall.F_DUPFD_CLOEXEC, 0)
	if errno != 0 {
		t.Fatal(errno)
	}
	exiting := os.NewFile(fd, "exiting")
	defer exiting.Close()
	released := make(chan struct{})
	go func() {
		runner.release(job, "", lock, holder)
		close(released)
	}()
	select {
	case <-released:
		t.Fatal("the job was released while an adapter process still held its lock")
	case <-time.After(200 * time.Millisecond):
	}
	if err := runner.sweep(request.Context); err != nil {
		t.Fatalf("a sibling sweep refused the job its invocation is still releasing: %v", err)
	}
	exiting.Close()
	select {
	case <-released:
	case <-time.After(5 * time.Second):
		t.Fatal("the release never noticed the lock come free")
	}
	if _, err := os.Lstat(job); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("%s outlived its release once its last process ended: %v", job, err)
	}
	if err := runner.sweep(request.Context); err != nil {
		t.Fatalf("a sibling sweep refused after the release: %v", err)
	}
}

// completingBeside completes, leaving a process that inherited the job lock
// but neither protocol channel nor output, which exits a moment after the
// adapter is reaped.
func completingBeside() *exec.Cmd {
	return exec.Command("/bin/sh", "-c", `printf '{"phase":"loaded"}\n' >&3; read -r reply <&4; `+
		`(exec 3>&- 4>&- </dev/null >/dev/null 2>&1; exec sleep 0.3) & `+
		`printf '{"evidence":{"absent":false},"outcome":"changed","phase":"completed"}\n' >&3`)
}

// A run whose adapter process outlives it by a moment leaves no job behind,
// so the next run of its invocation starts instead of refusing that job as
// left by processes that still hold it.
func TestTheNextRunStartsAfterADescendantOutlivesItsRunByAMoment(t *testing.T) {
	runner := sweepingRunner(t, completingBeside)
	runner.drain = 5 * time.Second
	ctx, end := runner.BeginInvocation(context.Background())
	defer end()
	started := time.Now()
	if _, err := runner.Run(ctx, adapterRequest(t, &bytes.Buffer{})); err != nil {
		t.Fatal(err)
	}
	if waited := time.Since(started); waited < 250*time.Millisecond {
		t.Fatalf("the run ended after %v, before the process holding its lock could have exited", waited)
	}
	if jobs := runNames(t, runner.jobParent); len(jobs) != 0 {
		t.Fatalf("the ended run left jobs %v", jobs)
	}
	runner.command = func(string, ...string) *exec.Cmd { return completing() }
	if _, err := runner.Run(ctx, adapterRequest(t, &bytes.Buffer{})); err != nil {
		t.Fatalf("the next run refused the job its predecessor released: %v", err)
	}
}
