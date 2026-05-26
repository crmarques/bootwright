package secret

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/localroot"
)

const InternalCallerHomeEnv = localroot.CallerHomeEnv

func ResolveKeyFilePath(file, envSourceDir string) (string, error) {
	if file == "" {
		return "", errors.New("file source is empty")
	}
	if strings.HasPrefix(file, "~/") || file == "~" {
		home, err := callerHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home directory: %w", err)
		}
		if file == "~" {
			return home, nil
		}
		return filepath.Join(home, file[2:]), nil
	}
	if filepath.IsAbs(file) {
		return filepath.Clean(file), nil
	}
	if envSourceDir == "" || envSourceDir == "." {
		abs, err := filepath.Abs(file)
		if err != nil {
			return "", fmt.Errorf("resolve %s: %w", file, err)
		}
		return abs, nil
	}
	return filepath.Clean(filepath.Join(envSourceDir, file)), nil
}

func callerHomeDir() (string, error) {
	if home, ok := localroot.CallerHomeDir(); ok {
		return home, nil
	}
	return os.UserHomeDir()
}

// ResolvePath returns the path for a declared SecretRef: file-sourced secrets
// resolve to their declared path; context-backed and generated secrets resolve
// inside secretsDir.
func ResolvePath(name string, env *v1alpha1.Environment, secretsDir string) string {
	if name == "" {
		return ""
	}
	if env != nil {
		if secret, ok := env.Spec.Secrets[name]; ok && secret.File != "" {
			envSourceDir := filepath.Dir(env.SourcePath)
			if path, err := ResolveKeyFilePath(secret.File, envSourceDir); err == nil {
				return path
			}
		}
	}
	return filepath.Join(secretsDir, name)
}

func ResolveTLSKeyPath(name string, env *v1alpha1.Environment, secretsDir string) string {
	if name == "" {
		return ""
	}
	if env != nil {
		if secret, ok := env.Spec.Secrets[name]; ok && secret.KeyFile != "" {
			envSourceDir := filepath.Dir(env.SourcePath)
			if path, err := ResolveKeyFilePath(secret.KeyFile, envSourceDir); err == nil {
				return path
			}
		}
	}
	return filepath.Join(secretsDir, name+".key")
}
