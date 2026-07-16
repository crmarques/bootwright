//go:build unix

package execution

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestExitCodeMapsSignal(t *testing.T) {
	err := OSRunner{}.Run(context.Background(), Command{
		Name: "/bin/sh",
		Args: []string{"-c", "kill -TERM $$"},
	})
	code, ok := ExitCode(err)
	if !ok {
		t.Fatalf("ExitCode did not recognize %v", err)
	}
	if code != 143 {
		t.Fatalf("exit code = %d, want 143", code)
	}
}

func TestGracefulCancelLetsChildHandleTermination(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	readyPath := filepath.Join(t.TempDir(), "ready")
	done := make(chan error, 1)
	go func() {
		done <- OSRunner{}.Run(ctx, Command{
			Name:           "/bin/sh",
			Args:           []string{"-c", `trap 'exit 42' TERM; : > "$1"; while :; do :; done`, "sh", readyPath},
			GracefulCancel: true,
			WaitDelay:      time.Second,
		})
	}()
	deadline := time.After(5 * time.Second)
	for {
		if _, err := os.Stat(readyPath); err == nil {
			break
		} else if !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("stat readiness file: %v", err)
		}
		select {
		case err := <-done:
			t.Fatalf("child exited before reporting readiness: %v", err)
		case <-deadline:
			t.Fatal("child did not report readiness")
		case <-time.After(time.Millisecond):
		}
	}
	cancel()
	err := <-done
	code, ok := ExitCode(err)
	if !ok {
		t.Fatalf("ExitCode did not recognize %v", err)
	}
	if code != 42 {
		t.Fatalf("exit code = %d, want 42", code)
	}
}
