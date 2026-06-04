package render

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/crmarques/bootwright/api/v1alpha1"
	secretstore "github.com/crmarques/bootwright/internal/runtime/secrets"
)

func TestValidatePullSecret(t *testing.T) {
	cases := []struct {
		name        string
		content     string
		wantErr     bool
		wantErrPart string
	}{
		{
			name:    "valid",
			content: `{"auths":{"quay.io":{"auth":"dXNlcjpwYXNz"}}}`,
		},
		{
			name:        "empty",
			content:     "",
			wantErr:     true,
			wantErrPart: "is empty",
		},
		{
			name:        "not json",
			content:     "not json",
			wantErr:     true,
			wantErrPart: "not valid JSON",
		},
		{
			name:        "missing auths",
			content:     `{"other":1}`,
			wantErr:     true,
			wantErrPart: "missing .auths",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validatePullSecret(tc.content, "/fake/path")
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tc.wantErrPart)
				}
				if !strings.Contains(err.Error(), tc.wantErrPart) {
					t.Fatalf("error %q missing substring %q", err, tc.wantErrPart)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestReadUserPassFile(t *testing.T) {
	cases := []struct {
		name        string
		content     string
		wantUser    string
		wantPass    string
		wantErrPart string
	}{
		{name: "happy path", content: "alice:s3cret", wantUser: "alice", wantPass: "s3cret"},
		{name: "trailing newline ok", content: "alice:s3cret\n", wantUser: "alice", wantPass: "s3cret"},
		{name: "password with colon", content: "alice:s3:cret", wantUser: "alice", wantPass: "s3:cret"},
		{name: "empty", content: "", wantErrPart: "is empty"},
		{name: "no colon", content: "alice", wantErrPart: "single username:password"},
		{name: "empty user", content: ":pass", wantErrPart: "single username:password"},
		{name: "empty pass", content: "alice:", wantErrPart: "single username:password"},
		{name: "two lines", content: "alice:pass\nbob:other", wantErrPart: "single username:password"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "creds")
			if err := os.WriteFile(path, []byte(tc.content), 0o600); err != nil {
				t.Fatalf("write fixture: %v", err)
			}
			got, err := readUserPassFile(path, "credential")
			if tc.wantErrPart != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got user=%q pass=%q", tc.wantErrPart, got.Username, got.Password)
				}
				if !strings.Contains(err.Error(), tc.wantErrPart) {
					t.Fatalf("error %q missing substring %q", err, tc.wantErrPart)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.Username != tc.wantUser || got.Password != tc.wantPass {
				t.Fatalf("user/pass got %q/%q, want %q/%q", got.Username, got.Password, tc.wantUser, tc.wantPass)
			}
		})
	}
}

func TestReadUserPassFileMissingFile(t *testing.T) {
	_, err := readUserPassFile("/nonexistent/credential", "credential")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
	if !strings.Contains(err.Error(), "/nonexistent/credential") {
		t.Fatalf("error %q missing the path", err)
	}
}

func TestLoadInstallerSecretsUsesGeneratedSSHPublicKey(t *testing.T) {
	secretsDir := t.TempDir()
	writeEncryptedSecret(t, secretsDir, "pull", secretstore.MaterialPrimary, `{"auths":{"quay.io":{"auth":"dXNlcjpwYXNz"}}}`)
	writeEncryptedSecret(t, secretsDir, "ssh", secretstore.MaterialSSHPrivate, "PRIVATE\n")
	writeEncryptedSecret(t, secretsDir, "ssh", secretstore.MaterialSSHPublic, "ssh-ed25519 AAAA generated\n")
	state := v1alpha1.State{
		Environments: []v1alpha1.Environment{{
			Metadata: v1alpha1.Metadata{Name: "env"},
			Spec: v1alpha1.EnvironmentSpec{
				BaseDomain: "example.test",
				Secrets: map[string]v1alpha1.EnvironmentSecretSpec{
					"pull": {},
					"ssh": {
						Generated: &v1alpha1.EnvironmentSecretGenerated{
							SSHKeyPair: &v1alpha1.GeneratedSSHKeyPairSpec{Type: v1alpha1.SSHKeyPairTypeEd25519},
						},
					},
				},
			},
		}},
		Machines: []v1alpha1.Machine{installerNodeMachine("master-0")},
	}
	ocp := v1alpha1.ContainerCluster{
		Metadata: v1alpha1.Metadata{Name: "ocp"},
		Spec: v1alpha1.ContainerClusterSpec{
			Install: v1alpha1.OCPInstallSpec{
				PullSecretRef: v1alpha1.SecretRef{Name: "pull"},
				NodeSSH:       v1alpha1.NodeSSHSpec{KeyPairRef: v1alpha1.SecretRef{Name: "ssh"}},
			},
			Nodes: []v1alpha1.OCPNodeSpec{{
				Hostname:   "master-0",
				Role:       v1alpha1.NodeRoleMaster,
				MachineRef: v1alpha1.LocalObjectReference{Name: "master-0"},
			}},
		},
	}

	secrets, err := LoadInstallerSecrets(state, ocp, secretsDir)
	if err != nil {
		t.Fatalf("LoadInstallerSecrets: %v", err)
	}
	if secrets.SSHKey != "ssh-ed25519 AAAA generated" {
		t.Fatalf("SSHKey = %q", secrets.SSHKey)
	}
}

