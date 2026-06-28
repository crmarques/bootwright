package secret

import (
	"path/filepath"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func TestSecretPlaceholder(t *testing.T) {
	cases := []struct {
		name, suffix, want string
	}{
		{"pull-secret", "", "{{ secret pull-secret }}"},
		{"api-tls", "tls-key", "{{ secret api-tls.tls-key }}"},
		{"vc", "username", "{{ secret vc.username }}"},
		{"", "", ""},
		{"", "tls-key", ""},
	}
	for _, c := range cases {
		if got := SecretPlaceholder(c.name, c.suffix); got != c.want {
			t.Errorf("SecretPlaceholder(%q,%q)=%q want %q", c.name, c.suffix, got, c.want)
		}
	}
}

// TestResolveMaterialPathPlaceholderMode pins that the placeholder sentinel
// turns every Resolve* path into a portable {{ secret <name>[.<role>] }} token,
// with the role suffix matching the MaterialRole value (primary omits it).
func TestResolveMaterialPathPlaceholderMode(t *testing.T) {
	cases := []struct {
		role MaterialRole
		want string
	}{
		{MaterialPrimary, "{{ secret api-tls }}"},
		{MaterialTLSKey, "{{ secret api-tls.tls-key }}"},
		{MaterialSSHPrivate, "{{ secret api-tls.ssh-private }}"},
		{MaterialSSHPublic, "{{ secret api-tls.ssh-public }}"},
	}
	for _, c := range cases {
		if got := ResolveMaterialPath("api-tls", nil, PlaceholderSecretsDir, c.role); got != c.want {
			t.Errorf("ResolveMaterialPath role=%s = %q want %q", c.role, got, c.want)
		}
	}
	if got := ResolveMaterialPath("", nil, PlaceholderSecretsDir, MaterialPrimary); got != "" {
		t.Errorf("empty name = %q want \"\"", got)
	}
	// The typed wrappers inherit the sentinel behavior through ResolveMaterialPath.
	if got := ResolveSSHPublicKeyPath("k", nil, PlaceholderSecretsDir); got != "{{ secret k.ssh-public }}" {
		t.Errorf("ResolveSSHPublicKeyPath = %q", got)
	}
}

// TestResolveMaterialPathPlaceholderBypassesExternalSource ensures placeholder
// mode short-circuits BEFORE the external-source branch, so a portable bundle
// never leaks the operator's local source-file path for a file-sourced secret.
func TestResolveMaterialPathPlaceholderBypassesExternalSource(t *testing.T) {
	env := &v1alpha1.Environment{
		SourcePath: filepath.Join("/input", "environment.yaml"),
		Spec: v1alpha1.EnvironmentSpec{
			Secrets: map[string]v1alpha1.EnvironmentSecretSpec{
				"pull-secret": {File: "pull-secret.json"},
			},
		},
	}
	if got := ResolveMaterialPath("pull-secret", env, PlaceholderSecretsDir, MaterialPrimary); got != "{{ secret pull-secret }}" {
		t.Fatalf("external-source secret in placeholder mode = %q, want a token (no source path)", got)
	}
}

func TestIsPlaceholderSecretsDir(t *testing.T) {
	if !IsPlaceholderSecretsDir(PlaceholderSecretsDir) {
		t.Fatal("sentinel not recognized")
	}
	if IsPlaceholderSecretsDir("/real/secrets") {
		t.Fatal("real path wrongly recognized as sentinel")
	}
}
