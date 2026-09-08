package secrets

type SourceKind string

const (
	PullSecretSource     SourceKind = "pull-secret"
	TLSSource            SourceKind = "tls"
	RawFileSource        SourceKind = "raw-file"
	StructuredFileSource SourceKind = "from-file"
	PasswordStdinSource  SourceKind = "password-stdin"
	GeneratedSource      SourceKind = "generate"
)

type Source struct {
	Kind            SourceKind
	File            string
	CertificateFile string
	PrivateKeyFile  string
}
