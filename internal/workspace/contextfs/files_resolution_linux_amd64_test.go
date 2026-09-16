//go:build linux && amd64

package contextfs

import (
	"errors"
	"os"
	"syscall"
	"testing"
)

// A resolution the kernel refused with EAGAIN is retried rather than reported.
// openat2 answers EAGAIN when a rename moves part of the path while it resolves
// under RESOLVE_BENEATH, which a concurrent writer does constantly; a caller
// that reported it failed whole operations at random.
func TestAResolutionRacedByARenameIsRetried(t *testing.T) {
	refusals := 0
	file, err := retryResolution(func() (*os.File, error) {
		if refusals < 3 {
			refusals++
			return nil, syscall.EAGAIN
		}
		return nil, nil
	})
	if refusals != 3 {
		t.Fatalf("the injected race did not fire: %d refusals", refusals)
	}
	if file != nil || err != nil {
		t.Fatalf("a retried resolution did not return its own answer: %v %v", file, err)
	}
}

// The retry is bounded, so a tree something renames without pause fails instead
// of spinning forever.
func TestAResolutionThatNeverSettlesStillFails(t *testing.T) {
	attempts := 0
	_, err := retryResolution(func() (*os.File, error) {
		attempts++
		return nil, syscall.EAGAIN
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
		_, err := retryResolution(func() (*os.File, error) {
			attempts++
			return nil, refusal
		})
		if !errors.Is(err, refusal) || attempts != 1 {
			t.Fatalf("%v was retried or rewritten: %d attempts, %v", refusal, attempts, err)
		}
	}
}
