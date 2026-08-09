package workflow

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestCommandLeaseSweepsGitCredentialResidueAndKeepsContent(t *testing.T) {
	runsDir := t.TempDir()
	gitDir := filepath.Join(runsDir, "content", "git")
	for _, name := range []string{"git-key-crash", "git-cred-crash", "resolved-commit"} {
		if err := os.MkdirAll(filepath.Join(gitDir, name), 0o700); err != nil {
			t.Fatalf("create cache entry %s: %v", name, err)
		}
	}
	guard, err := AcquireCommandRunLease(context.Background(), runsDir, "destroy")
	if err != nil {
		t.Fatalf("AcquireCommandRunLease: %v", err)
	}
	defer guard.Close()
	for _, pattern := range []string{"git-key-*", "git-cred-*"} {
		matches, err := filepath.Glob(filepath.Join(gitDir, pattern))
		if err != nil {
			t.Fatalf("glob credential residue: %v", err)
		}
		if len(matches) != 0 {
			t.Fatalf("command lease retained temporary git credentials: %v", matches)
		}
	}
	if _, err := os.Stat(filepath.Join(gitDir, "resolved-commit")); err != nil {
		t.Fatalf("credential sweep removed fetched content: %v", err)
	}
}
