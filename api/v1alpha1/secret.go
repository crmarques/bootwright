package v1alpha1

// Secret declares one named piece of sensitive material and how Bootwright
// obtains it. It is the promoted, first-class form of the former
// Environment.spec.secrets[] entries: every SecretRef in the fleet resolves to
// a Secret by metadata.name.
//
// A Secret never carries material bytes in desired state, so it is safe to
// commit. spec.type says what the material IS — which fixes the material roles
// the secret carries, the legal source arms, and the shape of any generated
// parameters — and spec.source says HOW the material is obtained: the
// per-context encrypted store (contextStore, the default), operator-owned files
// (file), or generation (generated). File paths resolve against this object's
// own file.
type Secret struct {
	APIVersion string     `yaml:"apiVersion" json:"apiVersion"`
	Kind       string     `yaml:"kind" json:"kind"`
	Metadata   Metadata   `yaml:"metadata" json:"metadata"`
	Spec       SecretSpec `yaml:"spec" json:"spec"`
	SourcePath string     `yaml:"-" json:"-"`
}

// SecretSpec is a two-axis declaration: type is what the material IS, source is
// how it is obtained.
type SecretSpec struct {
	// Type is what the secret IS; one of SecretTypes(). It fixes the material
	// roles the secret carries, which source arms are legal, and the shape of
	// spec.source.generated. Required — there is no inference.
	Type string `yaml:"type" json:"type"`
	// Source is how the material is obtained. Omitted (or an explicit empty
	// block) selects contextStore. Exactly one arm may be set, and the legal
	// arms are scoped by Type (see the SecretType* docs).
	Source SecretSource `yaml:"source,omitempty" json:"source,omitempty"`
}

// SecretSource is a presence union of the three ways material is obtained.
// Omitting every arm selects contextStore; at most one arm may be set.
type SecretSource struct {
	// ContextStore keeps the material only in the per-context AES-256-GCM store;
	// the operator populates it with `bootwright secret set|generate`. It is the
	// default (omit the block) and carries no parameters; the empty object
	// exists only for authors who prefer to state it.
	ContextStore *SecretContextStoreSource `yaml:"contextStore,omitempty" json:"contextStore,omitempty"`
	// File points at operator-owned file(s). The populated keys are scoped by
	// the enclosing type: single-file types use path; tlsCertificate uses
	// cert+key; sshKeyPair uses privateKey (+optional publicKey).
	File *SecretFileSource `yaml:"file,omitempty" json:"file,omitempty"`
	// Generated has Bootwright mint the material. Its populated fields are scoped
	// by the enclosing type; legal only for token, usernamePassword,
	// tlsCertificate, and sshKeyPair.
	Generated *SecretGeneratedSource `yaml:"generated,omitempty" json:"generated,omitempty"`
}

// IsZero reports whether no source arm is set (which selects contextStore). It
// keeps spec.source omitempty from emitting an empty block on marshal.
func (s SecretSource) IsZero() bool {
	return s.ContextStore == nil && s.File == nil && s.Generated == nil
}

// SecretContextStoreSource is a marker: its presence (or an omitted source)
// selects the per-context store. Storage handling of file secrets is governed
// globally by Environment.spec.secretStorage.mode, not here.
type SecretContextStoreSource struct{}

// SecretFileSource names operator-owned file(s). Exactly the key set the
// enclosing type consumes may be set; any other key is rejected at validation
// (fields a type does not consume are illegal). Paths are relative to this
// Secret's file, or absolute, or ~-rooted.
type SecretFileSource struct {
	// Path is the single-file form (role: primary). Legal for opaque, token,
	// usernamePassword, dockerConfigJson, and caBundle.
	Path string `yaml:"path,omitempty" json:"path,omitempty"`
	// Cert (role: primary) and Key (role: tls-key) are the tlsCertificate form;
	// both required together.
	Cert string `yaml:"cert,omitempty" json:"cert,omitempty"`
	Key  string `yaml:"key,omitempty" json:"key,omitempty"`
	// PrivateKey (role: ssh-private) and PublicKey (role: ssh-public) are the
	// sshKeyPair form. PrivateKey is required; an omitted PublicKey is derived
	// from it.
	PrivateKey string `yaml:"privateKey,omitempty" json:"privateKey,omitempty"`
	PublicKey  string `yaml:"publicKey,omitempty" json:"publicKey,omitempty"`
}

// SecretGeneratedSource holds type-scoped generation parameters directly (no
// inner discriminator: spec.type already selects which fields apply). Setting a
// field that the enclosing type does not consume is rejected at validation.
type SecretGeneratedSource struct {
	// Username — usernamePassword. The password is randomly generated; an
	// omitted username defaults to "admin".
	Username string `yaml:"username,omitempty" json:"username,omitempty"`

	// CommonName/DNSNames/IPAddresses/ValidityDays — tlsCertificate (self-signed).
	// CommonName is required; ValidityDays defaults to DefaultCertificateDays.
	CommonName   string   `yaml:"commonName,omitempty" json:"commonName,omitempty"`
	DNSNames     []string `yaml:"dnsNames,omitempty" json:"dnsNames,omitempty"`
	IPAddresses  []string `yaml:"ipAddresses,omitempty" json:"ipAddresses,omitempty"`
	ValidityDays int      `yaml:"validityDays,omitempty" json:"validityDays,omitempty"`

	// KeyType/Comment — sshKeyPair. KeyType is one of SSHKeyPairType*
	// (default ed25519). Named KeyType, not Type, because type is reserved.
	KeyType string `yaml:"keyType,omitempty" json:"keyType,omitempty"`
	Comment string `yaml:"comment,omitempty" json:"comment,omitempty"`

	// Bytes — token. Entropy of the random value in bytes (default
	// DefaultTokenBytes).
	Bytes int `yaml:"bytes,omitempty" json:"bytes,omitempty"`
}

