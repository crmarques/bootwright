package secret

import (
	"crypto/x509"
	"encoding/pem"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/localroot"
)

func TestResolveKeyFilePathUsesInternalCallerHome(t *testing.T) {
	t.Setenv(localroot.InternalEnv, "1")
	t.Setenv(InternalCallerHomeEnv, "/home/operator")

	got, err := ResolveKeyFilePath("~/.ssh/bootwright-ssh-key.pub", "")
	if err != nil {
		t.Fatalf("ResolveKeyFilePath: %v", err)
	}
	want := filepath.Join("/home/operator", ".ssh", "bootwright-ssh-key.pub")
	if got != want {
		t.Fatalf("ResolveKeyFilePath = %q, want %q", got, want)
	}
}

func TestResolveKeyFilePathIgnoresCallerHomeWithoutInternalMarker(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(InternalCallerHomeEnv, "/home/operator")

	got, err := ResolveKeyFilePath("~/.ssh/bootwright-ssh-key", "")
	if err != nil {
		t.Fatalf("ResolveKeyFilePath: %v", err)
	}
	want := filepath.Join(home, ".ssh", "bootwright-ssh-key")
	if got != want {
		t.Fatalf("ResolveKeyFilePath = %q, want %q", got, want)
	}
}

func TestResolveKeyFilePathUsesProcessHomeWithoutInternalCaller(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(InternalCallerHomeEnv, "")

	got, err := ResolveKeyFilePath("~/.ssh/bootwright-ssh-key", "")
	if err != nil {
		t.Fatalf("ResolveKeyFilePath: %v", err)
	}
	want := filepath.Join(home, ".ssh", "bootwright-ssh-key")
	if got != want {
		t.Fatalf("ResolveKeyFilePath = %q, want %q", got, want)
	}
}

func TestResolveSSHKeyPairMaterialPaths(t *testing.T) {
	env := &v1alpha1.Environment{
		Spec: v1alpha1.EnvironmentSpec{
			Secrets: map[string]v1alpha1.EnvironmentSecretSpec{
				"cluster-admin-ssh-key": {
					Generated: &v1alpha1.EnvironmentSecretGenerated{
						SSHKeyPair: &v1alpha1.GeneratedSSHKeyPairSpec{Type: v1alpha1.SSHKeyPairTypeEd25519},
					},
				},
			},
		},
	}
	secretsDir := filepath.Join("context", "secrets")
	if got := ResolveSSHPrivateKeyPath("cluster-admin-ssh-key", env, secretsDir); got != filepath.Join(secretsDir, "cluster-admin-ssh-key") {
		t.Fatalf("private path = %q", got)
	}
	if got := ResolveSSHPublicKeyPath("cluster-admin-ssh-key", env, secretsDir); got != filepath.Join(secretsDir, "cluster-admin-ssh-key.pub") {
		t.Fatalf("public path = %q", got)
	}
}

func TestResolveContextStorageSSHFileSourcePaths(t *testing.T) {
	env := &v1alpha1.Environment{
		SourcePath: filepath.Join("/input", "environment.yaml"),
		Spec: v1alpha1.EnvironmentSpec{
			SecretStorage: v1alpha1.EnvironmentSecretStorageSpec{Mode: v1alpha1.SecretStorageModeContext},
			Secrets: map[string]v1alpha1.EnvironmentSecretSpec{
				"cluster-admin-ssh-key": {File: "keys/admin"},
			},
		},
	}
	secretsDir := filepath.Join("context", "secrets")
	if got := ResolveSSHPrivateKeyPath("cluster-admin-ssh-key", env, secretsDir); got != filepath.Join(secretsDir, "cluster-admin-ssh-key") {
		t.Fatalf("context private path = %q", got)
	}
	if got := ResolveSSHPublicKeyPath("cluster-admin-ssh-key", env, secretsDir); got != filepath.Join(secretsDir, "cluster-admin-ssh-key.pub") {
		t.Fatalf("context public path = %q", got)
	}
	if got := ResolveSourceMaterialPath("cluster-admin-ssh-key", env, MaterialSSHPublic); got != filepath.Join("/input", "keys", "admin.pub") {
		t.Fatalf("source public path = %q", got)
	}
}

