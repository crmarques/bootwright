package secret

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

func (s *ContextStore) decryptEnvelope(key MaterialKey, env envelope, keyBytes []byte) ([]byte, error) {
	nonce, err := base64.StdEncoding.DecodeString(env.Nonce)
	if err != nil {
		return nil, fmt.Errorf("decode nonce for %s: %w", materialLabel(key), err)
	}
	ciphertext, err := base64.StdEncoding.DecodeString(env.Ciphertext)
	if err != nil {
		return nil, fmt.Errorf("decode ciphertext for %s: %w", materialLabel(key), err)
	}
	block, err := aes.NewCipher(keyBytes)
	if err != nil {
		return nil, fmt.Errorf("initialize cipher for %s: %w", materialLabel(key), err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("initialize AEAD for %s: %w", materialLabel(key), err)
	}
	if len(nonce) != gcm.NonceSize() {
		return nil, fmt.Errorf("secret %s nonce has %d byte(s), expected %d", materialLabel(key), len(nonce), gcm.NonceSize())
	}
	plaintext, err := gcm.Open(nil, nonce, ciphertext, s.aad(key, env.Algorithm, env.KeyProvider, env.KeyID))
	if err != nil {
		return nil, fmt.Errorf("decrypt %s: authentication failed", materialLabel(key))
	}
	return plaintext, nil
}

func (s *ContextStore) encryptEnvelope(key MaterialKey, plaintext []byte, keyID string, keyBytes []byte) (envelope, error) {
	block, err := aes.NewCipher(keyBytes)
	if err != nil {
		return envelope{}, fmt.Errorf("initialize cipher for %s: %w", materialLabel(key), err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return envelope{}, fmt.Errorf("initialize AEAD for %s: %w", materialLabel(key), err)
	}
	nonce := make([]byte, nonceSizeBytes)
	if _, err := rand.Read(nonce); err != nil {
		return envelope{}, fmt.Errorf("generate nonce for %s: %w", materialLabel(key), err)
	}
	if len(nonce) != gcm.NonceSize() {
		return envelope{}, fmt.Errorf("nonce size %d does not match AEAD nonce size %d", len(nonce), gcm.NonceSize())
	}
	env := envelope{
		Version:     storeVersion,
		Algorithm:   algorithmAES256GCM,
		KeyProvider: keyProviderRootOwnedFile,
		KeyID:       keyID,
		Context:     s.contextName,
		Name:        key.Name,
		Role:        string(key.Role),
		Nonce:       base64.StdEncoding.EncodeToString(nonce),
		KDF:         kdfNone,
	}
	ciphertext := gcm.Seal(nil, nonce, plaintext, s.aad(key, env.Algorithm, env.KeyProvider, keyID))
	env.Ciphertext = base64.StdEncoding.EncodeToString(ciphertext)
	return env, nil
}

func (s *ContextStore) readEnvelope(path string, key MaterialKey) (envelope, error) {
	if err := ensureRegularOwnedFile(path, s.ownerUID, 0o600); err != nil {
		return envelope{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return envelope{}, fmt.Errorf("read encrypted secret %s: %w", path, err)
	}
	var env envelope
	if err := json.Unmarshal(data, &env); err != nil {
		return envelope{}, fmt.Errorf("context secret %s is not encrypted; run bootwright secret encryption migrate", materialLabel(key))
	}
	if err := s.validateEnvelope(env, key); err != nil {
		return envelope{}, err
	}
	return env, nil
}

func (s *ContextStore) validateEnvelope(env envelope, key MaterialKey) error {
	if env.Version != storeVersion {
		return fmt.Errorf("secret %s has unsupported envelope version %d", materialLabel(key), env.Version)
	}
	if env.Algorithm != algorithmAES256GCM {
		return fmt.Errorf("secret %s has unsupported algorithm %q", materialLabel(key), env.Algorithm)
	}
	if env.KeyProvider != keyProviderRootOwnedFile {
		return fmt.Errorf("secret %s has unsupported key provider %q", materialLabel(key), env.KeyProvider)
	}
	if env.KDF != kdfNone {
		return fmt.Errorf("secret %s has unsupported KDF %q", materialLabel(key), env.KDF)
	}
	if env.KeyID == "" {
		return fmt.Errorf("secret %s is missing key ID", materialLabel(key))
	}
	if env.Context != s.contextName {
		return fmt.Errorf("secret %s is bound to context %q, not %q", materialLabel(key), env.Context, s.contextName)
	}
	if env.Name != key.Name {
		return fmt.Errorf("secret %s envelope name is %q", materialLabel(key), env.Name)
	}
	if env.Role != string(key.Role) {
		return fmt.Errorf("secret %s envelope role is %q", materialLabel(key), env.Role)
	}
	if env.Nonce == "" || env.Ciphertext == "" {
		return fmt.Errorf("secret %s envelope is missing nonce or ciphertext", materialLabel(key))
	}
	return nil
}

func (s *ContextStore) aad(key MaterialKey, algorithm, provider, keyID string) []byte {
	return []byte(strings.Join([]string{
		"bootwright-context-secret",
		"v=1",
		"context=" + s.contextName,
		"name=" + key.Name,
		"role=" + string(key.Role),
		"algorithm=" + algorithm,
		"keyProvider=" + provider,
		"keyID=" + keyID,
	}, "\n"))
}
