package sshtrust

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testPublicKey = "QUJDRA=="

func TestParseScanOutputSelectsPreferredKey(t *testing.T) {
	keys, err := ParseScanOutput("host.example.test", []byte(`
# host.example.test banner
host.example.test ssh-rsa QUJD
host.example.test ssh-ed25519 QUJDRA==
`))
	if err != nil {
		t.Fatalf("ParseScanOutput: %v", err)
	}
	key, ok := SelectPreferred(keys)
	if !ok {
		t.Fatal("SelectPreferred returned no key")
	}
	if key.KeyType != "ssh-ed25519" {
		t.Fatalf("key type = %q, want ssh-ed25519", key.KeyType)
	}
	if !strings.HasPrefix(key.FingerprintSHA256, "SHA256:") {
		t.Fatalf("fingerprint = %q", key.FingerprintSHA256)
	}
	if key.KnownHostsLine != "host.example.test ssh-ed25519 QUJDRA==" {
		t.Fatalf("known_hosts line = %q", key.KnownHostsLine)
	}
}

func TestSaveWritesStoreAndKnownHosts(t *testing.T) {
	dir := t.TempDir()
	fingerprint, err := FingerprintSHA256(testPublicKey)
	if err != nil {
		t.Fatalf("FingerprintSHA256: %v", err)
	}
	store := Store{Hosts: []HostRecord{{
		Name:              "provider-01",
		Address:           "provider.example.test",
		KeyType:           "ssh-ed25519",
		PublicKey:         testPublicKey,
		FingerprintSHA256: fingerprint,
		KnownHostsLine:    KnownHostsLine("provider.example.test", "ssh-ed25519", testPublicKey),
	}}}
	if err := Save(dir, store); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if got := readTrustTestFile(t, filepath.Join(dir, KnownHostsName)); got != "provider.example.test ssh-ed25519 QUJDRA==\n" {
		t.Fatalf("known_hosts = %q", got)
	}
	loaded, err := Load(filepath.Join(dir, StoreFileName))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if _, ok := loaded.Find("provider-01"); !ok {
		t.Fatalf("loaded store missing provider-01: %+v", loaded)
	}
}

func readTrustTestFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}
