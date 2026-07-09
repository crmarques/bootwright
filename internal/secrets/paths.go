package secret

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/crmarques/bootwright/internal/host/localroot"
)

const InternalCallerHomeEnv = localroot.CallerHomeEnv

const PlaceholderSecretsDir = "\x00bootwright-placeholder-secrets\x00"

func IsPlaceholderSecretsDir(secretsDir string) bool {
	return secretsDir == PlaceholderSecretsDir
}

func SecretPlaceholder(name, suffix string) string {
	if name == "" {
		return ""
	}
	if suffix == "" {
		return "{{ secret " + name + " }}"
	}
	return "{{ secret " + name + "." + suffix + " }}"
}

func materialPlaceholder(name string, role MaterialRole) string {
	if role == MaterialPrimary {
		return SecretPlaceholder(name, "")
	}
	return SecretPlaceholder(name, string(role))
}

type MaterialRole string

const (
	MaterialPrimary    MaterialRole = "primary"
	MaterialTLSKey     MaterialRole = "tls-key"
	MaterialSSHPrivate MaterialRole = "ssh-private"
	MaterialSSHPublic  MaterialRole = "ssh-public"
)

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

func ResolvePath(name string, idx Index, secretsDir string) string {
	return ResolveMaterialPath(name, idx, secretsDir, MaterialPrimary)
}

func ResolveMaterialPath(name string, idx Index, secretsDir string, role MaterialRole) string {
	if name == "" {
		return ""
	}
	if IsPlaceholderSecretsDir(secretsDir) {
		return materialPlaceholder(name, role)
	}
	if idx.useContextPath(name, role) {
		return contextMaterialPath(name, secretsDir, role)
	}
	if path, ok := idx.sourceFilePath(name, role); ok {
		return path
	}
	return contextMaterialPath(name, secretsDir, fallbackRole(role))
}

func ResolveTLSKeyPath(name string, idx Index, secretsDir string) string {
	return ResolveMaterialPath(name, idx, secretsDir, MaterialTLSKey)
}

func ResolveSSHPrivateKeyPath(name string, idx Index, secretsDir string) string {
	return ResolveMaterialPath(name, idx, secretsDir, MaterialSSHPrivate)
}

func ResolveSSHPublicKeyPath(name string, idx Index, secretsDir string) string {
	return ResolveMaterialPath(name, idx, secretsDir, MaterialSSHPublic)
}

func ResolveSourceMaterialPath(name string, idx Index, role MaterialRole) string {
	if name == "" {
		return ""
	}
	path, _ := idx.sourceFilePath(name, role)
	return path
}

func MaterialPathUsesExternalSource(name string, idx Index, role MaterialRole) bool {
	return idx.usesExternalSource(name, role)
}

func contextMaterialPath(name, secretsDir string, role MaterialRole) string {
	switch role {
	case MaterialTLSKey:
		return filepath.Join(secretsDir, name+".key")
	case MaterialSSHPublic:
		return filepath.Join(secretsDir, name+".pub")
	default:
		return filepath.Join(secretsDir, name)
	}
}

func fallbackRole(role MaterialRole) MaterialRole {
	switch role {
	case MaterialTLSKey:
		return MaterialTLSKey
	case MaterialSSHPublic:
		return MaterialSSHPublic
	case MaterialSSHPrivate:
		return MaterialSSHPrivate
	default:
		return MaterialPrimary
	}
}

func sshPublicPath(path string) string {
	if strings.HasSuffix(path, ".pub") {
		return path
	}
	return path + ".pub"
}
