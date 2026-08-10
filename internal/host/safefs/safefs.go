package safefs

import (
	"fmt"
	"os"
	"path/filepath"
)

func RegularFileExists(path string) (bool, error) {
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("stat %s: %w", path, err)
	}
	if info.IsDir() {
		return false, fmt.Errorf("%s is a directory; expected a file", path)
	}
	return true, nil
}

func WriteNewFile(path string, data []byte, mode os.FileMode) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return fmt.Errorf("create %s: %w", path, err)
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return fmt.Errorf("sync %s: %w", path, err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close %s: %w", path, err)
	}
	return nil
}

func EnsureDir(path string, mode os.FileMode) error {
	clean := filepath.Clean(path)
	var missing []string
	for current := clean; ; current = filepath.Dir(current) {
		info, err := os.Stat(current)
		if err == nil {
			if !info.IsDir() {
				return fmt.Errorf("%s is not a directory", current)
			}
			break
		}
		if !os.IsNotExist(err) {
			return fmt.Errorf("stat directory %s: %w", current, err)
		}
		missing = append(missing, current)
		parent := filepath.Dir(current)
		if parent == current {
			return fmt.Errorf("find existing parent directory for %s", clean)
		}
	}
	if err := os.MkdirAll(path, mode); err != nil {
		return fmt.Errorf("create directory %s: %w", path, err)
	}
	if err := os.Chmod(path, mode); err != nil {
		return fmt.Errorf("chmod directory %s: %w", path, err)
	}
	for i := len(missing) - 1; i >= 0; i-- {
		if err := syncDirectory(filepath.Dir(missing[i])); err != nil {
			return fmt.Errorf("persist directory %s: %w", missing[i], err)
		}
	}
	return nil
}

func WriteFileEnsuringDir(path string, data []byte, mode os.FileMode) error {
	if err := EnsureDir(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return AtomicWriteFile(path, data, mode)
}

func AtomicWriteFile(path string, data []byte, mode os.FileMode) error {
	file, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-")
	if err != nil {
		return fmt.Errorf("create temp file for %s: %w", path, err)
	}
	tempPath := file.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tempPath)
		}
	}()
	if err := file.Chmod(mode); err != nil {
		_ = file.Close()
		return fmt.Errorf("chmod temp file for %s: %w", path, err)
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return fmt.Errorf("write temp file for %s: %w", path, err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return fmt.Errorf("sync temp file for %s: %w", path, err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close temp file for %s: %w", path, err)
	}
	if err := os.Rename(tempPath, path); err != nil {
		return fmt.Errorf("replace %s: %w", path, err)
	}
	if err := syncDirectory(filepath.Dir(path)); err != nil {
		return fmt.Errorf("persist parent directory for %s: %w", path, err)
	}
	cleanup = false
	return nil
}

func RemoveFileDurable(path string) error {
	if err := os.Remove(path); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("remove %s: %w", path, err)
	}
	if err := syncDirectory(filepath.Dir(path)); err != nil {
		return fmt.Errorf("persist parent directory for %s: %w", path, err)
	}
	return nil
}

func syncDirectory(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open directory %s: %w", path, err)
	}
	if err := dir.Sync(); err != nil {
		_ = dir.Close()
		return fmt.Errorf("sync directory %s: %w", path, err)
	}
	if err := dir.Close(); err != nil {
		return fmt.Errorf("close directory %s: %w", path, err)
	}
	return nil
}
