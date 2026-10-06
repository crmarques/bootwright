package material

import (
	"bytes"
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	cryptorand "crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"encoding/base64"
	"encoding/pem"
	"io"
	"math/big"
	"net"
	"net/netip"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/crmarques/bootwright/internal/secrets"
	"golang.org/x/crypto/ssh"
)

type standardCryptography struct{}

func (standardCryptography) GenerateECDSA(curve elliptic.Curve) (*ecdsa.PrivateKey, error) {
	return ecdsa.GenerateKey(curve, cryptorand.Reader)
}

func (standardCryptography) GenerateRSA(bits int) (*rsa.PrivateKey, error) {
	return rsa.GenerateKey(cryptorand.Reader, bits)
}

func (standardCryptography) CreateCertificate(template, parent *x509.Certificate, publicKey crypto.PublicKey, privateKey crypto.Signer) ([]byte, error) {
	return x509.CreateCertificate(cryptorand.Reader, template, parent, publicKey, privateKey)
}

func (s *Service) Generate(ctx context.Context, declaration secrets.Declaration) (secrets.Material, error) {
	if err := ctx.Err(); err != nil {
		return secrets.Material{}, err
	}
	if declaration.Source != "generated" {
		return secrets.Material{}, failure("source", "secret declaration does not use generated material", "")
	}
	if err := validGenerationShape(declaration); err != nil {
		return secrets.Material{}, err
	}
	parts := map[secrets.Part][]byte{}
	defer clearParts(parts)

	switch declaration.Type {
	case "token":
		count := declaration.Generation.Bytes
		if count == 0 {
			count = 32
		}
		if count < 16 || count > 1024 {
			return secrets.Material{}, failure("source", "generated token byte count must be between 16 and 1024", "")
		}
		value, err := s.randomURLValue(ctx, count)
		if err != nil {
			return secrets.Material{}, err
		}
		parts[secrets.ValuePart] = value
	case "usernamePassword":
		name, err := generatedUsername(declaration.Generation.Username)
		if err != nil {
			return secrets.Material{}, err
		}
		parts[secrets.UsernamePart] = name
		value, err := s.randomURLValue(ctx, 32)
		if err != nil {
			return secrets.Material{}, err
		}
		parts[secrets.PasswordPart] = value
	case "caBundle", "tlsCertificate":
		certificate, privateKey, err := s.generateCertificate(ctx, declaration)
		if err != nil {
			return secrets.Material{}, err
		}
		parts[secrets.CertificatePart] = certificate
		parts[secrets.PrivateKeyPart] = privateKey
	case "sshKeyPair":
		privateKey, publicKey, err := s.generateSSH(ctx, declaration.Generation.KeyType, declaration.Generation.Comment)
		if err != nil {
			return secrets.Material{}, err
		}
		parts[secrets.PrivateKeyPart] = privateKey
		parts[secrets.PublicKeyPart] = publicKey
	case "opaque", "dockerConfigJson":
		return secrets.Material{}, failure("source", "declared secret type does not permit generation", "")
	default:
		return secrets.Material{}, failure("declaration", "secret declaration has an unsupported type", "")
	}
	return s.finish(ctx, declaration, parts)
}

func validGenerationShape(declaration secrets.Declaration) error {
	generation := declaration.Generation
	wrong := false
	switch declaration.Type {
	case "token":
		wrong = generation.Username != "" || generation.CommonName != "" || len(generation.DNSNames) != 0 ||
			len(generation.IPAddresses) != 0 || generation.ValidityDays != 0 || generation.KeyType != "" || generation.Comment != ""
	case "usernamePassword":
		wrong = generation.CommonName != "" || len(generation.DNSNames) != 0 || len(generation.IPAddresses) != 0 ||
			generation.ValidityDays != 0 || generation.KeyType != "" || generation.Comment != "" || generation.Bytes != 0
	case "caBundle", "tlsCertificate":
		wrong = generation.Username != "" || generation.KeyType != "" || generation.Comment != "" || generation.Bytes != 0
	case "sshKeyPair":
		wrong = generation.Username != "" || generation.CommonName != "" || len(generation.DNSNames) != 0 ||
			len(generation.IPAddresses) != 0 || generation.ValidityDays != 0 || generation.Bytes != 0
	}
	if wrong {
		return failure("source", "generation parameters are not accepted by the declared secret type", "")
	}
	return nil
}

