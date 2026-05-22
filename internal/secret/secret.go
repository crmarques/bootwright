package secret

import (
	"bytes"
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
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

// ParseBMCCredentials decodes a single `username:password` line into
// its parts. Empty input, multi-line input, missing colon, and a
// trailing colon all return errors that name the expected shape.
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
	data, err := os.ReadFile(path)
	if err != nil {
		return UserPassword{}, fmt.Errorf("read %s at %s: %w", kind, path, err)
	}
	username, password, err := ParseBMCCredentials(data)
	if err != nil {
		return UserPassword{}, fmt.Errorf("%s at %s: %w", kind, path, err)
	}
	return UserPassword{Username: username, Password: password}, nil
}

// ValidateBMCUsername rejects usernames containing whitespace or ':',
// matching the line-format ParseBMCCredentials expects.
func ValidateBMCUsername(username string) error {
	if username == "" {
		return errors.New("username must not be empty")
	}
	if strings.ContainsAny(username, ":\r\n\t ") {
		return errors.New("username must not contain whitespace or ':'")
	}
	return nil
}

// GenerateBMCPassword returns a 24-byte cryptographically-random URL-
// safe base64 password. Used by `secret set --generate` for lab BMCs.
func GenerateBMCPassword() (string, error) {
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate BMC password: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// ValidatePullSecretJSON checks that data parses as a JSON object with
// a top-level `.auths` object. Used by `secret set <name> --pull-secret`
// to catch broken inputs before they reach openshift-install.
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

// SelfSignedCertificatePEM generates a 4096-bit RSA key + self-signed
// certificate matching the requested spec (commonName / dnsNames /
// ipAddresses / validityDays). Returns the PEM-encoded cert and key.
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

// VerifySelfSignedCertificateMatchesRequest parses the PEM cert at
// certPath and asserts its commonName, DNS names, and IP addresses
// match the requested spec. Used by `secret generate` to detect drift
// between the desired-state spec and a previously-generated cert.
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

// CertificateSANs returns the (DNS names, IP addresses) the cert
// should carry. When neither dnsNames nor ipAddresses are explicit on
// the spec, the commonName is added in whichever shape it parses as.
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
