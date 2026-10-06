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

// Lend narrows this material to the named parts for a consumer that reads only
// those. The view shares these bytes rather than copying them, so it leaves no
// copy behind and clearing this material clears it too; only the lender clears
// it. A part this material does not hold stays absent.
func (m Material) Lend(parts ...Part) Material {
	lent := Material{parts: make(map[Part][]byte, len(parts))}
	for _, part := range parts {
		if value, ok := m.parts[part]; ok {
			lent.parts[part] = value
		}
	}
	return lent
}

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

// FileSource is always empty now that the file source is retired. It stays in
// the Declaration encoding every stored version's fingerprint was computed over.
type FileSource struct {
	Path        string `json:"path"`
	Certificate string `json:"certificate"`
	PrivateKey  string `json:"privateKey"`
	PublicKey   string `json:"publicKey"`
}

// Declaration is the non-secret identity of a declared Secret. Origin and
// Document stay in the encoding only for the legacy fingerprint: a live
// declaration leaves them empty, so its fingerprint covers type, source and
// parameters alone. An earlier build's fingerprint also covered the declaring
// file's path and document index; LegacyFingerprint is that digest, over
// LegacyOrigin and LegacyDocument, and none of the three is encoded.
type Declaration struct {
	Name              string     `json:"name"`
	Type              string     `json:"type"`
	Source            string     `json:"source"`
	Files             FileSource `json:"files"`
	Generation        Generation `json:"generation"`
	Origin            string     `json:"origin"`
	Document          int        `json:"document"`
	Fingerprint       string     `json:"fingerprint"`
	LegacyFingerprint string     `json:"-"`
	LegacyOrigin      string     `json:"-"`
	LegacyDocument    int        `json:"-"`
}

// Current reports whether a version stored under fingerprint is current for
// this declaration: stored under its fingerprint, or under the legacy one.
func (d Declaration) Current(fingerprint string) bool {
	return fingerprint == d.Fingerprint || d.LegacyFingerprint != "" && fingerprint == d.LegacyFingerprint
}

// Stored is this declaration in the encoding a version stored under
// fingerprint was written with, which a binding of that version carries.
func (d Declaration) Stored(fingerprint string) Declaration {
	if fingerprint != d.Fingerprint && d.LegacyFingerprint != "" && fingerprint == d.LegacyFingerprint {
		d.Origin, d.Document, d.Fingerprint = d.LegacyOrigin, d.LegacyDocument, d.LegacyFingerprint
	}
	return d
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