func validateGeneratedToken(declaration secrets.Declaration, value []byte) error {
	if err := validGenerationShape(declaration); err != nil {
		return err
	}
	count := declaration.Generation.Bytes
	if count == 0 {
		count = 32
	}
	if count < 16 || count > 1024 {
		return failure("source", "generated token byte count must be between 16 and 1024", "")
	}
	return validateGeneratedURLValue(value, count)
}

func validateGeneratedUsernamePassword(declaration secrets.Declaration, name, password []byte) error {
	if err := validGenerationShape(declaration); err != nil {
		return err
	}
	expected, err := generatedUsername(declaration.Generation.Username)
	if err != nil {
		return err
	}
	defer clear(expected)
	if !bytes.Equal(name, expected) {
		return failure("input", "generated secret material does not conform to its declaration", "")
	}
	return validateGeneratedURLValue(password, 32)
}

func generatedUsername(configured string) ([]byte, error) {
	if configured == "" {
		configured = "admin"
	}
	if len(configured) > secrets.MaxPartBytes {
		return nil, failure("store.limit", "generated username exceeds the part byte limit", "")
	}
	value := []byte(configured)
	if err := username(value); err != nil {
		clear(value)
		return nil, failure("source", "generated username must be one line without whitespace or colon", "")
	}
	return value, nil
}

func validateGeneratedURLValue(value []byte, count int) error {
	if len(value) != base64.RawURLEncoding.EncodedLen(count) {
		return failure("input", "generated secret material does not conform to its declaration", "")
	}
	decoded := make([]byte, count)
	defer clear(decoded)
	n, err := base64.RawURLEncoding.Strict().Decode(decoded, value)
	if err != nil || n != count {
		return failure("input", "generated secret material does not conform to its declaration", "")
	}
	canonical := make([]byte, len(value))
	defer clear(canonical)
	base64.RawURLEncoding.Encode(canonical, decoded)
	if !bytes.Equal(canonical, value) {
		return failure("input", "generated secret material does not conform to its declaration", "")
	}
	return nil
}

func validateGeneratedSSH(declaration secrets.Declaration, privateKey, publicKey []byte) error {
	if err := validGenerationShape(declaration); err != nil {
		return err
	}
	keyType := declaration.Generation.KeyType
	if keyType == "" {
		keyType = "ed25519"
	}
	switch keyType {
	case "ed25519", "rsa", "ecdsa-p256", "ecdsa-p384", "ecdsa-p521":
	default:
		return failure("source", "SSH key type is unsupported", "")
	}
	comment := declaration.Generation.Comment
	if err := validateGeneratedSSHComment(comment); err != nil {
		return err
	}
	if err := validateSSH(privateKey, publicKey); err != nil {
		return err
	}
	private, derived, err := parseSSHPrivate(privateKey)
	if err != nil {
		return err
	}
	defer clearPrivateKey(private)
	actual, exact := generatedSSHKeyType(private)
	if !exact || actual != keyType {
		return failure("input", "generated secret material does not conform to its declaration", "")
	}
	expectedPublic, err := marshalGeneratedSSHPublic(derived, comment)
	if err != nil {
		return err
	}
	defer clear(expectedPublic)
	if !bytes.Equal(publicKey, expectedPublic) {
		return failure("input", "generated secret material does not conform to its declaration", "")
	}
	return nil
}

