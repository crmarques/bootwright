package artifactserver

import (
	"crypto/rsa"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"net/netip"
	"slices"
	"strconv"
	"time"

	"github.com/crmarques/bootwright/internal/secrets"
)

const (
	maxCertificateBytes = 1 << 20
	maxPEMBlocks        = 16
	minimumRSABits      = 2048
)

// ServingCertificate is the non-secret evidence a validated certificate
// yields: what the server will present, so readiness can prove the running
// service is serving the material this operation bound.
type ServingCertificate struct {
	Fingerprint string
	NotAfter    time.Time
}

// ValidateServingCertificate proves the bound material can serve every address
// an HTTPS endpoint answers on, before any connection or installation. It
// never reports material, a material digest of the private key, or the reason
// in terms a caller could use to probe the bytes.
func ValidateServingCertificate(material secrets.Material, addresses []string, now time.Time) (ServingCertificate, error) {
	certificatePEM, ok := material.Part(secrets.CertificatePart)
	if !ok {
		return ServingCertificate{}, refusal("secret.part", "the serving certificate Secret has no certificate part", "store a tlsCertificate Secret with its certificate and private key")
	}
	keyPEM, ok := material.Part(secrets.PrivateKeyPart)
	if !ok {
		return ServingCertificate{}, refusal("secret.part", "the serving certificate Secret has no private-key part", "store a tlsCertificate Secret with its certificate and private key")
	}
	if len(certificatePEM) > maxCertificateBytes || len(keyPEM) > maxCertificateBytes {
		return ServingCertificate{}, refusal("secret.part", "the serving certificate material exceeds its bounds", "store a smaller certificate chain")
	}
	if err := boundedPEM(certificatePEM); err != nil {
		return ServingCertificate{}, err
	}
	pair, err := tls.X509KeyPair(certificatePEM, keyPEM)
	if err != nil || len(pair.Certificate) == 0 {
		return ServingCertificate{}, refusal("secret.part", "the serving certificate and private key do not form a usable pair", "regenerate or replace the tlsCertificate Secret")
	}
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		return ServingCertificate{}, refusal("secret.part", "the serving certificate could not be parsed", "regenerate or replace the tlsCertificate Secret")
	}
	if key, ok := leaf.PublicKey.(*rsa.PublicKey); ok && key.N.BitLen() < minimumRSABits {
		return ServingCertificate{}, refusal("secret.part", "the serving certificate's RSA key has "+strconv.Itoa(key.N.BitLen())+" bits; a serving key needs at least 2048",
			"replace the tlsCertificate Secret with an RSA-2048 or P-256 certificate")
	}
	if now.Before(leaf.NotBefore) || now.After(leaf.NotAfter) {
		return ServingCertificate{}, refusal("secret.part", "the serving certificate is not valid at this time", "regenerate the tlsCertificate Secret with secret generate --renew")
	}
	if leaf.IsCA {
		return ServingCertificate{}, refusal("secret.part", "a certificate authority cannot be used as a serving certificate", "replace the tlsCertificate Secret with a server certificate")
	}
	if !serverAuthentication(leaf) {
		return ServingCertificate{}, refusal("secret.part", "the serving certificate is not usable for server authentication", "replace the tlsCertificate Secret with a server certificate")
	}
	for _, address := range addresses {
		if err := leaf.VerifyHostname(address); err != nil {
			return ServingCertificate{}, refusal("secret.part", "the serving certificate does not cover every address its HTTPS endpoints answer on", "add "+address+" to the certificate's subject alternative names and regenerate it")
		}
	}
	digest := sha256.Sum256(pair.Certificate[0])
	return ServingCertificate{Fingerprint: hex.EncodeToString(digest[:]), NotAfter: leaf.NotAfter}, nil
}

// boundedPEM refuses a chain that would make parsing unbounded work before any
// certificate is interpreted.
func boundedPEM(data []byte) error {
	blocks := 0
	for rest := data; len(rest) != 0; {
		block, remainder := pem.Decode(rest)
		if block == nil {
			break
		}
		blocks++
		if blocks > maxPEMBlocks {
			return refusal("secret.part", "the serving certificate chain has too many blocks", "store a shorter certificate chain")
		}
		rest = remainder
	}
	if blocks == 0 {
		return refusal("secret.part", "the serving certificate material contains no PEM block", "store a PEM-encoded certificate")
	}
	return nil
}

func serverAuthentication(leaf *x509.Certificate) bool {
	if leaf.KeyUsage != 0 && leaf.KeyUsage&(x509.KeyUsageDigitalSignature|x509.KeyUsageKeyEncipherment) == 0 {
		return false
	}
	if len(leaf.ExtKeyUsage) == 0 && len(leaf.UnknownExtKeyUsage) == 0 {
		return true
	}
	return slices.ContainsFunc(leaf.ExtKeyUsage, func(usage x509.ExtKeyUsage) bool {
		return usage == x509.ExtKeyUsageServerAuth || usage == x509.ExtKeyUsageAny
	})
}

// CanonicalAddress normalizes an IP literal so certificate coverage and
// reservation keys compare the same text the server binds.
func CanonicalAddress(value string) string {
	if address, err := netip.ParseAddr(value); err == nil {
		return address.String()
	}
	return value
}
