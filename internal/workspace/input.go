package workspace

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/crmarques/bootwright/internal/host/safefs"
)

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

func ReplaceInputDir(ctx Context, sourceDir string) error {
	return ReplaceInputDirWithAddons(ctx, sourceDir, nil)
}

func ReplaceInputDirWithAddons(ctx Context, sourceDir string, addonDirs map[string]string) error {
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
	if err := copyAddonDirs(staging, addonDirs); err != nil {
		_ = os.RemoveAll(staging)
		return err
	}
	aside := staging + ".old"
	oldMoved := false
	if _, err := os.Lstat(ctx.InputDir); err == nil {
		if err := os.Rename(ctx.InputDir, aside); err != nil {
			_ = os.RemoveAll(staging)
			return fmt.Errorf("move existing input directory %s aside: %w", ctx.InputDir, err)
		}
		oldMoved = true
	} else if !errors.Is(err, os.ErrNotExist) {
		_ = os.RemoveAll(staging)
		return fmt.Errorf("stat input directory %s: %w", ctx.InputDir, err)
	}
	if err := os.Rename(staging, ctx.InputDir); err != nil {
		if oldMoved {
			_ = os.Rename(aside, ctx.InputDir)
		}
		_ = os.RemoveAll(staging)
		return fmt.Errorf("install input directory %s: %w", ctx.InputDir, err)
	}
	if oldMoved {
		_ = os.RemoveAll(aside)
	}
	return nil
}

func copyAddonDirs(staging string, addonDirs map[string]string) error {
	if len(addonDirs) == 0 {
		return nil
	}
	root := filepath.Join(staging, "add-ons", "_store")
	if err := os.MkdirAll(root, 0o700); err != nil {
		return fmt.Errorf("create %s: %w", root, err)
	}
	for name, dir := range addonDirs {
		if name == "" || name != filepath.Base(name) || strings.HasPrefix(name, ".") {
			return fmt.Errorf("registered add-on name %q is not a plain path segment", name)
		}
		dst := filepath.Join(root, name)
		if err := os.Mkdir(dst, 0o700); err != nil {
			return fmt.Errorf("create %s: %w", dst, err)
		}
		if err := copyInputTree(dir, dst); err != nil {
			return err
		}
	}
	return nil
}

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
		f, err := os.OpenFile(srcPath, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
		if err != nil {
			return fmt.Errorf("open %s: %w", srcPath, err)
		}
		data, err := io.ReadAll(f)
		_ = f.Close()
		if err != nil {
			return fmt.Errorf("read %s: %w", srcPath, err)
		}
		if err := safefs.AtomicWriteFile(dstPath, data, 0o600); err != nil {
			return err
		}
	}
	return nil
}

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