func generatedSSHKeyType(key crypto.PrivateKey) (string, bool) {
	switch value := key.(type) {
	case ed25519.PrivateKey:
		return "ed25519", len(value) == ed25519.PrivateKeySize
	case *ed25519.PrivateKey:
		return "ed25519", value != nil && len(*value) == ed25519.PrivateKeySize
	case *rsa.PrivateKey:
		return "rsa", value != nil && value.N != nil && value.N.BitLen() == 3072
	case *ecdsa.PrivateKey:
		if value == nil || value.Curve == nil || value.Curve.Params() == nil {
			return "", false
		}
		switch value.Curve.Params().Name {
		case elliptic.P256().Params().Name:
			return "ecdsa-p256", true
		case elliptic.P384().Params().Name:
			return "ecdsa-p384", true
		case elliptic.P521().Params().Name:
			return "ecdsa-p521", true
		}
	}
	return "", false
}

func (s *Service) validateGeneratedCertificate(declaration secrets.Declaration, certificate, privateKey []byte) error {
	if err := validGenerationShape(declaration); err != nil {
		return err
	}
	configuration := declaration.Generation
	days := configuration.ValidityDays
	if days == 0 {
		days = 3650
	}
	if configuration.CommonName == "" || !utf8.ValidString(configuration.CommonName) || strings.ContainsRune(configuration.CommonName, 0) || days < 1 || days > 36500 {
		return failure("source", "generated certificate parameters are invalid", "")
	}
	for _, name := range configuration.DNSNames {
		if !validDNSName(name) {
			return failure("source", "generated certificate contains an invalid DNS name", "")
		}
	}
	ipAddresses := make([]net.IP, 0, len(configuration.IPAddresses))
	for _, text := range configuration.IPAddresses {
		address, err := netip.ParseAddr(text)
		if err != nil || address.Zone() != "" {
			return failure("source", "generated certificate contains an invalid IP address", "")
		}
		ipAddresses = append(ipAddresses, net.IP(slices.Clone(address.AsSlice())))
	}

	certificates, err := parseCertificates(certificate)
	if err != nil {
		return err
	}
	if len(certificates) != 1 {
		return failure("input", "generated secret material does not conform to its declaration", "")
	}
	cert := certificates[0]
	block, err := exactPrivatePEM(privateKey)
	if err != nil {
		return err
	}
	defer clear(block.Bytes)
	if block.Type != "PRIVATE KEY" {
		return failure("input", "generated secret material does not conform to its declaration", "")
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return failure("input", "generated secret material does not conform to its declaration", "")
	}
	defer clearPrivateKey(parsed)
	key, ok := parsed.(*ecdsa.PrivateKey)
	if !ok || !validGeneratedP256PrivateKey(key) {
		return failure("input", "generated secret material does not conform to its declaration", "")
	}
	public, ok := cert.PublicKey.(*ecdsa.PublicKey)
	if !ok || !isP256(public.Curve) || public.X == nil || public.Y == nil || !public.Curve.IsOnCurve(public.X, public.Y) ||
		!matchingPublicKey(cert.PublicKey, &key.PublicKey) {
		return failure("input", "generated secret material does not conform to its declaration", "")
	}

	expectedSubject, err := asn1.Marshal(pkix.Name{CommonName: configuration.CommonName}.ToRDNSequence())
	if err != nil {
		return failure("source", "generated certificate parameters are invalid", "")
	}
	now := s.now()
	if cert.Version != 3 || cert.PublicKeyAlgorithm != x509.ECDSA || cert.SignatureAlgorithm != x509.ECDSAWithSHA256 ||
		cert.SerialNumber == nil || cert.SerialNumber.Sign() <= 0 || cert.SerialNumber.BitLen() > 128 || len(cert.UnhandledCriticalExtensions) != 0 ||
		!bytes.Equal(cert.RawSubject, expectedSubject) || !bytes.Equal(cert.RawIssuer, expectedSubject) ||
		!validAt(cert, now) || cert.NotBefore.Nanosecond() != 0 || cert.NotAfter.Nanosecond() != 0 || !generatedValidity(cert, days) ||
		cert.CheckSignature(cert.SignatureAlgorithm, cert.RawTBSCertificate, cert.Signature) != nil ||
		!exactGeneratedSANs(cert, configuration.DNSNames, ipAddresses) || !exactGeneratedCertificateUsage(cert, declaration.Type) {
		return failure("input", "generated secret material does not conform to its declaration", "")
	}
	return nil
}

