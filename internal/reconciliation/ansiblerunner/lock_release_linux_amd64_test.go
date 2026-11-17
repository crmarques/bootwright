//go:build linux && amd64

package ansiblerunner

import (
	"bytes"
	"os"
	"path/filepath"
	"syscall"
	"testing"
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
