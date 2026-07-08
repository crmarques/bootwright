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

// PlaceholderSecretsDir is a sentinel secrets-directory value that switches
// secret path resolution into placeholder mode. When ResolveMaterialPath (and
// every Resolve* wrapper) receives it, it returns a portable
// "{{ secret <name>[.<role>] }}" substitution token instead of a filesystem
// path under a real secrets directory. The context-free render
// (`bootwright render --input-dir`) threads this value through the inventory
// PathOptions so the rendered bundle carries no context-bound paths and no
// secret material — only placeholders a downstream secrets manager rehydrates.
//
// The value is NUL-wrapped so it can never collide with a real path or be
// created on disk. It is render-only: the read/write/store side
// (NewResolver, ReadMaterial, MaterializeForContext, ...) must never be
// invoked with it.
const PlaceholderSecretsDir = "\x00bootwright-placeholder-secrets\x00"

// IsPlaceholderSecretsDir reports whether secretsDir is the placeholder-mode
// sentinel.
func IsPlaceholderSecretsDir(secretsDir string) bool {
	return secretsDir == PlaceholderSecretsDir
}

// SecretPlaceholder renders the portable substitution token for a secret
// material: "{{ secret <name> }}" for the primary material, or
// "{{ secret <name>.<suffix> }}" when a secret carries more than one material
// (suffix is a role discriminator such as "tls-key", "ssh-public",
// "ssh-private", "username", "password"). An empty name yields "".
func SecretPlaceholder(name, suffix string) string {
	if name == "" {
		return ""
	}
	if suffix == "" {
		return "{{ secret " + name + " }}"
	}
	return "{{ secret " + name + "." + suffix + " }}"
}

// materialPlaceholder maps a MaterialRole onto SecretPlaceholder: the primary
// material has no suffix; every typed role uses its role string as the suffix.
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
	// Placeholder mode short-circuits BEFORE the context/external-source split so
	// a portable bundle never leaks either a context-secrets-dir path or the
	// operator's local source-file path: every reference becomes a token.
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

func sshPrivatePath(path string) string {
	if strings.HasSuffix(path, ".pub") {
		return strings.TrimSuffix(path, ".pub")
	}
	return path
}

func sshPublicPath(path string) string {
	if strings.HasSuffix(path, ".pub") {
		return path
	}
	return path + ".pub"
}