// certificateBackdate is how long before its generation second a generated
// certificate starts, so a verifier whose clock trails still accepts it.
const certificateBackdate = 24 * time.Hour

// generatedValidity accepts the validity a generated certificate has: it ends
// days after its generation second, and starts a day before it, or, when an
// earlier build generated it, at that second.
func generatedValidity(certificate *x509.Certificate, days int) bool {
	return certificate.NotAfter.Equal(certificate.NotBefore.AddDate(0, 0, days)) ||
		certificate.NotAfter.Equal(certificate.NotBefore.Add(certificateBackdate).AddDate(0, 0, days))
}

func validGeneratedP256PrivateKey(key *ecdsa.PrivateKey) bool {
	if key == nil || !isP256(key.Curve) || key.D == nil || key.D.Sign() <= 0 || key.D.Cmp(key.Curve.Params().N) >= 0 ||
		key.X == nil || key.Y == nil || !key.Curve.IsOnCurve(key.X, key.Y) {
		return false
	}
	scalar := key.D.Bytes()
	defer clear(scalar)
	x, y := key.Curve.ScalarBaseMult(scalar)
	return x.Cmp(key.X) == 0 && y.Cmp(key.Y) == 0
}

func isP256(curve elliptic.Curve) bool {
	return curve != nil && curve.Params() != nil && curve.Params().Name == elliptic.P256().Params().Name
}

var subjectAlternativeNameOID = asn1.ObjectIdentifier{2, 5, 29, 17}

func exactGeneratedSANs(certificate *x509.Certificate, dnsNames []string, ipAddresses []net.IP) bool {
	if !slices.Equal(certificate.DNSNames, dnsNames) || len(certificate.IPAddresses) != len(ipAddresses) ||
		len(certificate.EmailAddresses) != 0 || len(certificate.URIs) != 0 {
		return false
	}
	for index := range ipAddresses {
		if !certificate.IPAddresses[index].Equal(ipAddresses[index]) {
			return false
		}
	}
	var extensionValues [][]byte
	for _, extension := range certificate.Extensions {
		if extension.Id.Equal(subjectAlternativeNameOID) {
			if extension.Critical {
				return false
			}
			extensionValues = append(extensionValues, extension.Value)
		}
	}
	if len(dnsNames) == 0 && len(ipAddresses) == 0 {
		return len(extensionValues) == 0
	}
	if len(extensionValues) != 1 {
		return false
	}
	values := make([]asn1.RawValue, 0, len(dnsNames)+len(ipAddresses))
	for _, name := range dnsNames {
		values = append(values, asn1.RawValue{Class: asn1.ClassContextSpecific, Tag: 2, Bytes: []byte(name)})
	}
	for _, address := range ipAddresses {
		value := address
		if ipv4 := address.To4(); ipv4 != nil {
			value = ipv4
		}
		values = append(values, asn1.RawValue{Class: asn1.ClassContextSpecific, Tag: 7, Bytes: value})
	}
	expected, err := asn1.Marshal(values)
	return err == nil && bytes.Equal(extensionValues[0], expected)
}

func exactGeneratedCertificateUsage(certificate *x509.Certificate, secretType string) bool {
	if !certificate.BasicConstraintsValid || certificate.MaxPathLenZero || certificate.MaxPathLen > 0 || len(certificate.UnknownExtKeyUsage) != 0 {
		return false
	}
	switch secretType {
	case "caBundle":
		return certificate.IsCA && certificate.KeyUsage == x509.KeyUsageCertSign|x509.KeyUsageCRLSign|x509.KeyUsageDigitalSignature && len(certificate.ExtKeyUsage) == 0
	case "tlsCertificate":
		return !certificate.IsCA && certificate.KeyUsage == x509.KeyUsageDigitalSignature && slices.Equal(certificate.ExtKeyUsage, []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth})
	}
	return false
}

