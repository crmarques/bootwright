package workflow

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

func dockerConfig(t *testing.T, auths map[string]map[string]string) []byte {
	t.Helper()
	data, err := json.Marshal(map[string]any{"auths": auths})
	if err != nil {
		t.Fatalf("marshal dockerconfig: %v", err)
	}
	return data
}

func decodeAuths(t *testing.T, config []byte) map[string]map[string]string {
	t.Helper()
	var doc struct {
		Auths map[string]map[string]string `json:"auths"`
	}
	if err := json.Unmarshal(config, &doc); err != nil {
		t.Fatalf("decode merged dockerconfig: %v", err)
	}
	return doc.Auths
}

func TestMergedDockerConfigAuthAddsEntryPreservingOthers(t *testing.T) {
	existing := dockerConfig(t, map[string]map[string]string{
		"quay.io":             {"auth": "cXVheQ=="},
		"registry.redhat.io":  {"auth": "cmg="},
		"cloud.openshift.com": {"auth": "Y2xvdWQ=", "email": "ops@example.com"},
	})
	merged, changed, err := mergedDockerConfigAuth(existing, "cp.icr.io", "cp", "ENTITLEMENT")
	if err != nil {
		t.Fatalf("mergedDockerConfigAuth: %v", err)
	}
	if !changed {
		t.Fatal("expected changed=true when the registry entry is missing")
	}
	auths := decodeAuths(t, merged)
	if len(auths) != 4 {
		t.Fatalf("auths = %d entries, want 4 (others preserved)", len(auths))
	}
	want := base64.StdEncoding.EncodeToString([]byte("cp:ENTITLEMENT"))
	if auths["cp.icr.io"]["auth"] != want {
		t.Fatalf("cp.icr.io auth = %q, want %q", auths["cp.icr.io"]["auth"], want)
	}
	if auths["cloud.openshift.com"]["email"] != "ops@example.com" {
		t.Fatal("unrelated entry fields must be preserved")
	}
}

func TestMergedDockerConfigAuthIdempotentOnMatchingCredential(t *testing.T) {
	auth := base64.StdEncoding.EncodeToString([]byte("cp:ENTITLEMENT"))
	existing := dockerConfig(t, map[string]map[string]string{
		"cp.icr.io": {"auth": auth, "email": "ops@example.com"},
	})
	_, changed, err := mergedDockerConfigAuth(existing, "cp.icr.io", "cp", "ENTITLEMENT")
	if err != nil {
		t.Fatalf("mergedDockerConfigAuth: %v", err)
	}
	if changed {
		t.Fatal("expected changed=false when the credential already matches")
	}
}

func TestMergedDockerConfigAuthReplacesStaleCredential(t *testing.T) {
	existing := dockerConfig(t, map[string]map[string]string{
		"cp.icr.io": {"auth": base64.StdEncoding.EncodeToString([]byte("cp:OLD"))},
	})
	merged, changed, err := mergedDockerConfigAuth(existing, "cp.icr.io", "cp", "NEW")
	if err != nil {
		t.Fatalf("mergedDockerConfigAuth: %v", err)
	}
	if !changed {
		t.Fatal("expected changed=true for a rotated credential")
	}
	auths := decodeAuths(t, merged)
	want := base64.StdEncoding.EncodeToString([]byte("cp:NEW"))
	if auths["cp.icr.io"]["auth"] != want {
		t.Fatalf("cp.icr.io auth = %q, want %q", auths["cp.icr.io"]["auth"], want)
	}
}

func TestMergedPullSecretReplacementPreservesResourceVersion(t *testing.T) {
	config := dockerConfig(t, map[string]map[string]string{"quay.io": {"auth": "cXVheQ=="}})
	live, err := json.Marshal(map[string]any{
		"metadata": map[string]any{"name": "pull-secret", "namespace": "openshift-config", "resourceVersion": "12345"},
		"data":     map[string]string{".dockerconfigjson": base64.StdEncoding.EncodeToString(config)},
	})
	if err != nil {
		t.Fatalf("marshal live secret: %v", err)
	}
	replacement, changed, err := mergedPullSecretReplacement(live, "cp.icr.io", "cp", "KEY")
	if err != nil {
		t.Fatalf("mergedPullSecretReplacement: %v", err)
	}
	if !changed {
		t.Fatal("expected changed=true")
	}
	var out struct {
		Metadata struct {
			ResourceVersion string `json:"resourceVersion"`
		} `json:"metadata"`
		Type string            `json:"type"`
		Data map[string]string `json:"data"`
	}
	if err := json.Unmarshal(replacement, &out); err != nil {
		t.Fatalf("decode replacement: %v", err)
	}
	if out.Metadata.ResourceVersion != "12345" {
		t.Fatalf("resourceVersion = %q, want 12345 (optimistic concurrency)", out.Metadata.ResourceVersion)
	}
	if out.Type != "kubernetes.io/dockerconfigjson" {
		t.Fatalf("type = %q", out.Type)
	}
	payload, err := base64.StdEncoding.DecodeString(out.Data[".dockerconfigjson"])
	if err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	auths := decodeAuths(t, payload)
	if _, ok := auths["quay.io"]; !ok {
		t.Fatal("existing quay.io entry must be preserved")
	}
	if _, ok := auths["cp.icr.io"]; !ok {
		t.Fatal("cp.icr.io entry must be merged in")
	}
}

func TestMergedPullSecretReplacementRejectsMissingPayload(t *testing.T) {
	live, err := json.Marshal(map[string]any{
		"metadata": map[string]any{"name": "pull-secret", "resourceVersion": "1"},
		"data":     map[string]string{},
	})
	if err != nil {
		t.Fatalf("marshal live secret: %v", err)
	}
	_, _, merr := mergedPullSecretReplacement(live, "cp.icr.io", "cp", "KEY")
	if merr == nil || !strings.Contains(merr.Error(), "no .dockerconfigjson data") {
		t.Fatalf("err = %v, want missing-payload error", merr)
	}
}
