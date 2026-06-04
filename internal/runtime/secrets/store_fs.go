package secret

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"
	"syscall"
)

func validateMaterialKey(key MaterialKey) error {
	if key.Name == "" {
		return errors.New("secret material name is empty")
	}
	if !validContextSecretName(key.Name) {
		return fmt.Errorf("secret material name %q must be a lowercase DNS label", key.Name)
	}
	switch key.Role {
	case MaterialPrimary, MaterialTLSKey, MaterialSSHPrivate, MaterialSSHPublic:
		return nil
	default:
		return fmt.Errorf("unsupported secret material role %q", key.Role)
	}
}

func validContextSecretName(name string) bool {
	if name == "" || len(name) > 253 {
		return false
	}
	parts := strings.Split(name, "-")
	for _, part := range parts {
		if part == "" {
			return false
		}
		for i, r := range part {
			switch {
			case r >= 'a' && r <= 'z':
			case r >= '0' && r <= '9':
				if i == 0 && len(part) == 1 {
					return true
				}
			default:
				return false
			}
		}
	}
	return true
}

func materialKeyFromFileName(name string) (MaterialKey, bool) {
	if strings.HasPrefix(name, ".") || strings.ContainsRune(name, os.PathSeparator) {
		return MaterialKey{}, false
	}
	switch {
	case strings.HasSuffix(name, ".key"):
		secretName := strings.TrimSuffix(name, ".key")
		if !validContextSecretName(secretName) {
			return MaterialKey{}, false
		}
		return MaterialKey{Name: secretName, Role: MaterialTLSKey}, true
	case strings.HasSuffix(name, ".pub"):
		secretName := strings.TrimSuffix(name, ".pub")
		if !validContextSecretName(secretName) {
			return MaterialKey{}, false
		}
		return MaterialKey{Name: secretName, Role: MaterialSSHPublic}, true
	default:
		if !validContextSecretName(name) {
			return MaterialKey{}, false
		}
		return MaterialKey{Name: name, Role: MaterialPrimary}, true
	}
}

func materialLabel(key MaterialKey) string {
	return fmt.Sprintf("%s/%s", key.Name, key.Role)
}

func ensureOwnedDir(path string, ownerUID int, mode fs.FileMode) error {
	if err := os.MkdirAll(path, mode); err != nil {
		return fmt.Errorf("create directory %s: %w", path, err)
	}
	if err := os.Chmod(path, mode); err != nil {
		return fmt.Errorf("chmod directory %s: %w", path, err)
	}
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("stat directory %s: %w", path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return unsafePathError{message: fmt.Sprintf("%s is a symlink; expected directory", path)}
	}
	if !info.IsDir() {
		return unsafePathError{message: fmt.Sprintf("%s is not a directory", path)}
	}
	if actual := info.Mode().Perm(); actual != mode {
		return unsafePathError{message: fmt.Sprintf("%s has mode %04o; expected %04o", path, actual, mode)}
	}
	if err := ensureOwner(info, path, ownerUID); err != nil {
		return err
	}
	return nil
}

func ensureRegularOwnedFile(path string, ownerUID int, mode fs.FileMode) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return unsafePathError{message: fmt.Sprintf("%s is a symlink; expected regular file", path)}
	}
	if !info.Mode().IsRegular() {
		return unsafePathError{message: fmt.Sprintf("%s is not a regular file", path)}
	}
	if actual := info.Mode().Perm(); actual != mode {
		return unsafePathError{message: fmt.Sprintf("%s has mode %04o; expected %04o", path, actual, mode)}
	}
	if err := ensureOwner(info, path, ownerUID); err != nil {
		return err
	}
	return nil
}

func ensureOwner(info os.FileInfo, path string, ownerUID int) error {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return unsafePathError{message: fmt.Sprintf("cannot determine owner for %s", path)}
	}
	if int(stat.Uid) != ownerUID {
		return unsafePathError{message: fmt.Sprintf("%s is owned by uid %d; expected uid %d", path, stat.Uid, ownerUID)}
	}
	return nil
}

func refuseSymlink(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return unsafePathError{message: fmt.Sprintf("%s is a symlink; expected regular file", path)}
	}
	return nil
}

func writeNewOwnedFile(path string, data []byte, mode fs.FileMode) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return fmt.Errorf("create %s: %w", path, err)
	}
	if err := file.Chmod(mode); err != nil {
		_ = file.Close()
		return fmt.Errorf("chmod %s: %w", path, err)
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

type unsafePathError struct {
	message string
}

func (e unsafePathError) Error() string {
	return e.message
}
