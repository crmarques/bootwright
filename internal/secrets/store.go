package secret

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"

	safefs "github.com/crmarques/bootwright/internal/host/safefs"
)

const (
	storeDirName     = ".bootwright"
	storeKeysDirName = "keys"
	storeFileName    = "store.yaml"

	storeVersion = 1

	keyProviderRootOwnedFile = "root-owned-file"
	algorithmAES256GCM       = "AES-256-GCM"
	kdfNone                  = "none"

	keySizeBytes   = 32
	nonceSizeBytes = 12
	keyIDSizeBytes = 16
)

type MaterialKey struct {
	Name string
	Role MaterialRole
}

type SecretStore interface {
	Read(MaterialKey) ([]byte, error)
	Write(MaterialKey, []byte) error
	Delete(MaterialKey) error
	Exists(MaterialKey) (bool, error)
	Inspect(MaterialKey) (MaterialStatus, error)
}

type ContextStoreOptions struct {
	ContextName string
	SecretsDir  string
	OwnerUID    int
}

type ContextStore struct {
	contextName string
	secretsDir  string
	ownerUID    int
}

type MaterialState string

const (
	MaterialStateMissing          MaterialState = "missing"
	MaterialStateEncrypted        MaterialState = "encrypted"
	MaterialStatePlaintextBlocked MaterialState = "plaintext-blocked"
	MaterialStateUnsafe           MaterialState = "unsafe"
)

type MaterialStatus struct {
	Key       MaterialKey   `json:"key"`
	Path      string        `json:"path"`
	State     MaterialState `json:"state"`
	KeyID     string        `json:"keyID,omitempty"`
	Algorithm string        `json:"algorithm,omitempty"`
	Message   string        `json:"message,omitempty"`
}

type StoreStatus struct {
	Initialized    bool             `json:"initialized"`
	KeyProvider    string           `json:"keyProvider,omitempty"`
	ActiveKeyID    string           `json:"activeKeyID,omitempty"`
	Keys           []string         `json:"keys,omitempty"`
	Encrypted      int              `json:"encrypted"`
	Plaintext      int              `json:"plaintextBlocked"`
	Unsafe         int              `json:"unsafe"`
	Missing        int              `json:"missing,omitempty"`
	Material       []MaterialStatus `json:"material,omitempty"`
	Message        string           `json:"message,omitempty"`
	ValidationErrs []string         `json:"validationErrors,omitempty"`
}

type storeMetadata struct {
	Version     int    `yaml:"version"`
	KeyProvider string `yaml:"keyProvider"`
	ActiveKeyID string `yaml:"activeKeyID"`
}

type envelope struct {
	Version     int    `json:"version"`
	Algorithm   string `json:"algorithm"`
	KeyProvider string `json:"keyProvider"`
	KeyID       string `json:"keyID"`
	Context     string `json:"context"`
	Name        string `json:"name"`
	Role        string `json:"role"`
	Nonce       string `json:"nonce"`
	Ciphertext  string `json:"ciphertext"`
	KDF         string `json:"kdf"`
}

func NewContextStore(contextName, secretsDir string) *ContextStore {
	return NewContextStoreWithOptions(ContextStoreOptions{
		ContextName: contextName,
		SecretsDir:  secretsDir,
		OwnerUID:    os.Geteuid(),
	})
}

func NewContextStoreWithOptions(opts ContextStoreOptions) *ContextStore {
	ownerUID := opts.OwnerUID
	if ownerUID < 0 {
		ownerUID = os.Geteuid()
	}
	return &ContextStore{
		contextName: opts.ContextName,
		secretsDir:  opts.SecretsDir,
		ownerUID:    ownerUID,
	}
}

func (s *ContextStore) Read(key MaterialKey) ([]byte, error) {
	if err := validateMaterialKey(key); err != nil {
		return nil, err
	}
	env, err := s.readEnvelope(s.materialPath(key), key)
	if err != nil {
		return nil, err
	}
	metadata, err := s.loadMetadata()
	if err != nil {
		return nil, err
	}
	if err := validateStoreMetadata(metadata); err != nil {
		return nil, err
	}
	if env.KeyProvider != metadata.KeyProvider {
		return nil, fmt.Errorf("secret %s uses key provider %q but store uses %q", materialLabel(key), env.KeyProvider, metadata.KeyProvider)
	}
	keyBytes, err := s.readKey(env.KeyID)
	if err != nil {
		return nil, err
	}
	return s.decryptEnvelope(key, env, keyBytes)
}

