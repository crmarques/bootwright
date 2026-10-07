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

// ValidateServingCertificate proves the bound material of Secret secret can
// serve every address an HTTPS endpoint answers on, before any connection or
// installation. Every refusal names that Secret, the unmet condition and the
// command that stores usable material in context contextName, and ends with the
// exit that works: the operation keeps the version it bound, so new material
// serves only a fresh apply after this one is destroyed. It never reports
// material, a material digest of the private key, or the reason in terms a
// caller could use to probe the bytes.
func ValidateServingCertificate(material secrets.Material, secret, contextName string, addresses []string, now time.Time) (ServingCertificate, error) {
	remedy := servingRemedies{secret: secret, context: contextName}
	certificatePEM, ok := material.Part(secrets.CertificatePart)
	if !ok {
		return ServingCertificate{}, remedy.refuse("the serving certificate Secret has no certificate part", remedy.replace("store the certificate and its private key"))
	}
	keyPEM, ok := material.Part(secrets.PrivateKeyPart)
	if !ok {
		return ServingCertificate{}, remedy.refuse("the serving certificate Secret has no private-key part", remedy.replace("store the certificate and its private key"))
	}
	if len(certificatePEM) > maxCertificateBytes || len(keyPEM) > maxCertificateBytes {
		return ServingCertificate{}, remedy.refuse("the serving certificate material exceeds its bounds", remedy.replace("store a smaller certificate chain"))
	}
	if message, ok := boundedPEM(certificatePEM); !ok {
		return ServingCertificate{}, remedy.refuse(message, remedy.replace("store a PEM-encoded certificate chain of at most "+strconv.Itoa(maxPEMBlocks)+" blocks"))
	}
	pair, err := tls.X509KeyPair(certificatePEM, keyPEM)
	if err != nil || len(pair.Certificate) == 0 {
		return ServingCertificate{}, remedy.refuse("the serving certificate and private key do not form a usable pair", remedy.replace("store a certificate with its own private key"))
	}
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		return ServingCertificate{}, remedy.refuse("the serving certificate could not be parsed", remedy.replace("store a well-formed certificate"))
	}
	if key, ok := leaf.PublicKey.(*rsa.PublicKey); ok && key.N.BitLen() < minimumRSABits {
		return ServingCertificate{}, remedy.refuse("the serving certificate's RSA key has "+strconv.Itoa(key.N.BitLen())+" bits; a serving key needs at least 2048",
			"replace the tlsCertificate Secret with an RSA-2048 or P-256 certificate, stored with "+remedy.set()+"; "+remedy.exit())
	}
	if now.Before(leaf.NotBefore) || now.After(leaf.NotAfter) {
		return ServingCertificate{}, remedy.refuse("the serving certificate is not valid at this time",
			"renew a generated Secret with "+remedy.renew()+", or store a current certificate with "+remedy.set()+"; "+remedy.exit())
	}
	if leaf.IsCA {
		return ServingCertificate{}, remedy.refuse("a certificate authority cannot be used as a serving certificate", remedy.replace("store a server certificate"))
	}
	if !serverAuthentication(leaf) {
		return ServingCertificate{}, remedy.refuse("the serving certificate is not usable for server authentication", remedy.replace("store a server certificate"))
	}
	for _, address := range addresses {
		if err := leaf.VerifyHostname(address); err != nil {
			return ServingCertificate{}, remedy.refuse("the serving certificate does not name "+address+", which an HTTPS endpoint answers on", remedy.cover(address))
		}
	}
	digest := sha256.Sum256(pair.Certificate[0])
	return ServingCertificate{Fingerprint: hex.EncodeToString(digest[:]), NotAfter: leaf.NotAfter}, nil
}

// servingRemedies words the remedies of one serving certificate Secret: the
// commands that store usable material in its context, and the exit that makes
// an apply use it.
type servingRemedies struct {
	secret  string
	context string
}

func (r servingRemedies) refuse(message, remedy string) error {
	return secrets.Refusal("part", message, r.context, r.secret, remedy)
}

func (r servingRemedies) contextFlag() string {
	if r.context == "" {
		return ""
	}
	return " --context " + r.context
}

func (r servingRemedies) set() string {
	return "bootwright secret set --name " + r.secret + " --certificate-file <path> --private-key-file <path>" + r.contextFlag()
}

func (r servingRemedies) renew() string {
	return "bootwright secret generate --name " + r.secret + " --renew" + r.contextFlag()
}

// exit is the way out of an apply whose bound material refused: the operation
// keeps the Secret version it bound, so it is destroyed and applied again.
func (r servingRemedies) exit() string {
	return "then destroy this apply with bootwright destroy" + r.contextFlag() + " and apply again"
}

func (r servingRemedies) replace(action string) string {
	return action + " with " + r.set() + ", or, for a generated Secret, run " + r.renew() + "; " + r.exit()
}

// cover words the remedy of an address the certificate does not name. A
// generated certificate names what its declaration lists, so the address joins
// that list, the context imports it, and generation re-mints the certificate.
func (r servingRemedies) cover(address string) string {
	field := "spec.source.generated.dnsNames"
	if _, err := netip.ParseAddr(address); err == nil {
		field = "spec.source.generated.ipAddresses"
	}
	return "store a certificate that names " + address + " with " + r.set() + ", or, for a generated Secret, add " + address + " to " + field +
		", import the change with bootwright context update --name " + r.contextName() + " --input-dir <dir> and run bootwright secret generate --name " + r.secret + r.contextFlag() + "; " + r.exit()
}

func (r servingRemedies) contextName() string {
	if r.context == "" {
		return "<context>"
	}
	return r.context
}

// boundedPEM refuses a chain that would make parsing unbounded work before any
// certificate is interpreted, with the reason it refuses.
func boundedPEM(data []byte) (string, bool) {
	blocks := 0
	for rest := data; len(rest) != 0; {
		block, remainder := pem.Decode(rest)
		if block == nil {
			break
		}
		blocks++
		if blocks > maxPEMBlocks {
			return "the serving certificate chain has too many blocks", false
		}
		rest = remainder
	}
	if blocks == 0 {
		return "the serving certificate material contains no PEM block", false
	}
	return "", true
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
