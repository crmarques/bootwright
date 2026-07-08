package v1alpha1

import (
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

func TestSecretSourceIsZero(t *testing.T) {
	cases := []struct {
		name string
		src  SecretSource
		want bool
	}{
		{"empty selects contextStore", SecretSource{}, true},
		{"explicit contextStore", SecretSource{ContextStore: &SecretContextStoreSource{}}, false},
		{"file", SecretSource{File: &SecretFileSource{Path: "x"}}, false},
		{"generated", SecretSource{Generated: &SecretGeneratedSource{}}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.src.IsZero(); got != tc.want {
				t.Fatalf("IsZero() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestSecretYAMLRoundTrip(t *testing.T) {
	body := `apiVersion: bootwright.io/v1alpha1
kind: Secret
metadata:
  name: api-serving-cert
spec:
  type: tlsCertificate
  source:
    file:
      cert: ./tls/api.crt
      key: ./tls/api.key
`
	var secret Secret
	if err := yaml.Unmarshal([]byte(body), &secret); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if secret.Spec.Type != SecretTypeTLSCertificate {
		t.Fatalf("type = %q, want %q", secret.Spec.Type, SecretTypeTLSCertificate)
	}
	if secret.Spec.Source.File == nil || secret.Spec.Source.File.Cert != "./tls/api.crt" || secret.Spec.Source.File.Key != "./tls/api.key" {
		t.Fatalf("file source = %+v", secret.Spec.Source.File)
	}

	data, err := yaml.Marshal(secret)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if got := string(data); !strings.Contains(got, "type: tlsCertificate") || !strings.Contains(got, "cert: ./tls/api.crt") {
		t.Fatalf("marshal round-trip lost fields:\n%s", got)
	}
}

// A context-store secret omits its source block on marshal (empty source arm
// selects contextStore), keeping the common case terse.
func TestSecretContextStoreOmitsSource(t *testing.T) {
	secret := Secret{
		APIVersion: APIVersion,
		Kind:       KindSecret,
		Metadata:   Metadata{Name: "openshift-pull-secret"},
		Spec:       SecretSpec{Type: SecretTypeDockerConfigJSON},
	}
	data, err := yaml.Marshal(secret)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if got := string(data); strings.Contains(got, "source:") {
		t.Fatalf("empty source should be omitted:\n%s", got)
	}
}

func TestSecretGeneratedSourceScopedParams(t *testing.T) {
	body := `apiVersion: bootwright.io/v1alpha1
kind: Secret
metadata:
  name: cluster-admin-ssh-key
spec:
  type: sshKeyPair
  source:
    generated:
      keyType: ed25519
      comment: bootwright@lab
`
	var secret Secret
	if err := yaml.Unmarshal([]byte(body), &secret); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	gen := secret.Spec.Source.Generated
	if gen == nil || gen.KeyType != SSHKeyPairTypeEd25519 || gen.Comment != "bootwright@lab" {
		t.Fatalf("generated = %+v", gen)
	}
}

func TestSecretTypeGeneratable(t *testing.T) {
	generatable := map[string]bool{
		SecretTypeToken:            true,
		SecretTypeUsernamePassword: true,
		SecretTypeTLSCertificate:   true,
		SecretTypeSSHKeyPair:       true,
		SecretTypeOpaque:           false,
		SecretTypeDockerConfigJSON: false,
		SecretTypeCABundle:         true,
	}
	for _, secretType := range SecretTypes() {
		want, ok := generatable[secretType]
		if !ok {
			t.Fatalf("SecretTypes() has %q with no generatable expectation", secretType)
		}
		if got := SecretTypeGeneratable(secretType); got != want {
			t.Errorf("SecretTypeGeneratable(%q) = %v, want %v", secretType, got, want)
		}
	}
	if len(SecretTypes()) != len(generatable) {
		t.Fatalf("SecretTypes() has %d entries, expectations have %d", len(SecretTypes()), len(generatable))
	}
}
