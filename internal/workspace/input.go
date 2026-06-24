package workspace

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/crmarques/bootwright/internal/host/safefs"
)

// A context owns its input: `context init` and `context update` copy the
// operator's source directory into ctx.InputDir under the shared context
// directory, so every command reads the copy and the context survives deletion
// of the source. The whole tree is copied (not just YAML) because file:-sourced
// secrets and SSH keys resolve relative to the loaded YAML's directory, so
// referenced non-YAML material must come along.

// ResolveWorkspaceDir resolves a source path argument to its absolute, cleaned
// form (expanding ~ and relative paths against the caller's environment) and
// verifies it is an existing directory outside the Bootwright state directory
// and not a symlink. Call it before any sudo re-exec so the source is resolved
// against the caller's environment, not root's.
func ResolveWorkspaceDir(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", errors.New("a source directory is required; pass -f <dir>")
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
		return "", fmt.Errorf("source directory %s must live outside the Bootwright state directory %s", resolved, root)
	}
	info, err := os.Lstat(resolved)
	if errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("source directory %s does not exist", resolved)
	}
	if err != nil {
		return "", fmt.Errorf("stat source directory %s: %w", resolved, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("source directory %s must not be a symlink", resolved)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("source path %s is not a directory", resolved)
	}
	return resolved, nil
}

// ReplaceInputDir rebuilds ctx.InputDir from sourceDir, copying the whole tree
// so the context is self-contained. It is crash-safe: the new tree is built in
// a sibling staging directory under BaseDir (same filesystem, so the swap is an
// atomic rename) and installed only once fully populated. Symlinks are
// rejected; VCS and dependency directories are skipped; copied directories are
// 0700 and files 0600 under the root-managed 0700 tree.
func ReplaceInputDir(ctx Context, sourceDir string) error {
	if err := ValidateName(ctx.Name); err != nil {
		return err
	}
	if strings.TrimSpace(ctx.InputDir) == "" {
		return fmt.Errorf("context %q has no input directory", ctx.Name)
	}
	source, err := ResolveWorkspaceDir(sourceDir)
	if err != nil {
		return err
	}
	staging, err := os.MkdirTemp(ctx.BaseDir, ".input.tmp-")
	if err != nil {
		return fmt.Errorf("create input staging directory under %s: %w", ctx.BaseDir, err)
	}
	if err := copyInputTree(source, staging); err != nil {
		_ = os.RemoveAll(staging)
		return err
	}
	if err := os.RemoveAll(ctx.InputDir); err != nil {
		_ = os.RemoveAll(staging)
		return fmt.Errorf("reset input directory %s: %w", ctx.InputDir, err)
	}
	if err := os.Rename(staging, ctx.InputDir); err != nil {
		_ = os.RemoveAll(staging)
		return fmt.Errorf("install input directory %s: %w", ctx.InputDir, err)
	}
	return nil
}

// copyInputTree recursively copies src into dst, skipping VCS and dependency
// directories, rejecting symlinks, and writing directories 0700 / files 0600.
func copyInputTree(src, dst string) error {
	entries, err := os.ReadDir(src)
	if err != nil {
		return fmt.Errorf("read %s: %w", src, err)
	}
	for _, entry := range entries {
		name := entry.Name()
		srcPath := filepath.Join(src, name)
		dstPath := filepath.Join(dst, name)
		info, err := os.Lstat(srcPath)
		if err != nil {
			return fmt.Errorf("stat %s: %w", srcPath, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("input source %s must not contain symlinks", srcPath)
		}
		if info.IsDir() {
			if skipInputDir(name) {
				continue
			}
			if err := os.Mkdir(dstPath, 0o700); err != nil {
				return fmt.Errorf("create %s: %w", dstPath, err)
			}
			if err := copyInputTree(srcPath, dstPath); err != nil {
				return err
			}
			continue
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("input source %s is not a regular file", srcPath)
		}
		data, err := os.ReadFile(srcPath)
		if err != nil {
			return fmt.Errorf("read %s: %w", srcPath, err)
		}
		if err := safefs.AtomicWriteFile(dstPath, data, 0o600); err != nil {
			return err
		}
	}
	return nil
}

// skipInputDir mirrors the desired-state loader's directory traversal: dot
// directories (including .git) and dependency directories are never part of the
// authored input set, so they are not copied into the context.
func skipInputDir(name string) bool {
	if strings.HasPrefix(name, ".") {
		return true
	}
	switch name {
	case "node_modules", "vendor":
		return true
	}
	return false
}

// ValidateInputDir fails with a named error when ctx.InputDir — the owned copy
// of the operator's source — is missing or unreadable. init/update always
// populate it, so a missing input directory means a half-created or corrupted
// context, with a copy-in remediation rather than a missing external workspace.
func ValidateInputDir(ctx Context) error {
	dir := strings.TrimSpace(ctx.InputDir)
	if dir == "" {
		return fmt.Errorf("context %q has no input directory; re-run `bootwright context init --name %s -f <dir>`", ctx.Name, ctx.Name)
	}
	fail := func(reason string) error {
		return fmt.Errorf("context %q input directory %s %s; re-run `bootwright context update --name %s -f <dir>` or `bootwright context init --name %s -f <dir> --yes` to repopulate it", ctx.Name, dir, reason, ctx.Name, ctx.Name)
	}
	info, err := os.Stat(dir)
	if errors.Is(err, os.ErrNotExist) {
		return fail("is missing")
	}
	if err != nil {
		return fail(fmt.Sprintf("is not readable (%v)", err))
	}
	if !info.IsDir() {
		return fail("is not a directory")
	}
	f, err := os.Open(dir)
	if err != nil {
		return fail(fmt.Sprintf("is not readable (%v)", err))
	}
	_ = f.Close()
	return nil
}

func pathWithinOrSame(path, root string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}
