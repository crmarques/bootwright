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

// Material owns confidential bytes. Formatting and JSON never reveal them. The
// bytes sit behind a function because fmt cannot call String on a Material held
// in an unexported field, and under every verb it prints a function only as its
// address, where a pointer or map would be walked.
type Material struct{ held func() map[Part][]byte }

func materialOf(parts map[Part][]byte) Material {
	return Material{held: func() map[Part][]byte { return parts }}
}

func NewMaterial(parts map[Part][]byte) Material {
	held := make(map[Part][]byte, len(parts))
	for part, value := range parts {
		held[part] = slices.Clone(value)
	}
	return materialOf(held)
}

func (m Material) partMap() map[Part][]byte {
	if m.held == nil {
		return nil
	}
	return m.held()
}

func (m Material) Parts() []Part {
	held := m.partMap()
	parts := make([]Part, 0, len(held))
	for p := range held {
		parts = append(parts, p)
	}
	slices.Sort(parts)
	return parts
}

func (m Material) Part(part Part) ([]byte, bool) {
	b, ok := m.partMap()[part]
	return slices.Clone(b), ok
}

// Lend narrows this material to the named parts for a consumer that reads only
// those. The view shares these bytes rather than copying them, so it leaves no
// copy behind and clearing this material clears it too; only the lender clears
// it. A part this material does not hold stays absent.
func (m Material) Lend(parts ...Part) Material {
	held := m.partMap()
	lent := make(map[Part][]byte, len(parts))
	for _, part := range parts {
		if value, ok := held[part]; ok {
			lent[part] = value
		}
	}
	return materialOf(lent)
}

func (m Material) Size() int {
	n := 0
	for _, b := range m.partMap() {
		n += len(b)
	}
	return n
}
func (m Material) Clear() {
	for _, b := range m.partMap() {
		clear(b)
	}
}
func (m Material) String() string   { return "[confidential material]" }
func (m Material) GoString() string { return m.String() }
func (m Material) MarshalJSON() ([]byte, error) {
	return nil, errors.New("confidential material cannot be serialized")
}

type Generation struct {
	IssuerRef    string   `json:"issuerRef,omitempty"`
	Username     string   `json:"username"`
	CommonName   string   `json:"commonName"`
	DNSNames     []string `json:"dnsNames"`
	IPAddresses  []string `json:"ipAddresses"`
	ValidityDays int      `json:"validityDays"`
	KeyType      string   `json:"keyType"`
	Comment      string   `json:"comment"`
	Bytes        int      `json:"bytes"`
}

// Declaration is the non-secret identity of a declared Secret: its type,
// source and parameters, which its fingerprint covers.
type Declaration struct {
	Name        string     `json:"name"`
	Type        string     `json:"type"`
	Source      string     `json:"source"`
	Generation  Generation `json:"generation"`
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
	case "trustBundle":
		return []Part{CertificatePart}
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
// ContextName is the context the material is set in, which its refusals and
// its terminal prompt name.
type Input struct {
	ContextName     string
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
	shape, _ := shapeOf(kind)
	return shape.fields
}