func TestParseBMCCredentials(t *testing.T) {
	cases := []struct {
		name             string
		in               string
		wantUser         string
		wantPass         string
		wantErrSubstring string
	}{
		{"happy path", "admin:s3cret", "admin", "s3cret", ""},
		{"with trailing newline", "admin:s3cret\n", "admin", "s3cret", ""},
		{"with CRLF", "admin:s3cret\r\n", "admin", "s3cret", ""},
		{"empty", "", "", "", "is empty"},
		{"missing colon", "admin", "", "", "single username:password"},
		{"leading colon", ":secret", "", "", "single username:password"},
		{"trailing colon", "admin:", "", "", "single username:password"},
		{"multi-line", "admin:s3cret\nextra", "", "", "single username:password"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			u, p, err := ParseBMCCredentials([]byte(tc.in))
			if tc.wantErrSubstring != "" {
				if err == nil {
					t.Fatalf("want error containing %q, got nil", tc.wantErrSubstring)
				}
				if !strings.Contains(err.Error(), tc.wantErrSubstring) {
					t.Fatalf("error = %q, want substring %q", err, tc.wantErrSubstring)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if u != tc.wantUser || p != tc.wantPass {
				t.Errorf("got (%q, %q), want (%q, %q)", u, p, tc.wantUser, tc.wantPass)
			}
		})
	}
}

func TestReadUserPasswordFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "credentials")
	if err := os.WriteFile(path, []byte("admin:s3:cret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	creds, err := ReadUserPasswordFile(path, "proxy credentials")
	if err != nil {
		t.Fatalf("ReadUserPasswordFile: %v", err)
	}
	if creds.Username != "admin" || creds.Password != "s3:cret" {
		t.Fatalf("credentials = %q/%q, want admin/s3:cret", creds.Username, creds.Password)
	}
}

func TestReadUserPasswordFileAddsPathContext(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "credentials")
	if err := os.WriteFile(path, []byte("admin\nother"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := ReadUserPasswordFile(path, "proxy credentials")
	if err == nil {
		t.Fatal("expected error")
	}
	for _, want := range []string{"proxy credentials", path, "single username:password"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q does not contain %q", err, want)
		}
	}
}

func TestValidateBMCUsername(t *testing.T) {
	bad := []string{"", "user:name", "user name", "user\tname", "user\nname"}
	for _, in := range bad {
		if err := ValidateBMCUsername(in); err == nil {
			t.Errorf("ValidateBMCUsername(%q) should reject but accepted", in)
		}
	}
	good := []string{"admin", "user-1", "user.name", "BMC1"}
	for _, in := range good {
		if err := ValidateBMCUsername(in); err != nil {
			t.Errorf("ValidateBMCUsername(%q) rejected: %v", in, err)
		}
	}
}

func TestGenerateBMCPasswordIsUrlSafe(t *testing.T) {
	pw, err := GenerateBMCPassword()
	if err != nil {
		t.Fatal(err)
	}
	if len(pw) < 16 {
		t.Errorf("password too short: %d", len(pw))
	}
	for _, c := range pw {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '_') {
			t.Errorf("non-URL-safe char in password: %q", c)
		}
	}
}

func TestSSHKeyPairPEM(t *testing.T) {
	privateKey, publicKey, err := SSHKeyPairPEM(v1alpha1.GeneratedSSHKeyPairSpec{
		Type:    v1alpha1.SSHKeyPairTypeEd25519,
		Comment: "bootwright-cluster-admin",
	})
	if err != nil {
		t.Fatalf("SSHKeyPairPEM: %v", err)
	}
	if !strings.Contains(string(privateKey), "OPENSSH PRIVATE KEY") {
		t.Fatalf("private key missing OpenSSH header:\n%s", privateKey)
	}
	public := string(publicKey)
	if !strings.HasPrefix(public, "ssh-ed25519 ") || !strings.HasSuffix(public, " bootwright-cluster-admin\n") {
		t.Fatalf("public key = %q", public)
	}
}

