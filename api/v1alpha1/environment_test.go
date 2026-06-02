package v1alpha1

import (
	"encoding/json"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

type environmentSecretsHolder struct {
	Secrets EnvironmentSecrets `yaml:"secrets" json:"secrets"`
}

func TestEnvironmentSecretsYAMLListShape(t *testing.T) {
	body := `secrets:
  - openshift-pull-secret
  - cluster-admin-ssh-key:
      generated:
        sshKeyPair:
          comment: bootwright-cluster-admin
  - provider-host-ssh:
      file: ~/.ssh/bootwright-ssh-key
  - api-tls:
      file: ../secrets/api.crt
      keyFile: ../secrets/api.key
`
	var holder environmentSecretsHolder
	if err := yaml.Unmarshal([]byte(body), &holder); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got := len(holder.Secrets); got != 4 {
		t.Fatalf("len = %d, want 4", got)
	}
	if spec := holder.Secrets["openshift-pull-secret"]; spec.File != "" || spec.Generated != nil {
		t.Fatalf("openshift-pull-secret = %+v, want context-local empty source", spec)
	}
	if got := holder.Secrets["cluster-admin-ssh-key"].Generated.SSHKeyPair.Comment; got != "bootwright-cluster-admin" {
		t.Fatalf("ssh key comment = %q", got)
	}
	if got := holder.Secrets["provider-host-ssh"].File; got != "~/.ssh/bootwright-ssh-key" {
		t.Fatalf("provider-host-ssh file = %q", got)
	}
	if got := holder.Secrets["api-tls"].KeyFile; got != "../secrets/api.key" {
		t.Fatalf("api-tls keyFile = %q", got)
	}

	data, err := yaml.Marshal(holder)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	rendered := string(data)
	if strings.Contains(rendered, "null") {
		t.Fatalf("marshal rendered null:\n%s", rendered)
	}
	if !strings.Contains(rendered, "- openshift-pull-secret\n") {
		t.Fatalf("marshal did not render scalar context-local secret:\n%s", rendered)
	}
}

func TestEnvironmentSecretsYAMLRejectsInvalidShapes(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{
			name: "legacy-mapping",
			body: "secrets:\n  openshift-pull-secret:\n",
			want: "spec.secrets must be a list",
		},
		{
			name: "duplicate",
			body: "secrets:\n  - pull\n  - pull\n",
			want: `spec.secrets[1] "pull" is duplicated`,
		},
		{
			name: "empty-scalar",
			body: "secrets:\n  - \"\"\n",
			want: "spec.secrets[0] secret name must not be empty",
		},
		{
			name: "multi-key-object",
			body: "secrets:\n  - a:\n      file: a\n    b:\n      file: b\n",
			want: "spec.secrets[0] object item must contain exactly one secret name",
		},
		{
			name: "null-object-value",
			body: "secrets:\n  - pull:\n",
			want: "spec.secrets[0][pull] must be an object, not null",
		},
		{
			name: "empty-object-value",
			body: "secrets:\n  - pull: {}\n",
			want: "object form requires file, keyFile, or generated",
		},
		{
			name: "unknown-field",
			body: "secrets:\n  - pull:\n      path: ./pull-secret.json\n",
			want: "field path not found",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var holder environmentSecretsHolder
			err := yaml.Unmarshal([]byte(tc.body), &holder)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q does not contain %q", err, tc.want)
			}
		})
	}
}

func TestEnvironmentSecretsJSONListShape(t *testing.T) {
	body := `{"secrets":["openshift-pull-secret",{"provider-host-ssh":{"file":"~/.ssh/bootwright-ssh-key"}}]}`
	var holder environmentSecretsHolder
	if err := json.Unmarshal([]byte(body), &holder); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got := holder.Secrets["provider-host-ssh"].File; got != "~/.ssh/bootwright-ssh-key" {
		t.Fatalf("provider-host-ssh file = %q", got)
	}
	data, err := json.Marshal(holder)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	rendered := string(data)
	if strings.Contains(rendered, "null") {
		t.Fatalf("marshal rendered null: %s", rendered)
	}
	if !strings.Contains(rendered, `"openshift-pull-secret"`) {
		t.Fatalf("marshal missing context-local secret: %s", rendered)
	}
}
