//go:build linux

package nativefs

import (
	"errors"
	"syscall"
	"testing"
)

// A resolution the kernel refused with EAGAIN is retried rather than reported.
// openat2 answers EAGAIN when a rename moves part of the path while it resolves
// under RESOLVE_BENEATH, which a concurrent writer does constantly; a caller
// that reported it failed whole operations at random.
func TestAResolutionRacedByARenameIsRetried(t *testing.T) {
	refusals := 0
	fd, err := retry(func() (int, error) {
		if refusals < 3 {
			refusals++
			return -1, syscall.EAGAIN
		}
		return 7, nil
	})
	if refusals != 3 {
		t.Fatalf("the injected race did not fire: %d refusals", refusals)
	}
	if fd != 7 || err != nil {
		t.Fatalf("a retried resolution did not return its own answer: %v %v", fd, err)
	}
}

// The retry is bounded, so a tree something renames without pause fails instead
// of spinning forever.
func TestAResolutionThatNeverSettlesStillFails(t *testing.T) {
	attempts := 0
	_, err := retry(func() (int, error) {
		attempts++
		return -1, syscall.EAGAIN
	})
	if !errors.Is(err, syscall.EAGAIN) {
		t.Fatalf("an unsettled resolution hid its refusal: %v", err)
	}
	if attempts != resolutionRetries+1 {
		t.Fatalf("the retry bound was not honored: %d attempts", attempts)
	}
}

// Every other refusal is the caller's to report, so nothing else is retried.
func TestARefusalThatIsNotARaceIsReportedAtOnce(t *testing.T) {
	for _, refusal := range []error{syscall.ENOENT, syscall.ELOOP, syscall.EXDEV, syscall.EACCES} {
		attempts := 0
		_, err := retry(func() (int, error) {
			attempts++
			return -1, refusal
		})
		if !errors.Is(err, refusal) || attempts != 1 {
			t.Fatalf("%v was retried or rewritten: %d attempts, %v", refusal, attempts, err)
		}
	}
}
