package material

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rsa"
	"crypto/x509"
	"os"
)

// Operator resolves the authenticated invoking account only when file material
// is acquired. Runtime-store ownership remains independent of this identity.
type Operator interface {
	FileIdentity(context.Context) (FileIdentity, error)
}

// Files begins one acquisition's access to operator-named secret files under
// the invoking account's credentials.
type Files interface {
	Begin(context.Context) (FileSession, error)
}

// FileSession opens "/" and then one name at a time beneath a directory it
// issued, never following a link at that name. Acquisition proves every
// descriptor it receives.
type FileSession interface {
	Root() (*os.File, error)
	OpenAt(*os.File, string, int) (*os.File, error)
	Close() error
}

type InputReader interface {
	Read(context.Context, []byte) (int, error)
}

// Cryptography isolates non-context-aware key and certificate operations whose
// production implementations always use the standard library's OS entropy.
type Cryptography interface {
	GenerateECDSA(elliptic.Curve) (*ecdsa.PrivateKey, error)
	GenerateRSA(int) (*rsa.PrivateKey, error)
	CreateCertificate(*x509.Certificate, *x509.Certificate, crypto.PublicKey, crypto.Signer) ([]byte, error)
}
