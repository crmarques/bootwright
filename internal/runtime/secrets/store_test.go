package secret

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestContextStoreEncryptDecryptRoundTripAndNonceUniqueness(t *testing.T) {
	store := NewContextStore("lab", t.TempDir())
	key := MaterialKey{Name: "pull-secret", Role: MaterialPrimary}
	if err := store.Write(key, []byte("super-secret\n")); err != nil {
		t.Fatalf("Write first: %v", err)
	}
	first := readTestEnvelope(t, store.materialPath(key))
	raw := readTestBytes(t, store.materialPath(key))
	if strings.Contains(string(raw), "super-secret") {
		t.Fatalf("encrypted envelope leaked plaintext: %s", raw)
	}
	got, err := store.Read(key)
	if err != nil {
		t.Fatalf("Read first: %v", err)
	}
	if string(got) != "super-secret\n" {
		t.Fatalf("Read = %q", got)
	}
	if err := store.Write(key, []byte("super-secret\n")); err != nil {
		t.Fatalf("Write second: %v", err)
	}
	second := readTestEnvelope(t, store.materialPath(key))
	if first.Nonce == second.Nonce {
		t.Fatal("nonce was reused for the same material")
	}
	if first.Ciphertext == second.Ciphertext {
		t.Fatal("ciphertext did not change across writes")
	}
}

