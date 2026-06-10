package secret

import (
	"fmt"
	"os"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

type Resolver struct {
	Store      *ContextStore
	Env        *v1alpha1.Environment
	SecretsDir string
}

func NewResolver(contextName, secretsDir string, env *v1alpha1.Environment) Resolver {
	return Resolver{
		Store:      NewContextStore(contextName, secretsDir),
		Env:        env,
		SecretsDir: secretsDir,
	}
}

func (r Resolver) ReadMaterial(name string, role MaterialRole) ([]byte, error) {
	if MaterialPathUsesExternalSource(name, r.Env, role) {
		path := ResolveSourceMaterialPath(name, r.Env, role)
		if path == "" {
			return nil, fmt.Errorf("secret %q %s source path is empty", name, role)
		}
		data, err := ReadExternalFile(path)
		if err != nil {
			return nil, fmt.Errorf("read external secret %s for %s/%s: %w", path, name, role, err)
		}
		return data, nil
	}
	if r.Store == nil {
		return nil, fmt.Errorf("context secret store is not configured for %s/%s", name, role)
	}
	return r.Store.Read(MaterialKey{Name: name, Role: role})
}

// ReadMaterialWithPath resolves the on-disk path for a material and reads
// it, returning the path alongside the trailing-newline-trimmed content so
// callers can report where the material came from.
func (r Resolver) ReadMaterialWithPath(name string, role MaterialRole, kind string) (string, string, error) {
	path := ResolveMaterialPath(name, r.Env, r.SecretsDir, role)
	if path == "" {
		return "", "", fmt.Errorf("%s path is empty", kind)
	}
	data, err := r.ReadMaterial(name, role)
	if err != nil {
		return path, "", fmt.Errorf("read %s at %s: %w", kind, path, err)
	}
	return path, strings.TrimRight(string(data), "\n"), nil
}

func (r Resolver) ReadUserPasswordMaterial(name string, role MaterialRole, kind string) (UserPassword, error) {
	data, err := r.ReadMaterial(name, role)
	if err != nil {
		return UserPassword{}, err
	}
	username, password, err := ParseBMCCredentials(data)
	if err != nil {
		return UserPassword{}, fmt.Errorf("%s %s/%s: %w", kind, name, role, err)
	}
	return UserPassword{Username: username, Password: password}, nil
}

func (r Resolver) StatMaterial(name string, role MaterialRole) (os.FileInfo, error) {
	if MaterialPathUsesExternalSource(name, r.Env, role) {
		path := ResolveSourceMaterialPath(name, r.Env, role)
		if path == "" {
			return nil, fmt.Errorf("secret %q %s source path is empty", name, role)
		}
		return StatExternalFile(path)
	}
	path := ResolveMaterialPath(name, r.Env, r.SecretsDir, role)
	status, err := r.Store.Inspect(MaterialKey{Name: name, Role: role})
	if err != nil {
		return nil, err
	}
	if status.State != MaterialStateEncrypted {
		return nil, fmt.Errorf("context secret %s/%s at %s is %s: %s", name, role, path, status.State, status.Message)
	}
	return Stat(path)
}
