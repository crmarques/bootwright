package sshtrust

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/crmarques/bootwright/internal/host/safefs"
)

const (
	TrustDirName   = "trust"
	SSHDirName     = "ssh"
	StoreFileName  = "hosts.json"
	KnownHostsName = "known_hosts"
	localDirMode   = 0o700
	localFileMode  = 0o600
)

type Store struct {
	Hosts []HostRecord `json:"hosts"`
}

type HostRecord struct {
	Name              string `json:"name"`
	Address           string `json:"address"`
	KeyType           string `json:"keyType"`
	PublicKey         string `json:"publicKey"`
	FingerprintSHA256 string `json:"fingerprintSHA256"`
	KnownHostsLine    string `json:"knownHostsLine"`
}

type ScannedKey struct {
	Address           string
	KeyType           string
	PublicKey         string
	FingerprintSHA256 string
	KnownHostsLine    string
}

func DirForContext(contextDir string) string {
	if strings.TrimSpace(contextDir) == "" {
		return ""
	}
	return filepath.Join(contextDir, TrustDirName, SSHDirName)
}

func DirForSecrets(secretsDir string) string {
	if strings.TrimSpace(secretsDir) == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(secretsDir), TrustDirName, SSHDirName)
}

func StorePathForContext(contextDir string) string {
	dir := DirForContext(contextDir)
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, StoreFileName)
}

func StorePathForSecrets(secretsDir string) string {
	dir := DirForSecrets(secretsDir)
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, StoreFileName)
}

func KnownHostsPathForContext(contextDir string) string {
	dir := DirForContext(contextDir)
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, KnownHostsName)
}

func KnownHostsPathForSecrets(secretsDir string) string {
	dir := DirForSecrets(secretsDir)
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, KnownHostsName)
}

func Load(path string) (Store, error) {
	var store Store
	if strings.TrimSpace(path) == "" {
		return store, nil
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return store, nil
	}
	if err != nil {
		return Store{}, fmt.Errorf("read %s: %w", path, err)
	}
	if err := json.Unmarshal(data, &store); err != nil {
		return Store{}, fmt.Errorf("decode %s: %w", path, err)
	}
	sortStore(store)
	return store, nil
}

// ValidateAddressConsistency fails closed when two records pin the same address
// to different keys. OpenSSH accepts any matching known_hosts entry for a host,
// so emitting two divergent lines for one address would let a peer present
// *either* key and still pass StrictHostKeyChecking — silently weakening the
// trust-on-first-use pin from "exactly this key" to "either key". The store is
// keyed by Machine name, but two Machines may resolve to one address, so the
// invariant is enforced here at the single point that writes known_hosts.
func (s Store) ValidateAddressConsistency() error {
	type hostKey struct{ keyType, publicKey string }
	pinned := map[string]hostKey{}
	owner := map[string]string{}
	for _, record := range s.Hosts {
		address := strings.TrimSpace(record.Address)
		if address == "" {
			continue
		}
		current := hostKey{record.KeyType, record.PublicKey}
		if existing, ok := pinned[address]; ok {
			if existing != current {
				return fmt.Errorf("SSH trust store pins address %s to divergent keys for hosts %q and %q; remove or re-point one before saving", address, owner[address], record.Name)
			}
			continue
		}
		pinned[address] = current
		owner[address] = record.Name
	}
	return nil
}

func Save(dir string, store Store) error {
	if strings.TrimSpace(dir) == "" {
		return errors.New("SSH trust directory is required")
	}
	if err := store.ValidateAddressConsistency(); err != nil {
		return err
	}
	sortStore(store)
	if err := os.MkdirAll(dir, localDirMode); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}
	if err := os.Chmod(dir, localDirMode); err != nil {
		return fmt.Errorf("chmod %s: %w", dir, err)
	}
	data, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal SSH trust store: %w", err)
	}
	data = append(data, '\n')
	if err := safefs.AtomicWriteFile(filepath.Join(dir, StoreFileName), data, localFileMode); err != nil {
		return err
	}
	if err := safefs.AtomicWriteFile(filepath.Join(dir, KnownHostsName), []byte(KnownHosts(store)), localFileMode); err != nil {
		return err
	}
	return nil
}

func KnownHosts(store Store) string {
	sortStore(store)
	var lines []string
	for _, record := range store.Hosts {
		line := strings.TrimSpace(record.KnownHostsLine)
		if line == "" {
			line = KnownHostsLine(record.Address, record.KeyType, record.PublicKey)
		}
		if line != "" {
			lines = append(lines, line)
		}
	}
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "\n") + "\n"
}

func (s Store) Find(name string) (HostRecord, bool) {
	for _, record := range s.Hosts {
		if record.Name == name {
			return record, true
		}
	}
	return HostRecord{}, false
}

func (s *Store) Upsert(record HostRecord) {
	for i := range s.Hosts {
		if s.Hosts[i].Name == record.Name {
			s.Hosts[i] = record
			sortStore(*s)
			return
		}
	}
	s.Hosts = append(s.Hosts, record)
	sortStore(*s)
}

func ParseScanOutput(address string, data []byte) ([]ScannedKey, error) {
	var out []ScannedKey
	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		keyType := fields[1]
		publicKey := fields[2]
		fingerprint, err := FingerprintSHA256(publicKey)
		if err != nil {
			return nil, err
		}
		out = append(out, ScannedKey{
			Address:           address,
			KeyType:           keyType,
			PublicKey:         publicKey,
			FingerprintSHA256: fingerprint,
			KnownHostsLine:    KnownHostsLine(address, keyType, publicKey),
		})
	}
	sortScannedKeys(out)
	return out, nil
}

func SelectPreferred(keys []ScannedKey) (ScannedKey, bool) {
	if len(keys) == 0 {
		return ScannedKey{}, false
	}
	for _, preferred := range []string{"ssh-ed25519", "ecdsa-sha2-nistp256", "ssh-rsa"} {
		for _, key := range keys {
			if key.KeyType == preferred {
				return key, true
			}
		}
	}
	return keys[0], true
}

func FingerprintSHA256(publicKey string) (string, error) {
	blob, err := base64.StdEncoding.DecodeString(publicKey)
	if err != nil {
		return "", fmt.Errorf("decode SSH public key: %w", err)
	}
	sum := sha256.Sum256(blob)
	return "SHA256:" + base64.RawStdEncoding.EncodeToString(sum[:]), nil
}

func KnownHostsLine(address, keyType, publicKey string) string {
	if strings.TrimSpace(address) == "" || strings.TrimSpace(keyType) == "" || strings.TrimSpace(publicKey) == "" {
		return ""
	}
	return address + " " + keyType + " " + publicKey
}

func sortStore(store Store) {
	sort.Slice(store.Hosts, func(i, j int) bool {
		return store.Hosts[i].Name < store.Hosts[j].Name
	})
}

func sortScannedKeys(keys []ScannedKey) {
	priority := map[string]int{
		"ssh-ed25519":         0,
		"ecdsa-sha2-nistp256": 1,
		"ssh-rsa":             2,
	}
	sort.Slice(keys, func(i, j int) bool {
		pi, iok := priority[keys[i].KeyType]
		if !iok {
			pi = 100
		}
		pj, jok := priority[keys[j].KeyType]
		if !jok {
			pj = 100
		}
		if pi != pj {
			return pi < pj
		}
		return keys[i].KeyType < keys[j].KeyType
	})
}
