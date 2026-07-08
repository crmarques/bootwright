package secret

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	safefs "github.com/crmarques/bootwright/internal/host/safefs"
)

func (s *ContextStore) Status() StoreStatus {
	status := StoreStatus{}
	metadata, err := s.loadMetadata()
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			status.Message = "encrypted secret store is not initialized"
		} else {
			status.Message = err.Error()
			status.ValidationErrs = append(status.ValidationErrs, err.Error())
		}
	} else if err := validateStoreMetadata(metadata); err != nil {
		status.Message = err.Error()
		status.ValidationErrs = append(status.ValidationErrs, err.Error())
	} else {
		status.Initialized = true
		status.KeyProvider = metadata.KeyProvider
		status.ActiveKeyID = metadata.ActiveKeyID
		keys, keyErr := s.listKeyIDs()
		if keyErr != nil {
			status.ValidationErrs = append(status.ValidationErrs, keyErr.Error())
		}
		status.Keys = keys
	}
	materials, scanErr := s.ListMaterial()
	if scanErr != nil {
		status.ValidationErrs = append(status.ValidationErrs, scanErr.Error())
	}
	status.Material = materials
	for _, material := range materials {
		switch material.State {
		case MaterialStateEncrypted:
			status.Encrypted++
		case MaterialStatePlaintextBlocked:
			status.Plaintext++
		case MaterialStateUnsafe:
			status.Unsafe++
		case MaterialStateMissing:
			status.Missing++
		}
	}
	return status
}

func (s *ContextStore) EnsureInitialized() error {
	_, err := s.ensureInitialized()
	return err
}

func (s *ContextStore) ListMaterial() ([]MaterialStatus, error) {
	entries, err := os.ReadDir(s.secretsDir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("list secrets directory %s: %w", s.secretsDir, err)
	}
	var out []MaterialStatus
	for _, entry := range entries {
		name := entry.Name()
		if name == storeDirName {
			continue
		}
		status, ok, err := s.inspectMaterialEntry(name)
		if err != nil {
			return nil, err
		}
		if !ok {
			path := filepath.Join(s.secretsDir, name)
			out = append(out, MaterialStatus{
				Key:     MaterialKey{Name: name, Role: MaterialPrimary},
				Path:    path,
				State:   MaterialStatePlaintextBlocked,
				Message: fmt.Sprintf("%s cannot be mapped to a context secret material role", path),
			})
			continue
		}
		out = append(out, status)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Key.Name == out[j].Key.Name {
			return out[i].Key.Role < out[j].Key.Role
		}
		return out[i].Key.Name < out[j].Key.Name
	})
	return out, nil
}

func (s *ContextStore) inspectMaterialEntry(fileName string) (MaterialStatus, bool, error) {
	key, ok := materialKeyFromFileName(fileName)
	if !ok {
		return MaterialStatus{}, false, nil
	}
	if key.Role != MaterialPrimary {
		status, err := s.Inspect(key)
		return status, true, err
	}
	primary, err := s.Inspect(key)
	if err != nil {
		return MaterialStatus{}, true, err
	}
	if primary.State == MaterialStateEncrypted || primary.State == MaterialStateMissing {
		return primary, true, nil
	}
	private, err := s.Inspect(MaterialKey{Name: key.Name, Role: MaterialSSHPrivate})
	if err != nil {
		return MaterialStatus{}, true, err
	}
	if private.State == MaterialStateEncrypted {
		return private, true, nil
	}
	return primary, true, nil
}

func (s *ContextStore) MigratePlaintext(roleForName func(string) MaterialRole) ([]MaterialStatus, error) {
	if _, err := s.ensureInitialized(); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(s.secretsDir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("list secrets directory %s: %w", s.secretsDir, err)
	}
	var migrated []MaterialStatus
	for _, entry := range entries {
		if entry.Name() == storeDirName {
			continue
		}
		key, ok := materialKeyFromFileName(entry.Name())
		if !ok {
			return migrated, fmt.Errorf("refusing to migrate unmappable plaintext file %s", filepath.Join(s.secretsDir, entry.Name()))
		}
		if key.Role == MaterialPrimary && roleForName != nil {
			key.Role = roleForName(key.Name)
			if key.Role == "" {
				key.Role = MaterialPrimary
			}
		}
		path := filepath.Join(s.secretsDir, entry.Name())
		if err := ensureRegularOwnedFile(path, s.ownerUID, 0o600); err != nil {
			return migrated, err
		}
		current, err := s.Inspect(key)
		if err != nil {
			return migrated, err
		}
		if current.State == MaterialStateEncrypted {
			migrated = append(migrated, current)
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return migrated, fmt.Errorf("read plaintext secret %s: %w", path, err)
		}
		if err := s.write(key, data, true); err != nil {
			return migrated, err
		}
		status, err := s.Inspect(key)
		if err != nil {
			return migrated, err
		}
		migrated = append(migrated, status)
	}
	return migrated, nil
}