func TestLoadInstallerSecretsUsesNodeSSHPublicKeyRef(t *testing.T) {
	secretsDir := t.TempDir()
	writeEncryptedSecret(t, secretsDir, "pull", secretstore.MaterialPrimary, `{"auths":{"quay.io":{"auth":"dXNlcjpwYXNz"}}}`)
	sourceDir := t.TempDir()
	for name, content := range map[string]string{
		"admin.pub": "ssh-ed25519 AAAA public\n",
		"admin":     "PRIVATE\n",
	} {
		if err := os.WriteFile(filepath.Join(sourceDir, name), []byte(content), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	state := v1alpha1.State{
		Environments: []v1alpha1.Environment{{
			Metadata:   v1alpha1.Metadata{Name: "env"},
			SourcePath: filepath.Join(sourceDir, "environment.yaml"),
			Spec: v1alpha1.EnvironmentSpec{
				BaseDomain: "example.test",
				Secrets: map[string]v1alpha1.EnvironmentSecretSpec{
					"pull":            {},
					"cluster-public":  {File: "admin.pub"},
					"cluster-private": {File: "admin"},
				},
			},
		}},
		Machines: []v1alpha1.Machine{installerNodeMachine("master-0")},
	}
	ocp := v1alpha1.ContainerCluster{
		Metadata: v1alpha1.Metadata{Name: "ocp"},
		Spec: v1alpha1.ContainerClusterSpec{
			Install: v1alpha1.OCPInstallSpec{
				PullSecretRef: v1alpha1.SecretRef{Name: "pull"},
				NodeSSH: v1alpha1.NodeSSHSpec{
					PublicKeyRef:  v1alpha1.SecretRef{Name: "cluster-public"},
					PrivateKeyRef: v1alpha1.SecretRef{Name: "cluster-private"},
				},
			},
			Nodes: []v1alpha1.OCPNodeSpec{{
				Hostname:   "master-0",
				Role:       v1alpha1.NodeRoleMaster,
				MachineRef: v1alpha1.LocalObjectReference{Name: "master-0"},
			}},
		},
	}

	secrets, err := LoadInstallerSecrets(state, ocp, secretsDir)
	if err != nil {
		t.Fatalf("LoadInstallerSecrets: %v", err)
	}
	if secrets.SSHKey != "ssh-ed25519 AAAA public" {
		t.Fatalf("SSHKey = %q", secrets.SSHKey)
	}
}

func installerNodeMachine(name string) v1alpha1.Machine {
	return v1alpha1.Machine{
		Metadata: v1alpha1.Metadata{Name: name},
		Spec: v1alpha1.MachineSpec{
			OS: v1alpha1.MachineOSSpec{Provided: v1alpha1.BoolPtr(false)},
		},
	}
}

func TestMergeMirrorAuth(t *testing.T) {
	original := `{"auths":{"quay.io":{"auth":"ZXhpc3Q="}}}`
	merged, err := mergeMirrorAuth(original, "mirror.local:5000", userPass{Username: "alice", Password: "s3cret"})
	if err != nil {
		t.Fatalf("mergeMirrorAuth: %v", err)
	}

	var doc struct {
		Auths map[string]struct {
			Auth string `json:"auth"`
		} `json:"auths"`
	}
	if err := json.Unmarshal([]byte(merged), &doc); err != nil {
		t.Fatalf("parse merged pull secret: %v", err)
	}
	if _, ok := doc.Auths["quay.io"]; !ok {
		t.Fatal("merged pull secret dropped the existing quay.io entry")
	}
	mirror, ok := doc.Auths["mirror.local:5000"]
	if !ok {
		t.Fatal("merged pull secret missing mirror.local:5000")
	}
	decoded, err := base64.StdEncoding.DecodeString(mirror.Auth)
	if err != nil {
		t.Fatalf("decode mirror auth: %v", err)
	}
	if got := string(decoded); got != "alice:s3cret" {
		t.Fatalf("mirror auth decoded to %q, want %q", got, "alice:s3cret")
	}
}

func TestMergeMirrorAuthEmptyDoc(t *testing.T) {
	merged, err := mergeMirrorAuth(`{}`, "mirror.local:5000", userPass{Username: "u", Password: "p"})
	if err != nil {
		t.Fatalf("mergeMirrorAuth: %v", err)
	}
	if !strings.Contains(merged, "mirror.local:5000") {
		t.Fatalf("merged missing mirror entry: %s", merged)
	}
}

func TestMergeMirrorAuthInvalidJSON(t *testing.T) {
	if _, err := mergeMirrorAuth("not json", "mirror.local:5000", userPass{Username: "u", Password: "p"}); err == nil {
		t.Fatal("expected error for invalid pull secret JSON")
	}
}

func TestMergeMirrorAuthRejectsEmptyRegistryURL(t *testing.T) {
	if _, err := mergeMirrorAuth(`{"auths":{}}`, "", userPass{Username: "u", Password: "p"}); err == nil {
		t.Fatal("expected error for empty registry URL")
	}
}

func TestLoadInstallerSecretsMergesManagedMirrorAuth(t *testing.T) {
	secretsDir := t.TempDir()
	writeEncryptedSecret(t, secretsDir, "pull", secretstore.MaterialPrimary, `{"auths":{"quay.io":{"auth":"ZXhpc3Q="}}}`)
	writeEncryptedSecret(t, secretsDir, "ssh", secretstore.MaterialSSHPublic, "ssh-rsa AAAA test\n")
	writeEncryptedSecret(t, secretsDir, "mirror-creds", secretstore.MaterialPrimary, "mirror-user:mirror-pass\n")
	mirrorTrust, err := fastTestCertificatePEM("registry.lab")
	if err != nil {
		t.Fatalf("generate mirror trust: %v", err)
	}
	writeEncryptedSecretBytes(t, secretsDir, "mirror-trust", secretstore.MaterialPrimary, mirrorTrust)
	state := v1alpha1.State{
		Environments: []v1alpha1.Environment{{
			Metadata: v1alpha1.Metadata{Name: "env"},
			Spec: v1alpha1.EnvironmentSpec{
				Registries: &v1alpha1.EnvironmentRegistriesSpec{Mirror: &v1alpha1.EnvironmentRegistryMirrorSpec{
					CredentialsRef: v1alpha1.SecretRef{Name: "mirror-creds"},
					TrustBundleRef: v1alpha1.SecretRef{Name: "mirror-trust"},
				}},
				InfraComponents: v1alpha1.EnvironmentInfraComponentsSpec{
					Registries: []v1alpha1.EnvironmentRegistryComponent{{
						Name:         "default",
						Type:         v1alpha1.EnvironmentComponentManaged,
						ComponentRef: v1alpha1.LocalObjectReference{Name: "registry"},
					}},
				},
			},
		}},
		Machines: []v1alpha1.Machine{{
			Metadata: v1alpha1.Metadata{Name: "registry-host"},
			Spec: v1alpha1.MachineSpec{
				Capabilities: []string{v1alpha1.MachineCapabilityContainerRuntime, v1alpha1.MachineCapabilityRegistry},
				OS: v1alpha1.MachineOSSpec{
					Provided: v1alpha1.BoolPtr(true),
				},
				Addresses: []v1alpha1.MachineAddress{{Name: "ssh", Address: "registry.lab"}},
				Access: v1alpha1.MachineAccess{
					SSH: &v1alpha1.MachineSSHSpec{AddressRef: v1alpha1.LocalObjectReference{Name: "ssh"}},
				},
			},
		}, installerNodeMachine("master-0")},
		InfraComponents: []v1alpha1.InfraComponent{{
			Metadata: v1alpha1.Metadata{Name: "registry"},
			Spec: v1alpha1.InfraComponentSpec{Registry: &v1alpha1.RegistryComponent{
				Type:       v1alpha1.InfraComponentTypeMirrorRegistry,
				MachineRef: v1alpha1.LocalObjectReference{Name: "registry-host"},
				Port:       5000,
			}},
		}},
	}
	ocp := v1alpha1.ContainerCluster{
		Metadata: v1alpha1.Metadata{Name: "ocp"},
		Spec: v1alpha1.ContainerClusterSpec{
			Install: v1alpha1.OCPInstallSpec{
				Mode:          v1alpha1.InstallModeDisconnected,
				PullSecretRef: v1alpha1.SecretRef{Name: "pull"},
				NodeSSH:       v1alpha1.NodeSSHSpec{KeyPairRef: v1alpha1.SecretRef{Name: "ssh"}},
			},
			Nodes: []v1alpha1.OCPNodeSpec{{
				Hostname:   "master-0",
				Role:       v1alpha1.NodeRoleMaster,
				MachineRef: v1alpha1.LocalObjectReference{Name: "master-0"},
			}},
		},
	}

	secrets, err := LoadInstallerSecrets(state, ocp, secretsDir)
	if err != nil {
		t.Fatalf("LoadInstallerSecrets: %v", err)
	}
	var doc struct {
		Auths map[string]struct {
			Auth string `json:"auth"`
		} `json:"auths"`
	}
	if err := json.Unmarshal([]byte(secrets.PullSecret), &doc); err != nil {
		t.Fatalf("parse merged pull secret: %v", err)
	}
	mirror, ok := doc.Auths["registry.lab:5000"]
	if !ok {
		t.Fatalf("merged pull secret missing managed mirror auth: %v", doc.Auths)
	}
	decoded, err := base64.StdEncoding.DecodeString(mirror.Auth)
	if err != nil {
		t.Fatalf("decode mirror auth: %v", err)
	}
	if got := string(decoded); got != "mirror-user:mirror-pass" {
		t.Fatalf("mirror auth decoded to %q, want %q", got, "mirror-user:mirror-pass")
	}
}

func fastTestCertificatePEM(commonName string) ([]byte, error) {
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	serialLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serial, err := rand.Int(rand.Reader, serialLimit)
	if err != nil {
		return nil, err
	}
	template := x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName: commonName,
		},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().AddDate(0, 0, 1),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
		DNSNames:              []string{commonName},
	}
	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &privateKey.PublicKey, privateKey)
	if err != nil {
		return nil, err
	}
	var certBuf bytes.Buffer
	if err := pem.Encode(&certBuf, &pem.Block{Type: "CERTIFICATE", Bytes: der}); err != nil {
		return nil, err
	}
	return certBuf.Bytes(), nil
}

