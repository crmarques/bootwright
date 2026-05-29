package secret

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/runtime/context"
	"github.com/crmarques/bootwright/internal/runtime/root/callerio"
)

func ParseBMCCredentials(data []byte) (username, password string, err error) {
	text := strings.TrimRight(string(data), "\r\n")
	if text == "" {
		return "", "", errors.New("credentials file is empty; expected one line in the form username:password")
	}
	if strings.Contains(text, "\n") {
		return "", "", errors.New("credentials file must contain a single username:password line")
	}
	idx := strings.IndexByte(text, ':')
	if idx <= 0 || idx == len(text)-1 {
		return "", "", errors.New("credentials file must contain a single username:password line")
	}
	return text[:idx], text[idx+1:], nil
}

type UserPassword struct {
	Username string
	Password string
}

func ReadUserPasswordFile(path, kind string) (UserPassword, error) {
	if path == "" {
		return UserPassword{}, fmt.Errorf("%s path is empty", kind)
	}
	data, err := ReadFile(path)
	if err != nil {
		return UserPassword{}, fmt.Errorf("read %s at %s: %w", kind, path, err)
	}
	username, password, err := ParseBMCCredentials(data)
	if err != nil {
		return UserPassword{}, fmt.Errorf("%s at %s: %w", kind, path, err)
	}
	return UserPassword{Username: username, Password: password}, nil
}

func ReadFile(path string) ([]byte, error) {
	if pathUsesCaller(path) {
		if data, ok, err := callerio.ReadFile(path); ok {
			return data, err
		}
	}
	return os.ReadFile(path)
}

func Stat(path string) (os.FileInfo, error) {
	if pathUsesCaller(path) {
		if info, ok, err := callerio.Stat(path); ok {
			return info, err
		}
	}
	return os.Stat(path)
}

func pathUsesCaller(path string) bool {
	clean := filepath.Clean(path)
	root := filepath.Clean(contextstore.RootDir())
	rel, err := filepath.Rel(root, clean)
	if err != nil {
		return true
	}
	return rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator))
}

func ValidateBMCUsername(username string) error {
	if username == "" {
		return errors.New("username must not be empty")
	}
	if strings.ContainsAny(username, ":\r\n\t ") {
		return errors.New("username must not contain whitespace or ':'")
	}
	return nil
}

func GenerateBMCPassword() (string, error) {
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate BMC password: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func ValidatePullSecretJSON(data []byte) error {
	var root map[string]json.RawMessage
	if err := json.Unmarshal(data, &root); err != nil {
		return fmt.Errorf("pull secret must be valid JSON with top-level .auths object: %w", err)
	}
	authsRaw, ok := root["auths"]
	if !ok {
		return errors.New("pull secret must contain top-level .auths object")
	}
	var auths map[string]json.RawMessage
	if err := json.Unmarshal(authsRaw, &auths); err != nil {
		return errors.New("pull secret .auths must be a JSON object")
	}
	if auths == nil {
		return errors.New("pull secret .auths must be a JSON object")
	}
	return nil
}

func ParseCertificateBundlePEM(data []byte) ([]*x509.Certificate, error) {
	rest := bytes.TrimSpace(data)
	var certs []*x509.Certificate
	for len(rest) > 0 {
		block, remaining := pem.Decode(rest)
		if block == nil {
			return nil, errors.New("certificate bundle must contain PEM-encoded certificates only")
		}
		if block.Type != "CERTIFICATE" {
			return nil, fmt.Errorf("certificate bundle contains PEM block %q; expected CERTIFICATE", block.Type)
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("parse certificate: %w", err)
		}
		certs = append(certs, cert)
		rest = bytes.TrimSpace(remaining)
	}
	if len(certs) == 0 {
		return nil, errors.New("certificate bundle must contain at least one PEM certificate")
	}
	return certs, nil
}

func ValidateCABundlePEM(data []byte) error {
	_, err := ParseCertificateBundlePEM(data)
	return err
}

func ValidateTLSCertificateKey(certPEM, keyPEM []byte) ([]*x509.Certificate, error) {
	certs, err := ParseCertificateBundlePEM(certPEM)
	if err != nil {
		return nil, err
	}
	key, err := ParsePrivateKeyPEM(keyPEM)
	if err != nil {
		return nil, err
	}
	keyPublic, err := privateKeyPublicKey(key)
	if err != nil {
		return nil, err
	}
	if !publicKeysEqual(certs[0].PublicKey, keyPublic) {
		return nil, errors.New("certificate leaf public key does not match private key")
	}
	return certs, nil
}

func ParsePrivateKeyPEM(data []byte) (any, error) {
	rest := bytes.TrimSpace(data)
	for len(rest) > 0 {
		block, remaining := pem.Decode(rest)
		if block == nil {
			return nil, errors.New("private key must be PEM-encoded")
		}
		if block.Type == "ENCRYPTED PRIVATE KEY" || block.Headers["Proc-Type"] == "4,ENCRYPTED" {
			return nil, errors.New("private key must be unencrypted")
		}
		switch block.Type {
		case "RSA PRIVATE KEY":
			return x509.ParsePKCS1PrivateKey(block.Bytes)
		case "EC PRIVATE KEY":
			return x509.ParseECPrivateKey(block.Bytes)
		case "PRIVATE KEY":
			key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
			if err != nil {
				return nil, err
			}
			switch key.(type) {
			case *rsa.PrivateKey, *ecdsa.PrivateKey, ed25519.PrivateKey:
				return key, nil
			default:
				return nil, fmt.Errorf("unsupported private key type %T", key)
			}
		default:
			rest = bytes.TrimSpace(remaining)
		}
	}
	return nil, errors.New("private key PEM block not found")
}

func CertificateBundleCoversDNSName(certPEM []byte, name string) error {
	certs, err := ParseCertificateBundlePEM(certPEM)
	if err != nil {
		return err
	}
	return certs[0].VerifyHostname(name)
}

func privateKeyPublicKey(key any) (any, error) {
	switch k := key.(type) {
	case *rsa.PrivateKey:
		return &k.PublicKey, nil
	case *ecdsa.PrivateKey:
		return &k.PublicKey, nil
	case ed25519.PrivateKey:
		return k.Public(), nil
	default:
		return nil, fmt.Errorf("unsupported private key type %T", key)
	}
}

func publicKeysEqual(a, b any) bool {
	left, err := x509.MarshalPKIXPublicKey(a)
	if err != nil {
		return false
	}
	right, err := x509.MarshalPKIXPublicKey(b)
	if err != nil {
		return false
	}
	return bytes.Equal(left, right)
}

func SelfSignedCertificatePEM(source v1alpha1.SelfSignedCertificateSpec) (certPEM, keyPEM []byte, err error) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 4096)
	if err != nil {
		return nil, nil, fmt.Errorf("generate private key: %w", err)
	}
	serialLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serial, err := rand.Int(rand.Reader, serialLimit)
	if err != nil {
		return nil, nil, fmt.Errorf("generate certificate serial: %w", err)
	}
	dnsNames, ipAddresses := CertificateSANs(source)
	template := x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName: source.CommonName,
		},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().AddDate(0, 0, source.ValidityDays),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
		DNSNames:              dnsNames,
		IPAddresses:           ipAddresses,
	}
	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &privateKey.PublicKey, privateKey)
	if err != nil {
		return nil, nil, fmt.Errorf("create certificate: %w", err)
	}
	var certBuf bytes.Buffer
	if err := pem.Encode(&certBuf, &pem.Block{Type: "CERTIFICATE", Bytes: der}); err != nil {
		return nil, nil, fmt.Errorf("encode certificate: %w", err)
	}
	var keyBuf bytes.Buffer
	if err := pem.Encode(&keyBuf, &pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(privateKey)}); err != nil {
		return nil, nil, fmt.Errorf("encode private key: %w", err)
	}
	return certBuf.Bytes(), keyBuf.Bytes(), nil
}

