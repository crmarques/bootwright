package secrets

import (
	"errors"
	"slices"
)

const (
	MaxPartBytes     = 1 << 20
	MaxVersionBytes  = 2 << 20
	MaxMaterialBytes = 64 << 20
	MaxVersions      = 4096
)

type Part string

const (
	ValuePart       Part = "value"
	UsernamePart    Part = "username"
	PasswordPart    Part = "password"
	CertificatePart Part = "certificate"
	PrivateKeyPart  Part = "private-key"
	PublicKeyPart   Part = "public-key"
)

// Material owns confidential bytes. Formatting and JSON never reveal them.
type Material struct{ parts map[Part][]byte }

func NewMaterial(parts map[Part][]byte) Material {
	m := Material{parts: make(map[Part][]byte, len(parts))}
	for part, value := range parts {
		m.parts[part] = slices.Clone(value)
	}
	return m
}

func (m Material) Parts() []Part {
	parts := make([]Part, 0, len(m.parts))
	for p := range m.parts {
		parts = append(parts, p)
	}
	slices.Sort(parts)
	return parts
}

func (m Material) Part(part Part) ([]byte, bool) { b, ok := m.parts[part]; return slices.Clone(b), ok }
func (m Material) Size() int {
	n := 0
	for _, b := range m.parts {
		n += len(b)
	}
	return n
}
func (m Material) Clear() {
	for _, b := range m.parts {
		clear(b)
	}
}
func (m Material) String() string   { return "[confidential material]" }
func (m Material) GoString() string { return m.String() }
func (m Material) MarshalJSON() ([]byte, error) {
	return nil, errors.New("confidential material cannot be serialized")
}

type Generation struct {
	Username     string   `json:"username"`
	CommonName   string   `json:"commonName"`
	DNSNames     []string `json:"dnsNames"`
	IPAddresses  []string `json:"ipAddresses"`
	ValidityDays int      `json:"validityDays"`
	KeyType      string   `json:"keyType"`
	Comment      string   `json:"comment"`
	Bytes        int      `json:"bytes"`
}

type FileSource struct {
	Path        string `json:"path"`
	Certificate string `json:"certificate"`
	PrivateKey  string `json:"privateKey"`
	PublicKey   string `json:"publicKey"`
}

type Declaration struct {
	Name        string     `json:"name"`
	Type        string     `json:"type"`
	Source      string     `json:"source"`
	Files       FileSource `json:"files"`
	Generation  Generation `json:"generation"`
	Origin      string     `json:"origin"`
	Document    int        `json:"document"`
	Fingerprint string     `json:"fingerprint"`
}

// VersionDeclaration identifies acquired material without retaining acquisition
// paths or generation parameters. Fingerprint covers the complete declaration.
type VersionDeclaration struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Source      string `json:"source"`
	Fingerprint string `json:"fingerprint"`
}

func (d Declaration) Summary() VersionDeclaration {
	return VersionDeclaration{Name: d.Name, Type: d.Type, Source: d.Source, Fingerprint: d.Fingerprint}
}

func (d Declaration) Parts() []Part { return d.Summary().Parts() }

func (d VersionDeclaration) Parts() []Part {
	switch d.Type {
	case "opaque", "token", "dockerConfigJson":
		return []Part{ValuePart}
	case "usernamePassword":
		return []Part{PasswordPart, UsernamePart}
	case "caBundle":
		if d.Source == "generated" {
			return []Part{CertificatePart, PrivateKeyPart}
		}
		return []Part{CertificatePart}
	case "tlsCertificate":
		return []Part{CertificatePart, PrivateKeyPart}
	case "sshKeyPair":
		return []Part{PrivateKeyPart, PublicKeyPart}
	default:
		return []Part{}
	}
}

// Input is an explicit acquisition request. Stdin is read lazily by its port.
type Input struct {
	Provided        InputFields
	ValueFile       string
	ValueStdin      bool
	Username        string
	PasswordFile    string
	PasswordStdin   bool
	CertificateFile string
	PrivateKeyFile  string
	PublicKeyFile   string
}

func (i Input) UsesStdin() bool { return i.ValueStdin || i.PasswordStdin }

// InputFields preserves explicit empty/false flags so they cannot bypass the
// declaration's closed input matrix. Direct service callers may omit it.
type InputFields uint16

const (
	ValueFileInput InputFields = 1 << iota
	ValueStdinInput
	UsernameInput
	PasswordFileInput
	PasswordStdinInput
	CertificateFileInput
	PrivateKeyFileInput
	PublicKeyFileInput
)

func AllowedInputFields(kind string) InputFields {
	switch kind {
	case "opaque", "token", "dockerConfigJson":
		return ValueFileInput | ValueStdinInput
	case "usernamePassword":
		return UsernameInput | PasswordFileInput | PasswordStdinInput
	case "caBundle":
		return CertificateFileInput
	case "tlsCertificate":
		return CertificateFileInput | PrivateKeyFileInput
	case "sshKeyPair":
		return PrivateKeyFileInput | PublicKeyFileInput
	}
	return 0
}
