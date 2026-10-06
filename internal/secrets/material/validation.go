package material

import (
	"bytes"
	"crypto"
	"crypto/dsa"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rsa"
	"crypto/subtle"
	"crypto/x509"
	"encoding/pem"
	"strconv"
	"time"

	"github.com/crmarques/bootwright/internal/secrets"
	"golang.org/x/crypto/ssh"
)

// minimumServingRSABits is the smallest RSA modulus a TLS serving key may
// have. A CA bundle's roots are trusted as given.
const minimumServingRSABits = 2048

func validateCA(certificate, privateKey []byte, now time.Time) error {
	certificates, err := parseCertificates(certificate)
	if err != nil {
		return err
	}
	for _, cert := range certificates {
		if !validAt(cert, now) || len(cert.UnhandledCriticalExtensions) != 0 || !cert.BasicConstraintsValid || !cert.IsCA || cert.KeyUsage&x509.KeyUsageCertSign == 0 {
			return failure("input", "CA bundle must contain currently valid CA signing certificates", "")
		}
	}
	if privateKey == nil {
		return nil
	}
	key, err := parseCertificatePrivateKey(privateKey)
	if err != nil {
		return err
	}
	defer clearPrivateKey(key)
	if !matchingPublicKey(certificates[0].PublicKey, publicKey(key)) {
		return failure("input", "CA certificate and private key do not agree", "")
	}
	return nil
}

func validateTLS(certificate, privateKey []byte, now time.Time) error {
	certificates, err := parseCertificates(certificate)
	if err != nil {
		return err
	}
	leaf := certificates[0]
	if !validAt(leaf, now) || len(leaf.UnhandledCriticalExtensions) != 0 || leaf.IsCA || !serverAuth(leaf.ExtKeyUsage) || !serverKeyUsage(leaf.KeyUsage) {
		return failure("input", "TLS leaf certificate is not suitable for current server authentication", "")
	}
	if key, ok := leaf.PublicKey.(*rsa.PublicKey); ok && key.N.BitLen() < minimumServingRSABits {
		return failure("input", "TLS serving key is RSA-"+strconv.Itoa(key.N.BitLen())+"; a serving key needs at least 2048 RSA bits: issue the certificate with an RSA key of at least 2048 bits or a P-256 key", "")
	}
	for _, cert := range certificates[1:] {
		if !validAt(cert, now) || len(cert.UnhandledCriticalExtensions) != 0 || !cert.BasicConstraintsValid || !cert.IsCA || cert.KeyUsage&x509.KeyUsageCertSign == 0 {
			return failure("input", "TLS certificate chain contains an invalid CA certificate", "")
		}
	}
	for index := 0; index+1 < len(certificates); index++ {
		if err := certificates[index].CheckSignatureFrom(certificates[index+1]); err != nil {
			return failure("input", "TLS certificate chain signatures do not agree", "")
		}
	}
	key, err := parseCertificatePrivateKey(privateKey)
	if err != nil {
		return err
	}
	defer clearPrivateKey(key)
	if !matchingPublicKey(leaf.PublicKey, publicKey(key)) {
		return failure("input", "TLS certificate and private key do not agree", "")
	}
	return nil
}

func parseCertificates(data []byte) ([]*x509.Certificate, error) {
	remainder := trimPEMWhitespace(data)
	certificates := []*x509.Certificate{}
	for len(remainder) > 0 {
		block, rest, ok := strictPEMBlock(remainder)
		if !ok {
			return nil, failure("input", "certificate material must contain only complete PEM certificate blocks", "")
		}
		if block == nil || block.Type != "CERTIFICATE" || len(block.Headers) != 0 {
			return nil, failure("input", "certificate material contains an invalid PEM block", "")
		}
		parsed, err := x509.ParseCertificates(block.Bytes)
		if err != nil || len(parsed) != 1 {
			return nil, failure("input", "certificate material contains an invalid X.509 certificate", "")
		}
		certificates = append(certificates, parsed[0])
		remainder = trimPEMWhitespace(rest)
	}
	if len(certificates) == 0 {
		return nil, failure("input", "certificate material is empty", "")
	}
	return certificates, nil
}

