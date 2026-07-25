package secret

import (
	"fmt"
	"os"
	"strings"
)

type Resolver struct {
	Store      *ContextStore
	Index      Index
	SecretsDir string
}

func NewResolver(contextName, secretsDir string, idx Index) Resolver {
	return Resolver{
		Store:      NewContextStore(contextName, secretsDir),
		Index:      idx,
		SecretsDir: secretsDir,
	}
}

func (r Resolver) ReadMaterial(name string, role MaterialRole) ([]byte, error) {
	if MaterialPathUsesExternalSource(name, r.Index, role) {
		path := ResolveSourceMaterialPath(name, r.Index, role)
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

func (r Resolver) WithMaterialized(name string, role MaterialRole, parentDir string, fn func(string) error) error {
	key := MaterialKey{Name: name, Role: role}
	if err := validateMaterialKey(key); err != nil {
		return err
	}
	if MaterialPathUsesExternalSource(name, r.Index, role) {
		data, err := r.ReadMaterial(name, role)
		if err != nil {
			return err
		}
		return withMaterializedData(key, parentDir, data, fn)
	}
	if r.Store == nil {
		return fmt.Errorf("context secret store is not configured for %s/%s", name, role)
	}
	return r.Store.WithMaterialized(key, parentDir, fn)
}

func (r Resolver) ReadMaterialWithPath(name string, role MaterialRole, kind string) (string, string, error) {
	path := ResolveMaterialPath(name, r.Index, r.SecretsDir, role)
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
	if MaterialPathUsesExternalSource(name, r.Index, role) {
		path := ResolveSourceMaterialPath(name, r.Index, role)
		if path == "" {
			return nil, fmt.Errorf("secret %q %s source path is empty", name, role)
		}
		return StatExternalFile(path)
	}
	path := ResolveMaterialPath(name, r.Index, r.SecretsDir, role)
	status, err := r.Store.Inspect(MaterialKey{Name: name, Role: role})
	if err != nil {
		return nil, err
	}
	if status.State != MaterialStateEncrypted {
		return nil, fmt.Errorf("context secret %s/%s at %s is %s: %s", name, role, path, status.State, status.Message)
	}
	return Stat(path)
}
