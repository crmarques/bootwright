package contextstore

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/crmarques/bootwright/internal/runtime/fs"
	"go.yaml.in/yaml/v3"
)

// The recorded workspace path makes the operator-owned workspace directory the
// single owner of authored YAML: `context init` records its absolute path in
// InputSourceFileName under the shared context directory, every command reads
// the workspace directly through it, and the context directory itself holds
// only Bootwright outputs and state.

type inputSourceRecord struct {
	Path string `yaml:"path"`
}

// InputSourcePath returns the file that records the workspace directory for a
// context base directory.
func InputSourcePath(baseDir string) string {
	return filepath.Join(baseDir, InputSourceFileName)
}

// ResolveWorkspaceDir resolves a workspace path argument to its absolute,
// cleaned form (expanding ~ and relative paths against the caller's
// environment) and verifies it is an existing directory outside the
// Bootwright state directory and not a symlink. Call it before any sudo
// re-exec so the recorded path is the caller's, not root's.
func ResolveWorkspaceDir(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", errors.New("a workspace directory is required; pass -f <dir>")
	}
	resolved, err := cleanPath(path)
	if err != nil {
		return "", err
	}
	root, err := cleanPath(rootDir)
	if err != nil {
		return "", err
	}
	if pathWithinOrSame(resolved, root) {
		return "", fmt.Errorf("workspace directory %s must live outside the Bootwright state directory %s", resolved, root)
	}
	info, err := os.Lstat(resolved)
	if errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("workspace directory %s does not exist", resolved)
	}
	if err != nil {
		return "", fmt.Errorf("stat workspace directory %s: %w", resolved, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("workspace directory %s must not be a symlink", resolved)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("workspace path %s is not a directory", resolved)
	}
	return resolved, nil
}

// WriteInputSource records the absolute workspace directory for ctx in the
// shared context directory, so every sudo-capable administrator resolves the
// same source. Re-writing it is how `context init --yes` re-points a context.
func WriteInputSource(ctx Context, sourceDir string) error {
	if err := ValidateName(ctx.Name); err != nil {
		return err
	}
	source, err := ResolveWorkspaceDir(sourceDir)
	if err != nil {
		return err
	}
	data, err := yaml.Marshal(inputSourceRecord{Path: source})
	if err != nil {
		return fmt.Errorf("marshal %s: %w", InputSourcePath(ctx.BaseDir), err)
	}
	return safefs.AtomicWriteFile(InputSourcePath(ctx.BaseDir), data, 0o600)
}

// ReadInputSource returns the workspace directory recorded for ctx. A context
// without a recorded workspace path (including contexts created before
// workspace recording existed) fails with a named error; the old copied
// input/ directory is never read.
func ReadInputSource(ctx Context) (string, error) {
	path := InputSourcePath(ctx.BaseDir)
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("context %q has no recorded workspace path at %s; re-run `bootwright context init %s -f <workspace>` to point the context at its workspace directory", ctx.Name, path, ctx.Name)
	}
	if err != nil {
		return "", fmt.Errorf("read recorded workspace path %s: %w", path, err)
	}
	var record inputSourceRecord
	if err := yaml.Unmarshal(data, &record); err != nil {
		return "", fmt.Errorf("decode recorded workspace path %s: %w", path, err)
	}
	source := strings.TrimSpace(record.Path)
	if source == "" || !filepath.IsAbs(source) {
		return "", fmt.Errorf("recorded workspace path in %s must be absolute; re-run `bootwright context init %s -f <workspace>`", path, ctx.Name)
	}
	return filepath.Clean(source), nil
}

// ValidateInputSource fails with a named error when the recorded workspace
// directory for ctx is missing, unreadable, or not a directory. There is no
// fallback: commands must not run against a context whose workspace is gone.
func ValidateInputSource(ctx Context) error {
	source := strings.TrimSpace(ctx.InputDir)
	if source == "" {
		return fmt.Errorf("context %q has no recorded workspace path; re-run `bootwright context init %s -f <workspace>`", ctx.Name, ctx.Name)
	}
	fail := func(reason string) error {
		return fmt.Errorf("context %q workspace %s %s; the workspace directory must exist and be readable — if it moved, re-run `bootwright context init %s -f <dir>`", ctx.Name, source, reason, ctx.Name)
	}
	info, err := os.Stat(source)
	if errors.Is(err, os.ErrNotExist) {
		return fail("does not exist")
	}
	if err != nil {
		return fail(fmt.Sprintf("is not readable (%v)", err))
	}
	if !info.IsDir() {
		return fail("is not a directory")
	}
	dir, err := os.Open(source)
	if err != nil {
		return fail(fmt.Sprintf("is not readable (%v)", err))
	}
	_ = dir.Close()
	return nil
}

func pathWithinOrSame(path, root string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}
