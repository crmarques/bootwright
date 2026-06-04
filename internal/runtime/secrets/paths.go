package secret

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/runtime/root/localroot"
)

const InternalCallerHomeEnv = localroot.CallerHomeEnv

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

func ResolvePath(name string, env *v1alpha1.Environment, secretsDir string) string {
	return ResolveMaterialPath(name, env, secretsDir, MaterialPrimary)
}

func ResolveMaterialPath(name string, env *v1alpha1.Environment, secretsDir string, role MaterialRole) string {
	if name == "" {
		return ""
	}
	if shouldUseContextSecretPath(name, env, role) {
		return contextMaterialPath(name, secretsDir, role)
	}
	if secret, ok := environmentSecretSpec(name, env); ok {
		if path, ok := sourceMaterialPath(secret, env, role); ok {
			return path
		}
	}
	return contextMaterialPath(name, secretsDir, fallbackRole(role))
}

func ResolveTLSKeyPath(name string, env *v1alpha1.Environment, secretsDir string) string {
	return ResolveMaterialPath(name, env, secretsDir, MaterialTLSKey)
}

func ResolveSSHPrivateKeyPath(name string, env *v1alpha1.Environment, secretsDir string) string {
	return ResolveMaterialPath(name, env, secretsDir, MaterialSSHPrivate)
}

func ResolveSSHPublicKeyPath(name string, env *v1alpha1.Environment, secretsDir string) string {
	return ResolveMaterialPath(name, env, secretsDir, MaterialSSHPublic)
}

func ResolveSourceMaterialPath(name string, env *v1alpha1.Environment, role MaterialRole) string {
	if name == "" {
		return ""
	}
	spec, ok := environmentSecretSpec(name, env)
	if !ok {
		return ""
	}
	if path, ok := sourceMaterialPath(spec, env, role); ok {
		return path
	}
	return ""
}

func MaterialPathUsesExternalSource(name string, env *v1alpha1.Environment, role MaterialRole) bool {
	spec, ok := environmentSecretSpec(name, env)
	if !ok || spec.File == "" {
		return false
	}
	return !shouldUseContextSecretPath(name, env, role)
}

func environmentSecretSpec(name string, env *v1alpha1.Environment) (v1alpha1.EnvironmentSecretSpec, bool) {
	if env == nil {
		return v1alpha1.EnvironmentSecretSpec{}, false
	}
	secret, ok := env.Spec.Secrets[name]
	return secret, ok
}

func shouldUseContextSecretPath(name string, env *v1alpha1.Environment, role MaterialRole) bool {
	spec, ok := environmentSecretSpec(name, env)
	if !ok {
		return false
	}
	if env.Spec.SecretStorage.Mode == v1alpha1.SecretStorageModeContext {
		return true
	}
	if spec.Generated == nil {
		return false
	}
	if role == MaterialSSHPublic {
		return spec.Generated.SSHKeyPair != nil
	}
	return true
}

func sourceMaterialPath(spec v1alpha1.EnvironmentSecretSpec, env *v1alpha1.Environment, role MaterialRole) (string, bool) {
	envSourceDir := filepath.Dir(env.SourcePath)
	switch role {
	case MaterialTLSKey:
		if spec.KeyFile == "" {
			return "", false
		}
		path, err := ResolveKeyFilePath(spec.KeyFile, envSourceDir)
		return path, err == nil
	case MaterialSSHPublic:
		if spec.File == "" {
			return "", false
		}
		path, err := ResolveKeyFilePath(sshPublicPath(spec.File), envSourceDir)
		return path, err == nil
	case MaterialSSHPrivate:
		if spec.File == "" {
			return "", false
		}
		path, err := ResolveKeyFilePath(sshPrivatePath(spec.File), envSourceDir)
		return path, err == nil
	default:
		if spec.File == "" {
			return "", false
		}
		path, err := ResolveKeyFilePath(spec.File, envSourceDir)
		return path, err == nil
	}
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