// Secret material contracts. Each value is what the material IS; it fixes the
// material roles, the legal source arms, and the shape of the generated params.
const (
	// SecretTypeOpaque is an arbitrary externally-supplied blob the system never
	// mints (kubeconfig, known_hosts, RHSM org/activation-key, external Ceph
	// details, boot-media headers, playbook secrets). Role: primary.
	SecretTypeOpaque = "opaque"
	// SecretTypeToken is a single secret string the system MAY mint (Ceph mgmt
	// oauth2 client/cookie secrets). Role: primary.
	SecretTypeToken = "token"
	// SecretTypeUsernamePassword is a one-line username:password credential
	// (BMC, vCenter, registry, mirror, proxy). Role: primary.
	SecretTypeUsernamePassword = "usernamePassword"
	// SecretTypeDockerConfigJSON is a docker config.json with an .auths object
	// (the OpenShift pull secret). Role: primary.
	SecretTypeDockerConfigJSON = "dockerConfigJson"
	// SecretTypeCABundle is a CERTIFICATE-only PEM trust anchor set (all trust
	// bundles). Role: primary. It may be generated as a self-signed certificate
	// that acts as its own trust anchor (the common self-contained-lab case).
	SecretTypeCABundle = "caBundle"
	// SecretTypeTLSCertificate is a serving identity: cert PEM + private key PEM
	// (API-server named certs, ingress default, Ceph mgmt-gateway). Roles:
	// primary (cert) + tls-key.
	SecretTypeTLSCertificate = "tlsCertificate"
	// SecretTypeSSHKeyPair is an SSH key pair (cluster node SSH, host access,
	// cephadm cluster identity). Roles: ssh-private + ssh-public.
	SecretTypeSSHKeyPair = "sshKeyPair"

	// DefaultTokenBytes is the default entropy for a generated token.
	DefaultTokenBytes = 32
)

// SecretTypes lists every secret material contract, in canonical order. It is
// the single source of truth for the type vocabulary.
func SecretTypes() []string {
	return []string{
		SecretTypeOpaque,
		SecretTypeToken,
		SecretTypeUsernamePassword,
		SecretTypeDockerConfigJSON,
		SecretTypeCABundle,
		SecretTypeTLSCertificate,
		SecretTypeSSHKeyPair,
	}
}

// SecretTypeGeneratable reports whether Bootwright can mint material for a type.
// opaque and dockerConfigJson have no generated arm; caBundle and tlsCertificate
// both generate a self-signed certificate.
func SecretTypeGeneratable(secretType string) bool {
	switch secretType {
	case SecretTypeToken, SecretTypeUsernamePassword, SecretTypeTLSCertificate, SecretTypeCABundle, SecretTypeSSHKeyPair:
		return true
	default:
		return false
	}
}

// SecretTypeSelfSigned reports whether a generated secret of this type mints a
// self-signed certificate (tlsCertificate serves it with a key; caBundle uses
// it as a trust anchor).
func SecretTypeSelfSigned(secretType string) bool {
	return secretType == SecretTypeTLSCertificate || secretType == SecretTypeCABundle
}

// Generated-material value types. These are the structured inputs the
// materializer and crypto layer consume; a Secret's flat
// spec.source.generated params are projected onto the one that matches its
// type.

// GeneratedCredentialsSpec parameterizes a generated usernamePassword secret.
type GeneratedCredentialsSpec struct {
	Username string `yaml:"username,omitempty" json:"username,omitempty"`
}

// GeneratedSSHKeyPairSpec parameterizes a generated sshKeyPair secret. Type is
// the key algorithm (one of SSHKeyPairType*).
type GeneratedSSHKeyPairSpec struct {
	Type    string `yaml:"type,omitempty" json:"type,omitempty"`
	Comment string `yaml:"comment,omitempty" json:"comment,omitempty"`
}

// SelfSignedCertificateSpec parameterizes a generated tlsCertificate secret.
type SelfSignedCertificateSpec struct {
	CommonName   string   `yaml:"commonName" json:"commonName"`
	DNSNames     []string `yaml:"dnsNames,omitempty" json:"dnsNames,omitempty"`
	IPAddresses  []string `yaml:"ipAddresses,omitempty" json:"ipAddresses,omitempty"`
	ValidityDays int      `yaml:"validityDays,omitempty" json:"validityDays,omitempty"`
}

// GeneratedCredentials projects the flat generated params onto the
// usernamePassword value type.
func (s SecretGeneratedSource) GeneratedCredentials() GeneratedCredentialsSpec {
	return GeneratedCredentialsSpec{Username: s.Username}
}

// GeneratedSSHKeyPair projects the flat generated params onto the sshKeyPair
// value type (keyType maps onto the algorithm field).
func (s SecretGeneratedSource) GeneratedSSHKeyPair() GeneratedSSHKeyPairSpec {
	return GeneratedSSHKeyPairSpec{Type: s.KeyType, Comment: s.Comment}
}

// SelfSignedCertificate projects the flat generated params onto the
// tlsCertificate value type.
func (s SecretGeneratedSource) SelfSignedCertificate() SelfSignedCertificateSpec {
	return SelfSignedCertificateSpec{
		CommonName:   s.CommonName,
		DNSNames:     s.DNSNames,
		IPAddresses:  s.IPAddresses,
		ValidityDays: s.ValidityDays,
	}
}
