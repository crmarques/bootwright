package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func resolvePreferredIDKey() (string, error) {
	raw := strings.TrimSpace(preferredIDKey)
	if raw == "" {
		return "", nil
	}
	path, err := expandPreferredIDKeyPath(raw)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("--preferred-id-key %q: %w", raw, err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("--preferred-id-key %q is not a regular file", raw)
	}
	if info.Mode().Perm()&0o077 != 0 {
		return "", fmt.Errorf("--preferred-id-key %q has mode %#o; OpenSSH refuses a private key readable by group or other, so remove those permissions", raw, info.Mode().Perm())
	}
	return path, nil
}

func expandPreferredIDKeyPath(raw string) (string, error) {
	if strings.HasPrefix(raw, "~") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("--preferred-id-key %q: resolve home directory: %w", raw, err)
		}
		raw = filepath.Join(home, strings.TrimPrefix(strings.TrimPrefix(raw, "~"), "/"))
	}
	path, err := filepath.Abs(raw)
	if err != nil {
		return "", fmt.Errorf("--preferred-id-key %q: %w", raw, err)
	}
	return path, nil
}