func (s *Service) randomURLValue(ctx context.Context, count int) ([]byte, error) {
	raw, err := s.randomBytes(ctx, count)
	if err != nil {
		return nil, err
	}
	defer clear(raw)
	encoded := make([]byte, base64.RawURLEncoding.EncodedLen(len(raw)))
	base64.RawURLEncoding.Encode(encoded, raw)
	return encoded, nil
}

func (s *Service) randomBytes(ctx context.Context, count int) ([]byte, error) {
	if s == nil || s.random == nil || count < 0 {
		return nil, failure("store.crypto", "secret generation randomness is unavailable", "")
	}
	data := make([]byte, count)
	reader := contextualReader{ctx: ctx, reader: s.random}
	if _, err := io.ReadFull(reader, data); err != nil {
		clear(data)
		if canceled := ctx.Err(); canceled != nil {
			return nil, canceled
		}
		return nil, failure("store.crypto", "secret generation randomness failed", "")
	}
	return data, nil
}

func (s *Service) generateCertificate(ctx context.Context, declaration secrets.Declaration) ([]byte, []byte, error) {
	configuration := declaration.Generation
	days := configuration.ValidityDays
	if days == 0 {
		days = 3650
	}
	if configuration.CommonName == "" || !utf8.ValidString(configuration.CommonName) || strings.ContainsRune(configuration.CommonName, 0) || days < 1 || days > 36500 {
		return nil, nil, failure("source", "generated certificate parameters are invalid", "")
	}
	dnsNames := slices.Clone(configuration.DNSNames)
	for _, name := range dnsNames {
		if !validDNSName(name) {
			return nil, nil, failure("source", "generated certificate contains an invalid DNS name", "")
		}
	}
	ipAddresses := make([]net.IP, 0, len(configuration.IPAddresses))
	for _, text := range configuration.IPAddresses {
		address, err := netip.ParseAddr(text)
		if err != nil || address.Zone() != "" {
			return nil, nil, failure("source", "generated certificate contains an invalid IP address", "")
		}
		ipAddresses = append(ipAddresses, net.IP(slices.Clone(address.AsSlice())))
	}
	if s == nil || s.random == nil || s.clock == nil || s.cryptography == nil {
		return nil, nil, failure("store.crypto", "certificate generation dependencies are unavailable", "")
	}
	now := s.now()
	notBefore := now.Add(-certificateBackdate)
	if notBefore.Year() < 1950 || now.Year() > 9899 {
		return nil, nil, failure("store.crypto", "certificate generation clock is outside the supported range", "")
	}
	notAfter := now.AddDate(0, 0, days)
	if !notAfter.After(now) || notAfter.Year() > 9999 {
		return nil, nil, failure("source", "generated certificate validity is outside the supported range", "")
	}
	key, err := s.generateECDSA(ctx, elliptic.P256(), "certificate private key generation failed")
	if err != nil {
		return nil, nil, err
	}
	defer clearPrivateKey(key)
	serial, err := s.randomSerial(ctx)
	if err != nil {
		return nil, nil, err
	}
	template := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: configuration.CommonName},
		NotBefore:    notBefore,
		NotAfter:     notAfter,
		DNSNames:     dnsNames,
		IPAddresses:  ipAddresses,
	}
	if declaration.Type == "caBundle" {
		template.BasicConstraintsValid = true
		template.IsCA = true
		template.KeyUsage = x509.KeyUsageCertSign | x509.KeyUsageCRLSign | x509.KeyUsageDigitalSignature
	} else {
		template.BasicConstraintsValid = true
		template.KeyUsage = x509.KeyUsageDigitalSignature
		template.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}
	}
	certificateDER, err := s.createCertificate(ctx, template, template, &key.PublicKey, key)
	if err != nil {
		return nil, nil, err
	}
	certificate := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificateDER})
	clear(certificateDER)
	privateDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		clear(certificate)
		return nil, nil, failure("store.crypto", "certificate private key encoding failed", "")
	}
	privateKey := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privateDER})
	clear(privateDER)
	if certificate == nil || privateKey == nil {
		clear(certificate)
		clear(privateKey)
		return nil, nil, failure("store.crypto", "certificate material encoding failed", "")
	}
	return certificate, privateKey, nil
}

