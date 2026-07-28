package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/crmarques/bootwright/internal/host/localroot"
)

func resolvePreferredSSHIDKey() (string, error) {
	raw := strings.TrimSpace(preferredSSHIDKey)
	if raw == "" {
		return "", nil
	}
	path, err := expandPreferredSSHIDKeyPath(raw)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("--preferred-ssh-id-key %q: %w", raw, err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("--preferred-ssh-id-key %q is not a regular file", raw)
	}
	if info.Mode().Perm()&0o077 != 0 {
		return "", fmt.Errorf("--preferred-ssh-id-key %q has mode %#o; OpenSSH refuses a private key readable by group or other, so remove those permissions", raw, info.Mode().Perm())
	}
	return path, nil
}

func expandPreferredSSHIDKeyPath(raw string) (string, error) {
	if strings.HasPrefix(raw, "~") {
		home, err := operatorHomeDir()
		if err != nil {
			return "", fmt.Errorf("--preferred-ssh-id-key %q: resolve home directory: %w", raw, err)
		}
		raw = filepath.Join(home, strings.TrimPrefix(strings.TrimPrefix(raw, "~"), "/"))
	}
	path, err := filepath.Abs(raw)
	if err != nil {
		return "", fmt.Errorf("--preferred-ssh-id-key %q: %w", raw, err)
	}
	return path, nil
}

func operatorHomeDir() (string, error) {
	if home, ok := localroot.CallerHomeDir(); ok {
		return home, nil
	}
	return os.UserHomeDir()
}
