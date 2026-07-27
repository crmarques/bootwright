package secret

import "fmt"

type decryptSession struct {
	store    *ContextStore
	metadata storeMetadata
	keys     map[string][]byte
}

func (s *ContextStore) newDecryptSession() (*decryptSession, error) {
	metadata, err := s.loadMetadata()
	if err != nil {
		return nil, err
	}
	if err := validateStoreMetadata(metadata); err != nil {
		return nil, err
	}
	return &decryptSession{store: s, metadata: metadata, keys: map[string][]byte{}}, nil
}

func (d *decryptSession) close() {
	for keyID, keyBytes := range d.keys {
		clear(keyBytes)
		delete(d.keys, keyID)
	}
}

func (d *decryptSession) keyBytes(keyID string) ([]byte, error) {
	if cached, ok := d.keys[keyID]; ok {
		return cached, nil
	}
	loaded, err := d.store.readKey(keyID)
	if err != nil {
		return nil, err
	}
	d.keys[keyID] = loaded
	return loaded, nil
}

func (d *decryptSession) decrypt(key MaterialKey, env envelope) ([]byte, error) {
	if env.KeyProvider != d.metadata.KeyProvider {
		return nil, fmt.Errorf("secret %s uses key provider %q but store uses %q", materialLabel(key), env.KeyProvider, d.metadata.KeyProvider)
	}
	keyBytes, err := d.keyBytes(env.KeyID)
	if err != nil {
		return nil, err
	}
	return d.store.decryptEnvelope(key, env, keyBytes)
}

func (d *decryptSession) readMaterial(status MaterialStatus) ([]byte, error) {
	if status.env != nil {
		return d.decrypt(status.Key, *status.env)
	}
	env, err := d.store.readEnvelope(status.Path, status.Key)
	if err != nil {
		return nil, err
	}
	return d.decrypt(status.Key, env)
}
