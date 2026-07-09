package v1alpha1

type Secret struct {
	APIVersion string     `yaml:"apiVersion" json:"apiVersion"`
	Kind       string     `yaml:"kind" json:"kind"`
	Metadata   Metadata   `yaml:"metadata" json:"metadata"`
	Spec       SecretSpec `yaml:"spec" json:"spec"`
	SourcePath string     `yaml:"-" json:"-"`
}

type SecretSpec struct {
	Type   string       `yaml:"type" json:"type"`
	Source SecretSource `yaml:"source,omitempty" json:"source,omitempty"`
}

type SecretSource struct {
	ContextStore *SecretContextStoreSource `yaml:"contextStore,omitempty" json:"contextStore,omitempty"`
	File         *SecretFileSource         `yaml:"file,omitempty" json:"file,omitempty"`
	Generated    *SecretGeneratedSource    `yaml:"generated,omitempty" json:"generated,omitempty"`
}

func (s SecretSource) IsZero() bool {
	return s.ContextStore == nil && s.File == nil && s.Generated == nil
}

type SecretContextStoreSource struct{}

type SecretFileSource struct {
	Path       string `yaml:"path,omitempty" json:"path,omitempty"`
	Cert       string `yaml:"cert,omitempty" json:"cert,omitempty"`
	Key        string `yaml:"key,omitempty" json:"key,omitempty"`
	PrivateKey string `yaml:"privateKey,omitempty" json:"privateKey,omitempty"`
	PublicKey  string `yaml:"publicKey,omitempty" json:"publicKey,omitempty"`
}

type SecretGeneratedSource struct {
	Username string `yaml:"username,omitempty" json:"username,omitempty"`

	CommonName   string   `yaml:"commonName,omitempty" json:"commonName,omitempty"`
	DNSNames     []string `yaml:"dnsNames,omitempty" json:"dnsNames,omitempty"`
	IPAddresses  []string `yaml:"ipAddresses,omitempty" json:"ipAddresses,omitempty"`
	ValidityDays int      `yaml:"validityDays,omitempty" json:"validityDays,omitempty"`

	KeyType string `yaml:"keyType,omitempty" json:"keyType,omitempty"`
	Comment string `yaml:"comment,omitempty" json:"comment,omitempty"`

	Bytes int `yaml:"bytes,omitempty" json:"bytes,omitempty"`
}

const (
	SecretTypeOpaque           = "opaque"
	SecretTypeToken            = "token"
	SecretTypeUsernamePassword = "usernamePassword"
	SecretTypeDockerConfigJSON = "dockerConfigJson"
	SecretTypeCABundle         = "caBundle"
	SecretTypeTLSCertificate   = "tlsCertificate"
	SecretTypeSSHKeyPair       = "sshKeyPair"

	DefaultTokenBytes = 32
)

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

func SecretTypeGeneratable(secretType string) bool {
	switch secretType {
	case SecretTypeToken, SecretTypeUsernamePassword, SecretTypeTLSCertificate, SecretTypeCABundle, SecretTypeSSHKeyPair:
		return true
	default:
		return false
	}
}

func SecretTypeSelfSigned(secretType string) bool {
	return secretType == SecretTypeTLSCertificate || secretType == SecretTypeCABundle
}

type GeneratedCredentialsSpec struct {
	Username string `yaml:"username,omitempty" json:"username,omitempty"`
}

type GeneratedSSHKeyPairSpec struct {
	Type    string `yaml:"type,omitempty" json:"type,omitempty"`
	Comment string `yaml:"comment,omitempty" json:"comment,omitempty"`
}

type SelfSignedCertificateSpec struct {
	CommonName   string   `yaml:"commonName" json:"commonName"`
	DNSNames     []string `yaml:"dnsNames,omitempty" json:"dnsNames,omitempty"`
	IPAddresses  []string `yaml:"ipAddresses,omitempty" json:"ipAddresses,omitempty"`
	ValidityDays int      `yaml:"validityDays,omitempty" json:"validityDays,omitempty"`
}

func (s SecretGeneratedSource) GeneratedCredentials() GeneratedCredentialsSpec {
	return GeneratedCredentialsSpec{Username: s.Username}
}

func (s SecretGeneratedSource) GeneratedSSHKeyPair() GeneratedSSHKeyPairSpec {
	return GeneratedSSHKeyPairSpec{Type: s.KeyType, Comment: s.Comment}
}

func (s SecretGeneratedSource) SelfSignedCertificate() SelfSignedCertificateSpec {
	return SelfSignedCertificateSpec{
		CommonName:   s.CommonName,
		DNSNames:     s.DNSNames,
		IPAddresses:  s.IPAddresses,
		ValidityDays: s.ValidityDays,
	}
}
