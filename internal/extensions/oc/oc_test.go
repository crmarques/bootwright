package oc

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCommandRunnerReportsLogAppendError(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "blocker")
	if err := os.WriteFile(blocker, []byte("not a directory\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runner := CommandRunner{
		Command: "true",
		LogPath: filepath.Join(blocker, "oc.log"),
	}

	out, err := runner.Run(context.Background(), "/tmp/kubeconfig", []string{"get", "namespace"}, nil)
	if err == nil {
		t.Fatal("Run succeeded despite unwritable log path")
	}
	if len(out) != 0 {
		t.Fatalf("Run output = %q, want empty", out)
	}
	if got := err.Error(); !strings.Contains(got, "append oc log") || !strings.Contains(got, "not a directory") {
		t.Fatalf("Run error did not explain log append failure: %v", err)
	}
}

func TestCommandRunnerReportsCommandAndLogErrors(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "blocker")
	if err := os.WriteFile(blocker, []byte("not a directory\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runner := CommandRunner{
		Command: "false",
		LogPath: filepath.Join(blocker, "oc.log"),
	}

	_, err := runner.Run(context.Background(), "/tmp/kubeconfig", []string{"get", "namespace"}, nil)
	if err == nil {
		t.Fatal("Run succeeded despite command and log failures")
	}
	if got := err.Error(); !strings.Contains(got, "run false") || !strings.Contains(got, "also failed to append oc log") {
		t.Fatalf("Run error did not include command and log failures: %v", err)
	}
}