func TestValidatePullSecretJSON(t *testing.T) {
	cases := []struct {
		name             string
		in               string
		wantErrSubstring string
	}{
		{"valid", `{"auths":{"quay.io":{"auth":"dXNlcjpwYXNz"}}}`, ""},
		{"empty auths", `{"auths":{}}`, ""},
		{"not json", `not json`, "valid JSON"},
		{"missing auths", `{"other": 1}`, "top-level .auths"},
		{"auths is array", `{"auths":[]}`, ".auths must be a JSON object"},
		{"auths is null", `{"auths":null}`, ".auths must be a JSON object"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidatePullSecretJSON([]byte(tc.in))
			if tc.wantErrSubstring == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("want error containing %q, got nil", tc.wantErrSubstring)
			}
			if !strings.Contains(err.Error(), tc.wantErrSubstring) {
				t.Errorf("error = %q, want substring %q", err, tc.wantErrSubstring)
			}
		})
	}
}

func TestCertificateSANsFallsBackToCommonName(t *testing.T) {
	dns, ip := CertificateSANs(v1alpha1.SelfSignedCertificateSpec{CommonName: "registry.lab"})
	if !reflect.DeepEqual(dns, []string{"registry.lab"}) {
		t.Errorf("dns SAN should default to commonName: %v", dns)
	}
	if len(ip) != 0 {
		t.Errorf("ip SAN should be empty for non-IP commonName: %v", ip)
	}

	dns, ip = CertificateSANs(v1alpha1.SelfSignedCertificateSpec{CommonName: "10.0.0.5"})
	if len(dns) != 0 {
		t.Errorf("dns SAN should be empty when commonName is an IP: %v", dns)
	}
	if len(ip) != 1 || !ip[0].Equal(net.ParseIP("10.0.0.5")) {
		t.Errorf("ip SAN should be the commonName: %v", ip)
	}
}

func TestSelfSignedCertificatePEMRoundTrip(t *testing.T) {
	spec := v1alpha1.SelfSignedCertificateSpec{
		CommonName:   "registry.lab",
		DNSNames:     []string{"registry.lab", "alias.lab"},
		IPAddresses:  []string{"10.0.0.5"},
		ValidityDays: 7,
	}
	certPEM, keyPEM, err := SelfSignedCertificatePEM(spec)
	if err != nil {
		t.Fatalf("SelfSignedCertificatePEM: %v", err)
	}
	block, _ := pem.Decode(certPEM)
	if block == nil {
		t.Fatal("certPEM is not PEM")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("parse cert: %v", err)
	}
	if cert.Subject.CommonName != "registry.lab" {
		t.Errorf("CN = %q, want %q", cert.Subject.CommonName, "registry.lab")
	}
	if !reflect.DeepEqual(NormalizeStringSet(cert.DNSNames), []string{"alias.lab", "registry.lab"}) {
		t.Errorf("DNSNames = %v", cert.DNSNames)
	}
	if len(cert.IPAddresses) != 1 || !cert.IPAddresses[0].Equal(net.ParseIP("10.0.0.5")) {
		t.Errorf("IPAddresses = %v", cert.IPAddresses)
	}
	if !cert.IsCA {
		t.Error("certificate should be marked as CA (we self-sign)")
	}
	if block2, _ := pem.Decode(keyPEM); block2 == nil || block2.Type != "RSA PRIVATE KEY" {
		t.Errorf("keyPEM block type = %v", block2)
	}
}

func TestVerifySelfSignedCertificateMatchesRequest(t *testing.T) {
	spec := v1alpha1.SelfSignedCertificateSpec{
		CommonName:   "registry.lab",
		DNSNames:     []string{"registry.lab"},
		ValidityDays: 7,
	}
	certPEM, _, err := SelfSignedCertificatePEM(spec)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	certPath := filepath.Join(dir, "registry.crt")
	if err := os.WriteFile(certPath, certPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := VerifySelfSignedCertificateMatchesRequest(certPath, spec); err != nil {
		t.Errorf("matching spec rejected: %v", err)
	}
	drifted := spec
	drifted.DNSNames = []string{"other.lab"}
	if err := VerifySelfSignedCertificateMatchesRequest(certPath, drifted); err == nil {
		t.Error("drifted DNS names should be rejected")
	}
}
