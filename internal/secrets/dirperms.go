package secret

import (
	"fmt"
	"os"
)

func DirPermsWarning(secretsDir string) string {
	if secretsDir == "" {
		return ""
	}
	info, err := os.Stat(secretsDir)
	if err != nil || !info.IsDir() {
		return ""
	}
	mode := info.Mode().Perm()
	if mode == 0o700 {
		return ""
	}
	return fmt.Sprintf("%s has mode %#o; expected 0700 (run chmod 0700 %s)", secretsDir, mode, secretsDir)
}

func StrictDirCheck(secretsDir string) error {
	if secretsDir == "" {
		return nil
	}
	info, err := os.Stat(secretsDir)
	if err != nil {
		return fmt.Errorf("strict-secrets: cannot stat secrets-dir %s: %v", secretsDir, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("strict-secrets: secrets-dir %s is not a directory", secretsDir)
	}
	if mode := info.Mode().Perm(); mode != 0o700 {
		return fmt.Errorf("strict-secrets: secrets-dir %s has mode %#o; required 0700", secretsDir, mode)
	}
	entries, err := os.ReadDir(secretsDir)
	if err != nil {
		return fmt.Errorf("strict-secrets: cannot read secrets-dir %s: %v", secretsDir, err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		fi, err := entry.Info()
		if err != nil {
			return fmt.Errorf("strict-secrets: cannot stat %s/%s: %v", secretsDir, entry.Name(), err)
		}
		if mode := fi.Mode().Perm(); mode != 0o600 {
			return fmt.Errorf("strict-secrets: %s/%s has mode %#o; required 0600", secretsDir, entry.Name(), mode)
		}
	}
	return nil
}