func (s *ContextStore) Rotate() error {
	metadata, err := s.ensureInitialized()
	if err != nil {
		return err
	}
	materials, err := s.ListMaterial()
	if err != nil {
		return err
	}
	payloads := map[MaterialKey][]byte{}
	for _, material := range materials {
		switch material.State {
		case MaterialStateEncrypted:
			data, err := s.Read(material.Key)
			if err != nil {
				return err
			}
			payloads[material.Key] = data
		case MaterialStateMissing:
			continue
		default:
			return fmt.Errorf("refusing to rotate with %s material at %s: %s", material.State, material.Path, material.Message)
		}
	}
	newKeyID, err := s.createKey()
	if err != nil {
		return err
	}
	metadata.ActiveKeyID = newKeyID
	if err := s.writeMetadata(metadata); err != nil {
		return err
	}
	keys := make([]MaterialKey, 0, len(payloads))
	for key := range payloads {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].Name == keys[j].Name {
			return keys[i].Role < keys[j].Role
		}
		return keys[i].Name < keys[j].Name
	})
	for _, key := range keys {
		if err := s.Write(key, payloads[key]); err != nil {
			return err
		}
	}
	for _, key := range keys {
		data, err := s.Read(key)
		if err != nil {
			return err
		}
		if !bytes.Equal(data, payloads[key]) {
			return fmt.Errorf("verification failed after rotating %s", materialLabel(key))
		}
	}
	return s.removeUnusedKeys(metadata.ActiveKeyID)
}

// MaterializeSelected materializes only the named materials into targetDir,
// otherwise identical to MaterializeRuntime. It is the scoped-secrets primitive
// for ClusterAddon hook runs: a hook receives only its declared secretRefs (and,
// for its connection dir, only the target machines' SSH key material), never the
// whole store. Names not present in the store are silently skipped (a missing
// secret is reported by preflight, not here).
func (s *ContextStore) MaterializeSelected(targetDir string, names []string) (err error) {
	want := map[string]bool{}
	for _, name := range names {
		if name != "" {
			want[name] = true
		}
	}
	return s.materialize(targetDir, func(material MaterialStatus) bool {
		return want[material.Key.Name]
	})
}

func (s *ContextStore) MaterializeRuntime(targetDir string) (err error) {
	return s.materialize(targetDir, func(MaterialStatus) bool { return true })
}

func (s *ContextStore) materialize(targetDir string, include func(MaterialStatus) bool) (err error) {
	materials, err := s.ListMaterial()
	if err != nil {
		return err
	}
	if err := os.RemoveAll(targetDir); err != nil {
		return fmt.Errorf("clean runtime secrets directory %s: %w", targetDir, err)
	}
	if err := os.MkdirAll(targetDir, 0o700); err != nil {
		return fmt.Errorf("create runtime secrets directory %s: %w", targetDir, err)
	}
	if err := os.Chmod(targetDir, 0o700); err != nil {
		return fmt.Errorf("chmod runtime secrets directory %s: %w", targetDir, err)
	}
	// Plaintext copies are written one material at a time. If a later material
	// fails to decrypt or write, remove everything written so far: callers
	// register their own cleanup defer only on the success path, so without this
	// a failed materialization would leave partial plaintext secrets on disk.
	defer func() {
		if err != nil {
			_ = os.RemoveAll(targetDir)
		}
	}()
	for _, material := range materials {
		if material.State == MaterialStateMissing || !include(material) {
			continue
		}
		if material.State != MaterialStateEncrypted {
			return fmt.Errorf("refusing to materialize %s material at %s: %s", material.State, material.Path, material.Message)
		}
		data, err := s.Read(material.Key)
		if err != nil {
			return err
		}
		target := contextMaterialPath(material.Key.Name, targetDir, material.Key.Role)
		if err := safefs.AtomicWriteFile(target, data, 0o600); err != nil {
			return err
		}
	}
	return nil
}
