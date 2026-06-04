package secret

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	safefs "github.com/crmarques/bootwright/internal/runtime/fs"
	"go.yaml.in/yaml/v3"
)

func (s *ContextStore) ensureInitialized() (storeMetadata, error) {
	if err := s.ensureStoreDirs(); err != nil {
		return storeMetadata{}, err
	}
	metadata, err := s.loadMetadata()
	if err == nil {
		if err := validateStoreMetadata(metadata); err != nil {
			return storeMetadata{}, err
		}
		if _, err := s.readKey(metadata.ActiveKeyID); err != nil {
			return storeMetadata{}, err
		}
		return metadata, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return storeMetadata{}, err
	}
	keyID, err := s.createKey()
	if err != nil {
		return storeMetadata{}, err
	}
	metadata = storeMetadata{
		Version:     storeVersion,
		KeyProvider: keyProviderRootOwnedFile,
		ActiveKeyID: keyID,
	}
	if err := s.writeMetadata(metadata); err != nil {
		return storeMetadata{}, err
	}
	return metadata, nil
}

func validateStoreMetadata(metadata storeMetadata) error {
	if metadata.Version != storeVersion {
		return fmt.Errorf("secret store has unsupported version %d", metadata.Version)
	}
	if metadata.KeyProvider != keyProviderRootOwnedFile {
		return fmt.Errorf("secret store has unsupported key provider %q", metadata.KeyProvider)
	}
	if metadata.ActiveKeyID == "" {
		return errors.New("secret store is missing active key ID")
	}
	return nil
}

func (s *ContextStore) loadMetadata() (storeMetadata, error) {
	path := s.storePath()
	if err := ensureRegularOwnedFile(path, s.ownerUID, 0o600); err != nil {
		return storeMetadata{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return storeMetadata{}, fmt.Errorf("read secret store metadata %s: %w", path, err)
	}
	var metadata storeMetadata
	if err := yaml.Unmarshal(data, &metadata); err != nil {
		return storeMetadata{}, fmt.Errorf("parse secret store metadata %s: %w", path, err)
	}
	return metadata, nil
}

func (s *ContextStore) writeMetadata(metadata storeMetadata) error {
	if err := s.ensureStoreDirs(); err != nil {
		return err
	}
	data, err := yaml.Marshal(metadata)
	if err != nil {
		return fmt.Errorf("encode secret store metadata: %w", err)
	}
	return safefs.AtomicWriteFile(s.storePath(), data, 0o600)
}

func (s *ContextStore) createKey() (string, error) {
	if err := s.ensureStoreDirs(); err != nil {
		return "", err
	}
	keyIDBytes := make([]byte, keyIDSizeBytes)
	keyBytes := make([]byte, keySizeBytes)
	if _, err := rand.Read(keyIDBytes); err != nil {
		return "", fmt.Errorf("generate key ID: %w", err)
	}
	if _, err := rand.Read(keyBytes); err != nil {
		return "", fmt.Errorf("generate root-owned key: %w", err)
	}
	keyID := hex.EncodeToString(keyIDBytes)
	path := s.keyPath(keyID)
	if err := writeNewOwnedFile(path, keyBytes, 0o600); err != nil {
		return "", err
	}
	if err := ensureRegularOwnedFile(path, s.ownerUID, 0o600); err != nil {
		return "", err
	}
	return keyID, nil
}

func (s *ContextStore) readKey(keyID string) ([]byte, error) {
	if keyID == "" {
		return nil, errors.New("secret store key ID is empty")
	}
	path := s.keyPath(keyID)
	if err := ensureRegularOwnedFile(path, s.ownerUID, 0o600); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read secret store key %s: %w", path, err)
	}
	if len(data) != keySizeBytes {
		return nil, fmt.Errorf("secret store key %s has %d byte(s), expected %d", path, len(data), keySizeBytes)
	}
	return data, nil
}

func (s *ContextStore) listKeyIDs() ([]string, error) {
	entries, err := os.ReadDir(s.keysDir())
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("list secret store keys: %w", err)
	}
	var keys []string
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".key") {
			return keys, fmt.Errorf("unexpected keyring entry %s", filepath.Join(s.keysDir(), name))
		}
		keyID := strings.TrimSuffix(name, ".key")
		if _, err := s.readKey(keyID); err != nil {
			return keys, err
		}
		keys = append(keys, keyID)
	}
	sort.Strings(keys)
	return keys, nil
}

func (s *ContextStore) removeUnusedKeys(activeKeyID string) error {
	used := map[string]bool{activeKeyID: true}
	materials, err := s.ListMaterial()
	if err != nil {
		return err
	}
	for _, material := range materials {
		if material.State == MaterialStateEncrypted && material.KeyID != "" {
			used[material.KeyID] = true
		}
	}
	keys, err := s.listKeyIDs()
	if err != nil {
		return err
	}
	for _, keyID := range keys {
		if used[keyID] {
			continue
		}
		path := s.keyPath(keyID)
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove unused secret store key %s: %w", path, err)
		}
	}
	return nil
}

func (s *ContextStore) ensureStoreDirs() error {
	if err := s.ensureSecretsDir(); err != nil {
		return err
	}
	if err := ensureOwnedDir(s.metadataDir(), s.ownerUID, 0o700); err != nil {
		return err
	}
	return ensureOwnedDir(s.keysDir(), s.ownerUID, 0o700)
}

func (s *ContextStore) ensureSecretsDir() error {
	return ensureOwnedDir(s.secretsDir, s.ownerUID, 0o700)
}

func (s *ContextStore) materialPath(key MaterialKey) string {
	return contextMaterialPath(key.Name, s.secretsDir, key.Role)
}

func (s *ContextStore) metadataDir() string {
	return filepath.Join(s.secretsDir, storeDirName)
}

func (s *ContextStore) keysDir() string {
	return filepath.Join(s.metadataDir(), storeKeysDirName)
}

func (s *ContextStore) storePath() string {
	return filepath.Join(s.metadataDir(), storeFileName)
}

func (s *ContextStore) keyPath(keyID string) string {
	return filepath.Join(s.keysDir(), keyID+".key")
}