func (s *Service) randomSerial(ctx context.Context) (*big.Int, error) {
	for range 16 {
		data, err := s.randomBytes(ctx, 16)
		if err != nil {
			return nil, err
		}
		serial := new(big.Int).SetBytes(data)
		clear(data)
		if serial.Sign() > 0 {
			return serial, nil
		}
	}
	return nil, failure("store.crypto", "certificate serial generation produced no valid value", "")
}

func (s *Service) generateSSH(ctx context.Context, keyType, comment string) ([]byte, []byte, error) {
	if keyType == "" {
		keyType = "ed25519"
	}
	if err := validateGeneratedSSHComment(comment); err != nil {
		return nil, nil, err
	}
	if s == nil || s.random == nil || s.cryptography == nil {
		return nil, nil, failure("store.crypto", "SSH key generation randomness is unavailable", "")
	}
	random := contextualReader{ctx: ctx, reader: s.random}
	var key crypto.PrivateKey
	var err error
	switch keyType {
	case "ed25519":
		_, key, err = ed25519.GenerateKey(random)
	case "rsa":
		key, err = s.generateRSA(ctx, 3072, "SSH private key generation failed")
	case "ecdsa-p256":
		key, err = s.generateECDSA(ctx, elliptic.P256(), "SSH private key generation failed")
	case "ecdsa-p384":
		key, err = s.generateECDSA(ctx, elliptic.P384(), "SSH private key generation failed")
	case "ecdsa-p521":
		key, err = s.generateECDSA(ctx, elliptic.P521(), "SSH private key generation failed")
	default:
		return nil, nil, failure("source", "SSH key type is unsupported", "")
	}
	if canceled := ctx.Err(); canceled != nil {
		clearPrivateKey(key)
		return nil, nil, canceled
	}
	if err != nil {
		clearPrivateKey(key)
		return nil, nil, failure("store.crypto", "SSH private key generation failed", "")
	}
	defer clearPrivateKey(key)
	privateDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, nil, failure("store.crypto", "SSH private key encoding failed", "")
	}
	privateKey := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privateDER})
	clear(privateDER)
	public, err := ssh.NewPublicKey(publicKey(key))
	if err != nil {
		clear(privateKey)
		return nil, nil, failure("store.crypto", "SSH public key encoding failed", "")
	}
	publicKey, err := marshalGeneratedSSHPublic(public, comment)
	if err != nil {
		clear(privateKey)
		return nil, nil, err
	}
	if privateKey == nil || len(publicKey) == 0 {
		clear(privateKey)
		clear(publicKey)
		return nil, nil, failure("store.crypto", "SSH material encoding failed", "")
	}
	return privateKey, publicKey, nil
}

func validateGeneratedSSHComment(comment string) error {
	if len(comment) > secrets.MaxPartBytes {
		return failure("store.limit", "generated SSH public key comment exceeds the part byte limit", "")
	}
	if strings.TrimSpace(comment) != comment || !secrets.SSHComment(comment) {
		return failure("source", "SSH key comment must be one line without surrounding whitespace, a control character, a line or paragraph separator, a double quote or a backslash", "")
	}
	return nil
}

func marshalGeneratedSSHPublic(public ssh.PublicKey, comment string) ([]byte, error) {
	if err := validateGeneratedSSHComment(comment); err != nil {
		return nil, err
	}
	publicKey := bytes.TrimSuffix(ssh.MarshalAuthorizedKey(public), []byte{'\n'})
	overhead := 1
	if comment != "" {
		overhead++
	}
	if len(publicKey) > secrets.MaxPartBytes-overhead || len(comment) > secrets.MaxPartBytes-overhead-len(publicKey) {
		clear(publicKey)
		return nil, failure("store.limit", "generated SSH public key exceeds the part byte limit", "")
	}
	if comment != "" {
		publicKey = append(publicKey, ' ')
		publicKey = append(publicKey, comment...)
	}
	return append(publicKey, '\n'), nil
}

