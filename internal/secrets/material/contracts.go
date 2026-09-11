package material

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rsa"
	"crypto/x509"
)

// Operator resolves the authenticated invoking account only when file material
// is acquired. Runtime-store ownership remains independent of this identity.
type Operator interface {
	FileIdentity(context.Context) (FileIdentity, error)
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