func (s *ContextStore) Write(key MaterialKey, data []byte) error {
	return s.write(key, data, false)
}

func (s *ContextStore) write(key MaterialKey, data []byte, allowPlaintextOverwrite bool) error {
	if err := validateMaterialKey(key); err != nil {
		return err
	}
	metadata, err := s.ensureInitialized()
	if err != nil {
		return err
	}
	keyBytes, err := s.readKey(metadata.ActiveKeyID)
	if err != nil {
		return err
	}
	env, err := s.encryptEnvelope(key, data, metadata.ActiveKeyID, keyBytes)
	if err != nil {
		return err
	}
	encoded, err := json.MarshalIndent(env, "", "  ")
	if err != nil {
		return fmt.Errorf("encode encrypted envelope for %s: %w", materialLabel(key), err)
	}
	encoded = append(encoded, '\n')
	path := s.materialPath(key)
	if err := s.refuseUnsafeOverwrite(key, allowPlaintextOverwrite); err != nil {
		return err
	}
	if err := s.ensureSecretsDir(); err != nil {
		return err
	}
	if err := refuseSymlink(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return safefs.AtomicWriteFile(path, encoded, 0o600)
}

func (s *ContextStore) refuseUnsafeOverwrite(key MaterialKey, allowPlaintextOverwrite bool) error {
	status, err := s.Inspect(key)
	if err != nil {
		return err
	}
	if status.State == MaterialStatePlaintextBlocked && !allowPlaintextOverwrite {
		return fmt.Errorf("refusing to overwrite plaintext context secret %s at %s; run bootwright secret encryption migrate first", materialLabel(key), status.Path)
	}
	if status.State == MaterialStateUnsafe {
		return fmt.Errorf("refusing to overwrite unsafe context secret %s at %s: %s", materialLabel(key), status.Path, status.Message)
	}
	if key.Role != MaterialPrimary && key.Role != MaterialSSHPrivate {
		return nil
	}
	alternateRole := MaterialSSHPrivate
	if key.Role == MaterialSSHPrivate {
		alternateRole = MaterialPrimary
	}
	alternate, err := s.Inspect(MaterialKey{Name: key.Name, Role: alternateRole})
	if err != nil {
		return err
	}
	if alternate.State == MaterialStateEncrypted {
		return fmt.Errorf("refusing to overwrite %s material with %s material at %s", alternate.Key.Role, key.Role, alternate.Path)
	}
	if alternate.State == MaterialStateUnsafe {
		return fmt.Errorf("refusing to overwrite unsafe context secret %s at %s: %s", materialLabel(alternate.Key), alternate.Path, alternate.Message)
	}
	return nil
}

func (s *ContextStore) Delete(key MaterialKey) error {
	if err := validateMaterialKey(key); err != nil {
		return err
	}
	path := s.materialPath(key)
	if err := refuseSymlink(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("delete %s: %w", path, err)
	}
	return nil
}

func (s *ContextStore) Exists(key MaterialKey) (bool, error) {
	status, err := s.Inspect(key)
	if err != nil {
		return false, err
	}
	return status.State == MaterialStateEncrypted, nil
}

func (s *ContextStore) Inspect(key MaterialKey) (MaterialStatus, error) {
	if err := validateMaterialKey(key); err != nil {
		return MaterialStatus{}, err
	}
	path := s.materialPath(key)
	status := MaterialStatus{Key: key, Path: path, State: MaterialStateMissing}
	env, err := s.readEnvelope(path, key)
	if err == nil {
		status.State = MaterialStateEncrypted
		status.KeyID = env.KeyID
		status.Algorithm = env.Algorithm
		return status, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return status, nil
	}
	var unsafeErr unsafePathError
	if errors.As(err, &unsafeErr) {
		status.State = MaterialStateUnsafe
		status.Message = err.Error()
		return status, nil
	}
	status.State = MaterialStatePlaintextBlocked
	status.Message = err.Error()
	return status, nil
}