func validDNSName(value string) bool {
	if len(value) == 0 || len(value) > 253 || strings.HasSuffix(value, ".") {
		return false
	}
	for _, label := range strings.Split(value, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, character := range label {
			if !(character >= 'a' && character <= 'z' || character >= '0' && character <= '9' || character == '-') {
				return false
			}
		}
	}
	return true
}

func clearPrivateKey(key any) {
	switch value := key.(type) {
	case ed25519.PrivateKey:
		clear(value)
	case *ed25519.PrivateKey:
		if value != nil {
			clear(*value)
			*value = nil
		}
	case *ecdsa.PrivateKey:
		if value != nil {
			clearBigInt(value.D)
			value.D = nil
		}
	case *rsa.PrivateKey:
		if value != nil {
			clearBigInt(value.D)
			value.D = nil
			for _, prime := range value.Primes {
				clearBigInt(prime)
			}
			clear(value.Primes)
			value.Primes = nil
			clearBigInt(value.Precomputed.Dp)
			clearBigInt(value.Precomputed.Dq)
			clearBigInt(value.Precomputed.Qinv)
			for index := range value.Precomputed.CRTValues {
				clearBigInt(value.Precomputed.CRTValues[index].Exp)
				clearBigInt(value.Precomputed.CRTValues[index].Coeff)
				clearBigInt(value.Precomputed.CRTValues[index].R)
			}
			clear(value.Precomputed.CRTValues)
			value.Precomputed = rsa.PrecomputedValues{}
		}
	}
}

func clearBigInt(value *big.Int) {
	if value == nil {
		return
	}
	words := value.Bits()
	clear(words[:cap(words)])
	value.SetInt64(0)
}

func (s *Service) generateECDSA(ctx context.Context, curve elliptic.Curve, message string) (*ecdsa.PrivateKey, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s == nil || s.cryptography == nil {
		return nil, failure("store.crypto", message, "")
	}
	key, err := s.cryptography.GenerateECDSA(curve)
	if canceled := ctx.Err(); canceled != nil {
		clearPrivateKey(key)
		return nil, canceled
	}
	if err != nil || key == nil {
		clearPrivateKey(key)
		return nil, failure("store.crypto", message, "")
	}
	return key, nil
}

func (s *Service) generateRSA(ctx context.Context, bits int, message string) (*rsa.PrivateKey, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s == nil || s.cryptography == nil {
		return nil, failure("store.crypto", message, "")
	}
	key, err := s.cryptography.GenerateRSA(bits)
	if canceled := ctx.Err(); canceled != nil {
		clearPrivateKey(key)
		return nil, canceled
	}
	if err != nil || key == nil {
		clearPrivateKey(key)
		return nil, failure("store.crypto", message, "")
	}
	return key, nil
}

func (s *Service) createCertificate(ctx context.Context, template, parent *x509.Certificate, publicKey crypto.PublicKey, privateKey crypto.Signer) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s == nil || s.cryptography == nil {
		return nil, failure("store.crypto", "certificate signing failed", "")
	}
	certificate, err := s.cryptography.CreateCertificate(template, parent, publicKey, privateKey)
	if canceled := ctx.Err(); canceled != nil {
		clear(certificate)
		return nil, canceled
	}
	if err != nil || len(certificate) == 0 {
		clear(certificate)
		return nil, failure("store.crypto", "certificate signing failed", "")
	}
	return certificate, nil
}

type contextualReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r contextualReader) Read(destination []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := r.reader.Read(destination)
	if canceled := r.ctx.Err(); canceled != nil {
		return n, canceled
	}
	return n, err
}

var _ io.Reader = contextualReader{}
