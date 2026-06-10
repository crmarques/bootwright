package become

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/crmarques/bootwright/internal/host/safefs"
)

// InheritedPasswordFile returns the BECOME password file handed down by a
// prompted parent sudo session, or "" when no usable file was inherited.
func InheritedPasswordFile() string {
	if strings.TrimSpace(os.Getenv(SudoAuthEnv)) != AuthPrompted {
		return ""
	}
	return strings.TrimSpace(os.Getenv(PasswordFileEnv))
}

func ReadPasswordFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read inherited BECOME password file: %w", err)
	}
	password := strings.TrimRight(string(data), "\r\n")
	if password == "" {
		return "", errors.New("inherited BECOME password file is empty")
	}
	return password, nil
}

func WritePasswordFile(password string) (string, func(), error) {
	if password == "" {
		return "", nil, errors.New("BECOME password cannot be empty")
	}
	// Materialize the plaintext BECOME password under a 0700 per-run directory
	// (not bare in the shared, world-writable /tmp), mirroring the secret-staging
	// path and the spec's short-lived-plaintext contract: 0700 directory, 0600
	// file, removed by cleanup.
	dir, err := os.MkdirTemp("", "bootwright-become-")
	if err != nil {
		return "", nil, fmt.Errorf("create BECOME password directory: %w", err)
	}
	cleanup := func() { _ = os.RemoveAll(dir) }
	if err := os.Chmod(dir, 0o700); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("chmod BECOME password directory: %w", err)
	}
	path := filepath.Join(dir, "become-pass")
	if err := safefs.WriteNewFile(path, []byte(password+"\n"), 0o600); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("write BECOME password file: %w", err)
	}
	return path, cleanup, nil
}
