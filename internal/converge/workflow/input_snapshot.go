package workflow

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/crmarques/bootwright/internal/runtime/fs"
)

// RunInputSnapshotDir returns the directory where a mutating apply run records
// the input YAML it was launched from, next to the run's other per-run
// artifacts: <runs>/history/<run-id>/input.
func RunInputSnapshotDir(runsDir, runID string) string {
	return filepath.Join(runsDir, "history", runID, "input")
}

// LastDestroyInputSnapshotDir returns the rolling snapshot directory used by
// mutating destroy runs. Destroy executes through workflow.Run, which mints
// its run ID internally for the lease only and keeps no per-run history
// directory, so its snapshot lands in a fixed location under the runs dir.
func LastDestroyInputSnapshotDir(runsDir string) string {
	return filepath.Join(runsDir, "last-destroy-input")
}

// SnapshotRunInput copies the loaded input YAML files into snapshotDir as a
// forensic record of what a mutating run was launched from. The snapshot is
// an output only; nothing reads it back. Layout under snapshotDir mirrors the
// files' paths relative to sourceRoot (the context workspace); files outside
// sourceRoot fall back to a unique base name.
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