func TestBakeProxyCredentials(t *testing.T) {
	cases := []struct {
		name    string
		raw     string
		creds   userPass
		wantErr bool
		want    string
	}{
		{name: "happy path", raw: "http://proxy.lab:3128", creds: userPass{Username: "u", Password: "p"}, want: "http://u:p@proxy.lab:3128"},
		{name: "dollar preserved", raw: "http://proxy.lab:3128", creds: userPass{Username: "u", Password: "p$w"}, want: "http://u:p$w@proxy.lab:3128"},
		{name: "special chars escaped", raw: "https://proxy.lab", creds: userPass{Username: "u@x", Password: "p:s/q"}, want: "https://u%40x:p%3As%2Fq@proxy.lab"},
		{name: "replaces existing user", raw: "http://old:old@proxy.lab", creds: userPass{Username: "n", Password: "n"}, want: "http://n:n@proxy.lab"},
		{name: "missing scheme", raw: "proxy.lab", wantErr: true},
		{name: "missing host", raw: "http:///path", wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := bakeProxyCredentials(tc.raw, tc.creds)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
			// Sanity-check that the encoded URL round-trips and the
			// password (which may contain reserved chars) decodes back
			// to the original — guards against future "raw" embedding
			// regressions that would leak literal `:` or `/` into the
			// userinfo segment and break HTTP_PROXY parsers downstream.
			u, err := url.Parse(got)
			if err != nil {
				t.Fatalf("baked URL does not parse: %v", err)
			}
			pwd, _ := u.User.Password()
			if pwd != tc.creds.Password {
				t.Fatalf("password round-trip got %q, want %q", pwd, tc.creds.Password)
			}
		})
	}
}

func writeEncryptedSecret(t *testing.T, dir, name string, role secretstore.MaterialRole, content string) {
	t.Helper()
	writeEncryptedSecretBytes(t, dir, name, role, []byte(content))
}

func writeEncryptedSecretBytes(t *testing.T, dir, name string, role secretstore.MaterialRole, content []byte) {
	t.Helper()
	store := secretstore.NewContextStore("test", dir)
	if err := store.Write(secretstore.MaterialKey{Name: name, Role: role}, content); err != nil {
		t.Fatalf("write encrypted %s/%s: %v", name, role, err)
	}
}
