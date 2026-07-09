package workflow

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/crmarques/bootwright/internal/host/safefs"
)

func RunInputSnapshotDir(runsDir, runID string) string {
	return filepath.Join(runsDir, "history", runID, "input")
}

func LastDestroyInputSnapshotDir(runsDir string) string {
	return filepath.Join(runsDir, "last-destroy-input")
}

func SnapshotRunInput(snapshotDir, sourceRoot string, files []string) error {
	if strings.TrimSpace(snapshotDir) == "" {
		return errors.New("input snapshot dir is required")
	}
	if err := os.RemoveAll(snapshotDir); err != nil {
		return fmt.Errorf("reset input snapshot %s: %w", snapshotDir, err)
	}
	if err := os.MkdirAll(snapshotDir, 0o700); err != nil {
		return fmt.Errorf("create input snapshot %s: %w", snapshotDir, err)
	}
	used := map[string]bool{}
	for _, file := range files {
		rel := snapshotRelPath(sourceRoot, file)
		for i := 1; used[rel]; i++ {
			rel = filepath.Join("external", fmt.Sprintf("%d-%s", i, filepath.Base(file)))
		}
		used[rel] = true
		target := filepath.Join(snapshotDir, rel)
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return fmt.Errorf("create input snapshot directory %s: %w", filepath.Dir(target), err)
		}
		data, err := os.ReadFile(file)
		if err != nil {
			return fmt.Errorf("read input file %s for snapshot: %w", file, err)
		}
		if err := safefs.AtomicWriteFile(target, data, 0o600); err != nil {
			return fmt.Errorf("write input snapshot %s: %w", target, err)
		}
	}
	return nil
}

func snapshotRelPath(root, file string) string {
	root = strings.TrimSpace(root)
	if root != "" {
		if rel, err := filepath.Rel(root, file); err == nil &&
			rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel) {
			return rel
		}
	}
	return filepath.Base(file)
}
