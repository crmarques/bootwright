package trust

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/crmarques/bootwright/internal/diagnostics"
)

const (
	defaultPort  = 22
	maxKeyBytes  = 16 << 10
	maxLineBytes = 32 << 10
)

// algorithms is the qualified set of host-key types a session pins. It names
// key types as a known_hosts entry records them, so the RSA signature
// algorithms a server may negotiate with an `ssh-rsa` key are not members.
var algorithms = []string{
	"ssh-ed25519",
	"ecdsa-sha2-nistp256",
	"ecdsa-sha2-nistp384",
	"ecdsa-sha2-nistp521",
	"ssh-rsa",
}

// HostKey is one server identity: the key type a known_hosts entry records and
// the base64 key blob, never a host, a comment or a marker.
type HostKey struct {
	Type      string
	PublicKey string
}

func (k HostKey) Present() bool { return k.Type != "" && k.PublicKey != "" }

// Fingerprint is the value an operator compares before trusting a key. It is
// the SHA-256 digest of the key blob in OpenSSH's own presentation, so what a
// prompt displays is what `ssh-keygen -l` shows for the same key.
func (k HostKey) Fingerprint() string {
	blob, err := k.blob()
	if err != nil {
		return ""
	}
	digest := sha256.Sum256(blob)
	return "SHA256:" + base64.RawStdEncoding.EncodeToString(digest[:])
}

// Algorithms names what a session accepts for this key. An `ssh-rsa` key is
// the one type whose signature algorithms differ from its own name: a server
// holding it may sign with SHA-2, and many refuse the SHA-1 form entirely, so
// pinning the key type alone would refuse the key this record already trusts.
func (k HostKey) Algorithms() string {
	if k.Type == "ssh-rsa" {
		return "rsa-sha2-512,rsa-sha2-256,ssh-rsa"
	}
	return k.Type
}

// Line renders the single known_hosts entry a session pins this key with.
func (k HostKey) Line(address string, port int) string {
	if !k.Present() || address == "" {
		return ""
	}
	return HostToken(address, port) + " " + k.Type + " " + k.PublicKey + "\n"
}

func (k HostKey) blob() ([]byte, error) {
	blob, err := base64.StdEncoding.DecodeString(k.PublicKey)
	if err != nil || len(blob) == 0 || len(blob) > maxKeyBytes {
		return nil, failure("the SSH host key is not a bounded base64 key blob", "")
	}
	return blob, nil
}

// blobAlgorithm reads the algorithm name an SSH public key blob starts with.
// Every key type encodes it the same way, as a length-prefixed string, so this
// identifies what a blob actually is without interpreting the key itself.
func blobAlgorithm(blob []byte) (string, bool) {
	if len(blob) < 4 {
		return "", false
	}
	length := binary.BigEndian.Uint32(blob[:4])
	if length == 0 || uint64(length) > uint64(len(blob)-4) {
		return "", false
	}
	return string(blob[4 : 4+length]), true
}

// HostToken is the host field of a known_hosts entry: the bare address on the
// default port, and the bracketed form otherwise.
func HostToken(address string, port int) string {
	if port == 0 || port == defaultPort {
		return address
	}
	return "[" + address + "]:" + strconv.Itoa(port)
}

// ParseAuthorizedKey reads one `<type> <blob> [comment]` line, which is the
// shape an installation publishes its host key in.
func ParseAuthorizedKey(line string) (HostKey, error) {
	fields, err := keyFields(line)
	if err != nil {
		return HostKey{}, err
	}
	if len(fields) < 2 {
		return HostKey{}, failure("the SSH host key names no type and key", "")
	}
	return qualify(fields[0], fields[1])
}

// ParseKnownHostsLine reads the exact single-entry known_hosts material a
// Machine's knownHostsRef Secret carries. Every shape OpenSSH would accept for
// more than one host is refused, so one declared target, port and key is bound
// before anything is observed.
func ParseKnownHostsLine(text, address string, port int) (HostKey, error) {
	fields, err := keyFields(text)
	if err != nil {
		return HostKey{}, err
	}
	if len(fields) < 3 {
		return HostKey{}, failure("the known-hosts entry names no host, type and key", "")
	}
	host := fields[0]
	switch {
	case strings.HasPrefix(host, "@"):
		return HostKey{}, failure("a known-hosts marker is not a bound host key", "")
	case strings.HasPrefix(host, "|"):
		return HostKey{}, failure("a hashed known-hosts host cannot be bound to a declared address", "")
	case strings.ContainsAny(host, ",*?!"):
		return HostKey{}, failure("a known-hosts pattern or host list is not one bound host key", "")
	case host != HostToken(address, port):
		return HostKey{}, failure("the known-hosts entry names another host than this Machine's SSH address",
			"author the entry for "+HostToken(address, port))
	}
	return qualify(fields[1], fields[2])
}

// keyFields reduces material to the fields of its single data line. Anything
// carrying a second entry is refused rather than silently reduced to its first.
func keyFields(text string) ([]string, error) {
	if len(text) > maxLineBytes || !utf8.ValidString(text) {
		return nil, failure("the SSH host key is not bounded UTF-8 text", "")
	}
	var data string
	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if data != "" {
			return nil, failure("the SSH host key material carries more than one entry", "")
		}
		data = line
	}
	if data == "" {
		return nil, failure("the SSH host key material carries no entry", "")
	}
	return strings.Fields(data), nil
}

// qualify accepts a key only when its recorded type is one this product pins
// and the blob it decodes to agrees, so a mislabeled entry never reaches a
// session as a type it would then pin.
func qualify(keyType, publicKey string) (HostKey, error) {
	if !known(keyType) {
		return HostKey{}, failure("the SSH host key type is not one this product accepts",
			"use one of "+strings.Join(algorithms, ", "))
	}
	key := HostKey{Type: keyType, PublicKey: publicKey}
	blob, err := key.blob()
	if err != nil {
		return HostKey{}, err
	}
	algorithm, ok := blobAlgorithm(blob)
	if !ok {
		return HostKey{}, failure("the SSH host key could not be decoded", "")
	}
	if algorithm != keyType {
		return HostKey{}, failure("the SSH host key type contradicts the key it names", "")
	}
	return key, nil
}

func known(keyType string) bool {
	for _, allowed := range algorithms {
		if allowed == keyType {
			return true
		}
	}
	return false
}

func failure(message, remediation string) error {
	return diagnostics.NewFailureWithRemediation("trust.identity", message, "", remediation)
}
