package managedroot

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/crmarques/bootwright/internal/runtime/fs"
)

const (
	MarkerName = ".bootwright-managed-root"
	markerBody = "bootwright managed root\n"
)

func ValidateTarget(path string) (string, error) {
	clean, err := ValidatePath(path)
	if err != nil {
		return "", err
	}
	info, err := os.Lstat(clean)
	if errors.Is(err, os.ErrNotExist) {
		return clean, nil
	}
	if err != nil {
		return "", fmt.Errorf("stat managed root %s: %w", clean, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("managed root %s must be a directory", clean)
	}
	if err := rejectNonEmptyUnmarkedDir(clean); err != nil {
		return "", err
	}
	return clean, nil
}

func ValidatePath(path string) (string, error) {
	clean, err := cleanPath(path)
	if err != nil {
		return "", err
	}
	if err := rejectProtectedPath(clean); err != nil {
		return "", err
	}
	if err := rejectSymlinkAncestors(clean); err != nil {
		return "", err
	}
	return clean, nil
}

func Ensure(path string, mode os.FileMode) (string, error) {
	clean, err := ValidateTarget(path)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(clean, mode); err != nil {
		return "", fmt.Errorf("create managed root %s: %w", clean, err)
	}
	if err := os.Chmod(clean, mode); err != nil {
		return "", fmt.Errorf("chmod managed root %s: %w", clean, err)
	}
	if err := writeMarker(clean); err != nil {
		return "", err
	}
	return clean, nil
}

func Require(path string) (string, error) {
	clean, err := ValidatePath(path)
	if err != nil {
		return "", err
	}
	ok, err := MarkerExists(clean)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", fmt.Errorf("refusing to modify unmarked managed root %s", clean)
	}
	return clean, nil
}

func MarkerExists(path string) (bool, error) {
	marker := filepath.Join(path, MarkerName)
	info, err := os.Lstat(marker)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("stat managed root marker %s: %w", marker, err)
	}
	if !info.Mode().IsRegular() {
		return false, fmt.Errorf("managed root marker %s must be a regular file", marker)
	}
	return true, nil
}

func cleanPath(path string) (string, error) {
	if path == "" {
		return "", errors.New("managed root path is required")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve managed root %s: %w", path, err)
	}
	return filepath.Clean(abs), nil
}

func rejectProtectedPath(path string) error {
	if path == filepath.Clean(string(filepath.Separator)) {
		return errors.New("refusing to use filesystem root as a managed root")
	}
	if home, err := os.UserHomeDir(); err == nil && path == filepath.Clean(home) {
		return fmt.Errorf("refusing to use home directory %s as a managed root", path)
	}
	if cwd, err := os.Getwd(); err == nil && path == filepath.Clean(cwd) {
		return fmt.Errorf("refusing to use current working directory %s as a managed root", path)
	}
	if tmp := os.TempDir(); tmp != "" && path == filepath.Clean(tmp) {
		return fmt.Errorf("refusing to use system temp directory %s as a managed root", path)
	}
	if runtime.GOOS == "windows" {
		return nil
	}
	protected := map[string]struct{}{
		"/bin": {}, "/boot": {}, "/dev": {}, "/etc": {}, "/lib": {}, "/lib64": {},
		"/opt": {}, "/proc": {}, "/run": {}, "/sbin": {}, "/sys": {}, "/usr": {},
		"/usr/bin": {}, "/usr/local": {}, "/var": {}, "/var/tmp": {},
	}
	if _, ok := protected[path]; ok {
		return fmt.Errorf("refusing to use system directory %s as a managed root", path)
	}
	return nil
}

func rejectSymlinkAncestors(path string) error {
	volume := filepath.VolumeName(path)
	rest := path[len(volume):]
	root := volume + string(filepath.Separator)
	current := root
	for _, elem := range splitPath(rest) {
		current = filepath.Join(current, elem)
		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("stat managed root path %s: %w", current, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("managed root path %s must not contain symlink %s", path, current)
		}
	}
	return nil
}

func splitPath(path string) []string {
	clean := filepath.Clean(path)
	clean = strings.TrimPrefix(clean, string(filepath.Separator))
	var out []string
	for _, elem := range strings.Split(clean, string(filepath.Separator)) {
		if elem != "" {
			out = append(out, elem)
		}
	}
	return out
}

func rejectNonEmptyUnmarkedDir(path string) error {
	entries, err := os.ReadDir(path)
	if err != nil {
		return fmt.Errorf("read managed root %s: %w", path, err)
	}
	if len(entries) == 0 {
		return nil
	}
	ok, err := MarkerExists(path)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("managed root %s already exists and is not marked as Bootwright-owned", path)
	}
	return nil
}

func writeMarker(path string) error {
	ok, err := MarkerExists(path)
	if err != nil {
		return err
	}
	if ok {
		return nil
	}
	marker := filepath.Join(path, MarkerName)
	if err := safefs.WriteNewFile(marker, []byte(markerBody), 0o600); err != nil {
		return fmt.Errorf("write managed root marker %s: %w", marker, err)
	}
	return nil
}
