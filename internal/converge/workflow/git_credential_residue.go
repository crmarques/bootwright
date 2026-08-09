package workflow

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func SweepGitCredentialResidue(runsDir string) error {
	gitDir := filepath.Join(runsDir, "content", "git")
	entries, err := os.ReadDir(gitDir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read git content cache for credential residue: %w", err)
	}
	var problems []error
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), "git-key-") && !strings.HasPrefix(entry.Name(), "git-cred-") {
			continue
		}
		path := filepath.Join(gitDir, entry.Name())
		if err := os.RemoveAll(path); err != nil {
			problems = append(problems, fmt.Errorf("remove temporary git credential residue %s: %w", path, err))
		}
	}
	return errors.Join(problems...)
}
