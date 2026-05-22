package render

import (
	"encoding/base64"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
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
	for name, content := range map[string]string{
		"pull":         `{"auths":{"quay.io":{"auth":"ZXhpc3Q="}}}`,
		"ssh":          "ssh-rsa AAAA test\n",
		"mirror-creds": "mirror-user:mirror-pass\n",
	} {
		if err := os.WriteFile(filepath.Join(secretsDir, name), []byte(content), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	state := v1alpha1.State{
		Environments: []v1alpha1.Environment{{
			Metadata: v1alpha1.Metadata{Name: "env"},
			Spec: v1alpha1.EnvironmentSpec{
				Registries: &v1alpha1.EnvironmentRegistriesSpec{Mirror: &v1alpha1.EnvironmentRegistryMirrorSpec{
					CredentialsRef: v1alpha1.SecretRef{Name: "mirror-creds"},
					TrustBundleRef: v1alpha1.SecretRef{Name: "mirror-trust"},
				}},
			},
		}},
		Hosts: []v1alpha1.Host{{
			Metadata: v1alpha1.Metadata{Name: "registry-host"},
			Spec: v1alpha1.HostSpec{
				Addresses:    []v1alpha1.HostAddress{{Name: "ssh", Address: "registry.lab"}},
				SSH:          &v1alpha1.HostSSHSpec{AddressName: "ssh"},
				Capabilities: []string{v1alpha1.HostCapabilityContainerRuntime},
			},
		}},
		InfraProviders: []v1alpha1.InfraProvider{{
			Metadata: v1alpha1.Metadata{Name: "provider"},
			Spec: v1alpha1.InfraProviderSpec{
				Registries: []v1alpha1.RegistryCapability{{
					Name:           "default",
					MirrorRegistry: &v1alpha1.MirrorRegistryCapability{HostRef: v1alpha1.LocalObjectReference{Name: "registry-host"}},
				}},
			},
		}},
		ClusterInfras: []v1alpha1.ClusterInfra{{
			Metadata: v1alpha1.Metadata{Name: "infra"},
			Spec: v1alpha1.ClusterInfraSpec{Components: v1alpha1.ClusterComponents{
				Registry: &v1alpha1.ClusterComponentRef{
					From: v1alpha1.From{Provider: "provider", Name: "default"},
					Port: 5000,
				},
			}},
		}},
	}
	ocp := v1alpha1.ContainerCluster{
		Metadata: v1alpha1.Metadata{Name: "ocp"},
		Spec: v1alpha1.ContainerClusterSpec{
			Install: v1alpha1.OCPInstallSpec{
				Mode:          v1alpha1.InstallModeDisconnected,
				PullSecretRef: v1alpha1.SecretRef{Name: "pull"},
				SSHKeyRef:     v1alpha1.SecretRef{Name: "ssh"},
			},
			Nodes: []v1alpha1.OCPNodeSpec{{
				Hostname: "master-0",
				Role:     v1alpha1.NodeRoleMaster,
				MachineRef: v1alpha1.NodeMachineRef{
					ClusterInfra: "infra",
					Name:         "master-0",
				},
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
