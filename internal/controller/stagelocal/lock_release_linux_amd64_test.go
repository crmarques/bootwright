//go:build linux && amd64

package stagelocal

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// A fork in flight holds syscall.ForkLock for writing until its child has
// executed, and the child holds a copy of the automation lock until then.
// Releasing the automation waits for that fork, so the tree goes with its
// invocation rather than being left for a later sweep.
func TestAutomationReleaseWaitsForAForkInFlightAndRemovesTheTree(t *testing.T) {
	staged, err := NewAutomationStager(stagingAt(t)).Stage(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	syscall.ForkLock.Lock()
	child, err := syscall.Dup(int(staged.Lock.(*os.File).Fd()))
	if err != nil {
		syscall.ForkLock.Unlock()
		t.Fatal(err)
	}
	released := make(chan struct{})
	go func() {
		staged.Release()
		close(released)
	}()
	select {
	case <-released:
		syscall.Close(child)
		syscall.ForkLock.Unlock()
		t.Fatal("the automation was released while a fork that copied its lock was still in flight")
	case <-time.After(200 * time.Millisecond):
	}
	syscall.Close(child)
	syscall.ForkLock.Unlock()
	<-released
	if exists(filepath.Dir(staged.Path)) {
		t.Fatal("the tree outlived its release once the fork ended")
	}
}

// Releasing a stage unlocks it before closing it, so a copy of its lock
// descriptor a child forked meanwhile still holds never keeps the lock held.
func TestStageReleaseUnlocksWhileACopyOfItsDescriptorLives(t *testing.T) {
	parent, made, err := NewStaging(filepath.Join(t.TempDir(), "staging")).open("native")
	if err != nil {
		t.Fatal(err)
	}
	defer parent.close()
	probe, err := os.Open(filepath.Join(parent.path, made.name, stageLock))
	if err != nil {
		t.Fatal(err)
	}
	defer probe.Close()
	copied, err := syscall.Dup(int(made.lock.Fd()))
	if err != nil {
		t.Fatal(err)
	}
	defer syscall.Close(copied)
	parent.release(made.name, made.lock, made.held)
	if exists(filepath.Join(parent.path, made.name)) {
		t.Fatal("the released stage was not removed")
	}
	if err := syscall.Flock(int(probe.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatalf("the released stage stays locked for a copy of its descriptor: %v", err)
	}
}