func VerifySelfSignedCertificateMatchesRequest(certPath string, source v1alpha1.SelfSignedCertificateSpec) error {
	data, err := os.ReadFile(certPath)
	if err != nil {
		return fmt.Errorf("read certificate: %w", err)
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return fmt.Errorf("certificate is not PEM-encoded")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return fmt.Errorf("parse certificate: %w", err)
	}
	if cert.Subject.CommonName != source.CommonName {
		return fmt.Errorf("commonName drift: got %q, want %q", cert.Subject.CommonName, source.CommonName)
	}
	wantDNS, wantIP := CertificateSANs(source)
	gotDNS := NormalizeStringSet(cert.DNSNames)
	expectedDNS := NormalizeStringSet(wantDNS)
	if !reflect.DeepEqual(gotDNS, expectedDNS) {
		return fmt.Errorf("dnsNames drift: got %v, want %v", gotDNS, expectedDNS)
	}
	gotIP := NormalizeIPSet(cert.IPAddresses)
	expectedIP := NormalizeIPSet(wantIP)
	if !reflect.DeepEqual(gotIP, expectedIP) {
		return fmt.Errorf("ipAddresses drift: got %v, want %v", gotIP, expectedIP)
	}
	return nil
}

func CertificateSANs(source v1alpha1.SelfSignedCertificateSpec) ([]string, []net.IP) {
	dnsNames := append([]string(nil), source.DNSNames...)
	ipAddressStrings := append([]string(nil), source.IPAddresses...)
	if len(dnsNames) == 0 && len(ipAddressStrings) == 0 {
		if _, err := netip.ParseAddr(source.CommonName); err == nil {
			ipAddressStrings = append(ipAddressStrings, source.CommonName)
		} else if source.CommonName != "" {
			dnsNames = append(dnsNames, source.CommonName)
		}
	}
	ipAddresses := make([]net.IP, 0, len(ipAddressStrings))
	for _, item := range ipAddressStrings {
		address, err := netip.ParseAddr(item)
		if err != nil {
			continue
		}
		ipAddresses = append(ipAddresses, net.IP(append([]byte(nil), address.AsSlice()...)))
	}
	return dnsNames, ipAddresses
}

func NormalizeStringSet(in []string) []string {
	out := append([]string(nil), in...)
	sort.Strings(out)
	if len(out) == 0 {
		return nil
	}
	return out
}

func NormalizeIPSet(in []net.IP) []string {
	out := make([]string, 0, len(in))
	for _, ip := range in {
		out = append(out, ip.String())
	}
	sort.Strings(out)
	if len(out) == 0 {
		return nil
	}
	return out
}