func parseCertificatePrivateKey(data []byte) (crypto.PrivateKey, error) {
	block, err := exactPrivatePEM(data)
	if err != nil {
		return nil, err
	}
	defer clear(block.Bytes)
	var key any
	switch block.Type {
	case "PRIVATE KEY":
		key, err = x509.ParsePKCS8PrivateKey(block.Bytes)
	case "RSA PRIVATE KEY":
		key, err = x509.ParsePKCS1PrivateKey(block.Bytes)
	case "EC PRIVATE KEY":
		key, err = x509.ParseECPrivateKey(block.Bytes)
	default:
		return nil, failure("input", "certificate private key uses an unsupported format", "")
	}
	if err != nil || !validCertificatePrivateKey(key) {
		clearPrivateKey(key)
		return nil, failure("input", "certificate private key is invalid or unsupported", "")
	}
	return key, nil
}

func exactPrivatePEM(data []byte) (*pem.Block, error) {
	trimmed := trimPEMWhitespace(data)
	if len(trimmed) == 0 {
		return nil, failure("input", "private key must be exactly one unencrypted PEM block", "")
	}
	block, rest, ok := strictPEMBlock(trimmed)
	if !ok || block == nil || len(block.Headers) != 0 || len(trimPEMWhitespace(rest)) != 0 {
		return nil, failure("input", "private key must be exactly one unencrypted PEM block", "")
	}
	return block, nil
}

// strictPEMBlock decodes only the first complete block. encoding/pem.Decode is
// intentionally permissive and can skip malformed or nested BEGIN records;
// secret material must account for every non-whitespace input byte instead.
func strictPEMBlock(data []byte) (*pem.Block, []byte, bool) {
	data = bytes.TrimLeft(data, " \t\r\n\v\f")
	const beginPrefix = "-----BEGIN "
	const lineSuffix = "-----"
	if !bytes.HasPrefix(data, []byte(beginPrefix)) {
		return nil, data, false
	}
	beginEnd := bytes.IndexByte(data, '\n')
	if beginEnd < 0 {
		return nil, data, false
	}
	beginLine := bytes.TrimSuffix(data[:beginEnd], []byte{'\r'})
	if !bytes.HasSuffix(beginLine, []byte(lineSuffix)) {
		return nil, data, false
	}
	typeName := beginLine[len(beginPrefix) : len(beginLine)-len(lineSuffix)]
	if len(typeName) == 0 || bytes.ContainsAny(typeName, "\r\n") {
		return nil, data, false
	}
	endMarker := append([]byte("\n-----END "), typeName...)
	endMarker = append(endMarker, []byte(lineSuffix)...)
	endStart := bytes.Index(data, endMarker)
	if endStart < 0 || bytes.Contains(data[beginEnd:endStart], []byte("\n-----BEGIN ")) {
		return nil, data, false
	}
	afterMarker := endStart + len(endMarker)
	lineEnd := bytes.IndexByte(data[afterMarker:], '\n')
	consumed := len(data)
	if lineEnd >= 0 {
		consumed = afterMarker + lineEnd + 1
	}
	if len(bytes.Trim(data[afterMarker:consumed], " \t\r\n")) != 0 {
		return nil, data, false
	}
	blockBytes := data[:consumed]
	block, ignored := pem.Decode(blockBytes)
	if block == nil || len(trimPEMWhitespace(ignored)) != 0 || block.Type != string(typeName) {
		return nil, data, false
	}
	return block, data[consumed:], true
}

func trimPEMWhitespace(data []byte) []byte {
	return bytes.Trim(data, " \t\r\n\v\f")
}

func validCertificatePrivateKey(key any) bool {
	switch value := key.(type) {
	case *rsa.PrivateKey:
		return value.N != nil && value.Validate() == nil
	case *ecdsa.PrivateKey:
		return supportedCurve(value.Curve) && value.D != nil && value.D.Sign() > 0
	case ed25519.PrivateKey:
		return len(value) == ed25519.PrivateKeySize
	case *ed25519.PrivateKey:
		return value != nil && len(*value) == ed25519.PrivateKeySize
	}
	return false
}

func publicKey(key any) crypto.PublicKey {
	switch value := key.(type) {
	case *rsa.PrivateKey:
		return &value.PublicKey
	case *ecdsa.PrivateKey:
		return &value.PublicKey
	case ed25519.PrivateKey:
		return value.Public()
	case *ed25519.PrivateKey:
		return (*value).Public()
	}
	return nil
}

func matchingPublicKey(left, right crypto.PublicKey) bool {
	if left == nil || right == nil {
		return false
	}
	leftDER, leftErr := x509.MarshalPKIXPublicKey(left)
	rightDER, rightErr := x509.MarshalPKIXPublicKey(right)
	if leftErr != nil || rightErr != nil || len(leftDER) != len(rightDER) {
		return false
	}
	return subtle.ConstantTimeCompare(leftDER, rightDER) == 1
}

