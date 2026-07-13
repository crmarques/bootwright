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

func TestSavePreservesUnmanagedKnownHostsPins(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// A managed-OS node pin appended out of band by the install playbook, plus a
	// stale line for an address the store is about to (re)write.
	seed := "cephnode.example.test ssh-ed25519 TUFOQUdFRA==\nprovider.example.test ssh-ed25519 U1RBTEU=\n"
	if err := os.WriteFile(filepath.Join(dir, KnownHostsName), []byte(seed), 0o600); err != nil {
		t.Fatalf("seed known_hosts: %v", err)
	}
	store := Store{Hosts: []HostRecord{{
		Name:           "provider-01",
		Address:        "provider.example.test",
		KeyType:        "ssh-ed25519",
		PublicKey:      testPublicKey,
		KnownHostsLine: KnownHostsLine("provider.example.test", "ssh-ed25519", testPublicKey),
	}}}
	if err := Save(dir, store); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got := readTrustTestFile(t, filepath.Join(dir, KnownHostsName))
	if !strings.Contains(got, "cephnode.example.test ssh-ed25519 TUFOQUdFRA==") {
		t.Fatalf("Save dropped the unmanaged managed-OS pin: %q", got)
	}
	if !strings.Contains(got, "provider.example.test ssh-ed25519 QUJDRA==") {
		t.Fatalf("Save must write the store's line for a managed address: %q", got)
	}
	if strings.Contains(got, "U1RBTEU=") {
		t.Fatalf("Save must replace the stale line for a store-managed address: %q", got)
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

func TestSaveRejectsDivergentKeysForOneAddress(t *testing.T) {
	dir := t.TempDir()
	store := Store{Hosts: []HostRecord{
		{Name: "node-a", Address: "10.0.0.5", KeyType: "ssh-ed25519", PublicKey: "QUJDRA=="},
		{Name: "node-b", Address: "10.0.0.5", KeyType: "ssh-ed25519", PublicKey: "RUZHSA=="},
	}}
	err := Save(dir, store)
	if err == nil || !strings.Contains(err.Error(), "10.0.0.5") || !strings.Contains(err.Error(), "divergent") {
		t.Fatalf("Save with divergent keys for one address = %v, want a divergent-key error", err)
	}
	if _, statErr := os.Stat(filepath.Join(dir, KnownHostsName)); statErr == nil {
		t.Fatal("known_hosts was written despite the address conflict")
	}
}

func TestSaveAllowsSharedAddressWithIdenticalKey(t *testing.T) {
	dir := t.TempDir()
	store := Store{Hosts: []HostRecord{
		{Name: "node-a", Address: "10.0.0.5", KeyType: "ssh-ed25519", PublicKey: testPublicKey},
		{Name: "node-b", Address: "10.0.0.5", KeyType: "ssh-ed25519", PublicKey: testPublicKey},
	}}
	if err := Save(dir, store); err != nil {
		t.Fatalf("Save with a shared address and identical key: %v", err)
	}
}