func TestContextStoreRejectsAADBindingTamper(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*envelope)
		want   string
	}{
		{name: "context", mutate: func(env *envelope) { env.Context = "other" }, want: "bound to context"},
		{name: "name", mutate: func(env *envelope) { env.Name = "other-secret" }, want: "envelope name"},
		{name: "role", mutate: func(env *envelope) { env.Role = string(MaterialTLSKey) }, want: "envelope role"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := NewContextStore("lab", t.TempDir())
			key := MaterialKey{Name: "demo", Role: MaterialPrimary}
			if err := store.Write(key, []byte("secret\n")); err != nil {
				t.Fatalf("Write: %v", err)
			}
			path := store.materialPath(key)
			env := readTestEnvelope(t, path)
			tc.mutate(&env)
			writeTestEnvelope(t, path, env)
			_, err := store.Read(key)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Read error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestContextStoreRejectsCiphertextTamper(t *testing.T) {
	store := NewContextStore("lab", t.TempDir())
	key := MaterialKey{Name: "demo", Role: MaterialPrimary}
	if err := store.Write(key, []byte("secret\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	path := store.materialPath(key)
	env := readTestEnvelope(t, path)
	ciphertext, err := base64.StdEncoding.DecodeString(env.Ciphertext)
	if err != nil {
		t.Fatalf("decode ciphertext: %v", err)
	}
	ciphertext[0] ^= 0xff
	env.Ciphertext = base64.StdEncoding.EncodeToString(ciphertext)
	writeTestEnvelope(t, path, env)
	_, err = store.Read(key)
	if err == nil || !strings.Contains(err.Error(), "authentication failed") {
		t.Fatalf("Read error = %v, want authentication failure", err)
	}
}

func TestContextStoreRejectsMissingAndUnsafeKeyFiles(t *testing.T) {
	store := NewContextStore("lab", t.TempDir())
	key := MaterialKey{Name: "demo", Role: MaterialPrimary}
	if err := store.Write(key, []byte("secret\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	env := readTestEnvelope(t, store.materialPath(key))
	keyPath := store.keyPath(env.KeyID)
	if err := os.Remove(keyPath); err != nil {
		t.Fatalf("remove key: %v", err)
	}
	if _, err := store.Read(key); err == nil {
		t.Fatal("Read succeeded with missing key file")
	}

	unsafeStore := NewContextStore("lab", t.TempDir())
	if err := unsafeStore.Write(key, []byte("secret\n")); err != nil {
		t.Fatalf("Write unsafe fixture: %v", err)
	}
	env = readTestEnvelope(t, unsafeStore.materialPath(key))
	keyPath = unsafeStore.keyPath(env.KeyID)
	if err := os.Chmod(keyPath, 0o644); err != nil {
		t.Fatalf("chmod key: %v", err)
	}
	if _, err := unsafeStore.Read(key); err == nil || !strings.Contains(err.Error(), "expected 0600") {
		t.Fatalf("Read error = %v, want unsafe mode", err)
	}
}

func TestMaterializeRuntimeRemovesPartialPlaintextOnError(t *testing.T) {
	store := NewContextStore("lab", t.TempDir())
	first := MaterialKey{Name: "aaa-first", Role: MaterialPrimary}
	second := MaterialKey{Name: "zzz-second", Role: MaterialPrimary}
	if err := store.Write(first, []byte("first-secret\n")); err != nil {
		t.Fatalf("Write first: %v", err)
	}
	if err := store.Write(second, []byte("second-secret\n")); err != nil {
		t.Fatalf("Write second: %v", err)
	}
	// Corrupt the second material (sorted after the first) so its decrypt fails
	// mid-loop, after the first has already been written as plaintext.
	path := store.materialPath(second)
	env := readTestEnvelope(t, path)
	ciphertext, err := base64.StdEncoding.DecodeString(env.Ciphertext)
	if err != nil {
		t.Fatalf("decode ciphertext: %v", err)
	}
	ciphertext[0] ^= 0xff
	env.Ciphertext = base64.StdEncoding.EncodeToString(ciphertext)
	writeTestEnvelope(t, path, env)

	targetDir := filepath.Join(t.TempDir(), "runtime-secrets")
	if err := store.MaterializeRuntime(targetDir); err == nil {
		t.Fatal("MaterializeRuntime succeeded despite a corrupt material")
	}
	if entries, statErr := os.ReadDir(targetDir); statErr == nil && len(entries) > 0 {
		t.Fatalf("failed materialization left partial plaintext on disk: %v", entries)
	}
}

func TestContextStoreRejectsWrongOwnerPolicy(t *testing.T) {
	dir := t.TempDir()
	store := NewContextStore("lab", dir)
	key := MaterialKey{Name: "demo", Role: MaterialPrimary}
	if err := store.Write(key, []byte("secret\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	wrongOwnerStore := NewContextStoreWithOptions(ContextStoreOptions{
		ContextName: "lab",
		SecretsDir:  dir,
		OwnerUID:    os.Geteuid() + 1,
	})
	if _, err := wrongOwnerStore.Read(key); err == nil || !strings.Contains(err.Error(), "expected uid") {
		t.Fatalf("Read error = %v, want owner refusal", err)
	}
}

func TestContextStoreBlocksPlaintextOutsideMigration(t *testing.T) {
	store := NewContextStore("lab", t.TempDir())
	path := filepath.Join(store.secretsDir, "demo")
	if err := os.MkdirAll(store.secretsDir, 0o700); err != nil {
		t.Fatalf("mkdir secrets: %v", err)
	}
	if err := os.WriteFile(path, []byte("plaintext\n"), 0o600); err != nil {
		t.Fatalf("write plaintext: %v", err)
	}
	key := MaterialKey{Name: "demo", Role: MaterialPrimary}
	status, err := store.Inspect(key)
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	if status.State != MaterialStatePlaintextBlocked {
		t.Fatalf("state = %s, want plaintext-blocked", status.State)
	}
	if _, err := store.Read(key); err == nil || !strings.Contains(err.Error(), "not encrypted") {
		t.Fatalf("Read error = %v, want plaintext refusal", err)
	}
	if err := store.Write(key, []byte("replacement\n")); err == nil || !strings.Contains(err.Error(), "refusing to overwrite plaintext") {
		t.Fatalf("Write error = %v, want plaintext overwrite refusal", err)
	}
}

func TestContextStoreMigratesPlaintextAndRejectsUnmappableFiles(t *testing.T) {
	store := NewContextStore("lab", t.TempDir())
	if err := os.MkdirAll(store.secretsDir, 0o700); err != nil {
		t.Fatalf("mkdir secrets: %v", err)
	}
	for name, body := range map[string]string{
		"pull-secret": "pull\n",
		"tls.key":     "key\n",
		"ssh.pub":     "public\n",
	} {
		if err := os.WriteFile(filepath.Join(store.secretsDir, name), []byte(body), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	if _, err := store.MigratePlaintext(nil); err != nil {
		t.Fatalf("MigratePlaintext: %v", err)
	}
	for _, tc := range []struct {
		key  MaterialKey
		want string
	}{
		{key: MaterialKey{Name: "pull-secret", Role: MaterialPrimary}, want: "pull\n"},
		{key: MaterialKey{Name: "tls", Role: MaterialTLSKey}, want: "key\n"},
		{key: MaterialKey{Name: "ssh", Role: MaterialSSHPublic}, want: "public\n"},
	} {
		got, err := store.Read(tc.key)
		if err != nil {
			t.Fatalf("Read %s/%s: %v", tc.key.Name, tc.key.Role, err)
		}
		if string(got) != tc.want {
			t.Fatalf("Read %s/%s = %q, want %q", tc.key.Name, tc.key.Role, got, tc.want)
		}
	}
	if err := os.WriteFile(filepath.Join(store.secretsDir, "bad.name"), []byte("bad\n"), 0o600); err != nil {
		t.Fatalf("write unmappable: %v", err)
	}
	if _, err := store.MigratePlaintext(nil); err == nil || !strings.Contains(err.Error(), "unmappable") {
		t.Fatalf("MigratePlaintext error = %v, want unmappable refusal", err)
	}
}

func TestContextStoreRotatesKeysAndKeepsMaterialReadable(t *testing.T) {
	store := NewContextStore("lab", t.TempDir())
	firstKey := MaterialKey{Name: "one", Role: MaterialPrimary}
	secondKey := MaterialKey{Name: "two", Role: MaterialTLSKey}
	if err := store.Write(firstKey, []byte("one\n")); err != nil {
		t.Fatalf("Write one: %v", err)
	}
	if err := store.Write(secondKey, []byte("two\n")); err != nil {
		t.Fatalf("Write two: %v", err)
	}
	before := readTestEnvelope(t, store.materialPath(firstKey))
	beforeKeys, err := store.listKeyIDs()
	if err != nil {
		t.Fatalf("list keys before: %v", err)
	}
	if len(beforeKeys) != 1 {
		t.Fatalf("keys before = %v, want one", beforeKeys)
	}
	if err := store.Rotate(); err != nil {
		t.Fatalf("Rotate: %v", err)
	}
	after := readTestEnvelope(t, store.materialPath(firstKey))
	if after.KeyID == before.KeyID {
		t.Fatalf("active key did not rotate: %s", after.KeyID)
	}
	if after.Ciphertext == before.Ciphertext {
		t.Fatal("ciphertext did not change after rotation")
	}
	for _, tc := range []struct {
		key  MaterialKey
		want string
	}{
		{key: firstKey, want: "one\n"},
		{key: secondKey, want: "two\n"},
	} {
		got, err := store.Read(tc.key)
		if err != nil {
			t.Fatalf("Read after rotate %s/%s: %v", tc.key.Name, tc.key.Role, err)
		}
		if string(got) != tc.want {
			t.Fatalf("Read after rotate = %q, want %q", got, tc.want)
		}
	}
	afterKeys, err := store.listKeyIDs()
	if err != nil {
		t.Fatalf("list keys after: %v", err)
	}
	if len(afterKeys) != 1 || afterKeys[0] == before.KeyID {
		t.Fatalf("keys after = %v, old key %s should be removed", afterKeys, before.KeyID)
	}
}

func readTestEnvelope(t *testing.T, path string) envelope {
	t.Helper()
	var env envelope
	if err := json.Unmarshal(readTestBytes(t, path), &env); err != nil {
		t.Fatalf("decode envelope %s: %v", path, err)
	}
	return env
}

func writeTestEnvelope(t *testing.T, path string, env envelope) {
	t.Helper()
	data, err := json.MarshalIndent(env, "", "  ")
	if err != nil {
		t.Fatalf("encode envelope: %v", err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write envelope %s: %v", path, err)
	}
}

func readTestBytes(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return data
}