func validAt(certificate *x509.Certificate, now time.Time) bool {
	return !now.Before(certificate.NotBefore) && !now.After(certificate.NotAfter)
}

func serverAuth(usages []x509.ExtKeyUsage) bool {
	if len(usages) == 0 {
		return true
	}
	for _, usage := range usages {
		if usage == x509.ExtKeyUsageAny || usage == x509.ExtKeyUsageServerAuth {
			return true
		}
	}
	return false
}

func serverKeyUsage(usage x509.KeyUsage) bool {
	return usage == 0 || usage&(x509.KeyUsageDigitalSignature|x509.KeyUsageKeyEncipherment|x509.KeyUsageKeyAgreement) != 0
}

func validateSSH(privateKey, publicKey []byte) error {
	private, derived, err := parseSSHPrivate(privateKey)
	if err != nil {
		return err
	}
	defer clearPrivateKey(private)
	quoted := bytes.TrimRight(publicKey, "\n")
	publicKey = bytes.Trim(publicKey, " \t\r\n")
	if len(publicKey) == 0 || bytes.ContainsAny(publicKey, "\r\n") {
		return failure("input", "SSH public key must contain exactly one key without options", "")
	}
	if !secrets.SSHComment(string(quoted)) {
		return failure("input", "SSH public key line must not contain a control character (a tab, or the carriage return of a CRLF line ending, included), a line or paragraph separator, a double quote or a backslash; separate its fields with spaces and end it with a bare line feed", "")
	}
	provided, _, options, rest, err := ssh.ParseAuthorizedKey(publicKey)
	if err != nil || len(options) != 0 || len(bytes.TrimSpace(rest)) != 0 {
		return failure("input", "SSH public key must contain exactly one key without options", "")
	}
	if !allowedSSHPublic(provided) || !bytes.Equal(derived.Marshal(), provided.Marshal()) {
		return failure("input", "SSH public key is unsupported or does not agree with its private key", "")
	}
	return nil
}

func deriveSSHPublic(privateKey []byte) ([]byte, error) {
	private, public, err := parseSSHPrivate(privateKey)
	if err != nil {
		return nil, err
	}
	defer clearPrivateKey(private)
	return ssh.MarshalAuthorizedKey(public), nil
}

func parseSSHPrivate(data []byte) (crypto.PrivateKey, ssh.PublicKey, error) {
	block, err := exactPrivatePEM(data)
	if err != nil {
		return nil, nil, err
	}
	defer clear(block.Bytes)
	encoded := pem.EncodeToMemory(block)
	defer clear(encoded)
	key, err := ssh.ParseRawPrivateKey(encoded)
	if err != nil {
		return nil, nil, failure("input", "SSH private key must be one supported unencrypted key", "")
	}
	if !validSSHPrivate(key) {
		clearPrivateKey(key)
		return nil, nil, failure("input", "SSH private key algorithm or strength is unsupported", "")
	}
	public, err := ssh.NewPublicKey(publicKey(key))
	if err != nil || !allowedSSHPublic(public) {
		clearPrivateKey(key)
		return nil, nil, failure("input", "SSH private key could not produce a supported public key", "")
	}
	return key, public, nil
}

func validSSHPrivate(key any) bool {
	switch value := key.(type) {
	case *rsa.PrivateKey:
		return value.N != nil && value.N.BitLen() >= 3072 && value.Validate() == nil
	case *ecdsa.PrivateKey:
		return supportedCurve(value.Curve) && value.D != nil && value.D.Sign() > 0
	case ed25519.PrivateKey:
		return len(value) == ed25519.PrivateKeySize
	case *ed25519.PrivateKey:
		return value != nil && len(*value) == ed25519.PrivateKeySize
	case *dsa.PrivateKey:
		return false
	}
	return false
}

func allowedSSHPublic(key ssh.PublicKey) bool {
	if key == nil {
		return false
	}
	switch key.Type() {
	case ssh.KeyAlgoRSA, ssh.KeyAlgoECDSA256, ssh.KeyAlgoECDSA384, ssh.KeyAlgoECDSA521, ssh.KeyAlgoED25519:
		return true
	}
	return false
}

func supportedCurve(curve elliptic.Curve) bool {
	return curve == elliptic.P256() || curve == elliptic.P384() || curve == elliptic.P521()
}
