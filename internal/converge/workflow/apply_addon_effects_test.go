package workflow

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
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

func TestMergeGlobalPullSecretCredentialRetriesConflictWithLatestResourceVersion(t *testing.T) {
	runner := &conflictPullSecretRunner{
		gets: [][]byte{
			livePullSecretJSON(t, "1", dockerConfig(t, map[string]map[string]string{"quay.io": {"auth": "cXVheQ=="}})),
			livePullSecretJSON(t, "2", dockerConfig(t, map[string]map[string]string{"quay.io": {"auth": "cXVheQ=="}})),
		},
		replaceErrs: []error{errors.New(`Error from server (Conflict): Operation cannot be fulfilled on secrets "pull-secret": the object has been modified`)},
	}
	changed, err := mergeGlobalPullSecretCredential(context.Background(), runner, "/tmp/kubeconfig", "cp.icr.io", "cp", "KEY")
	if err != nil {
		t.Fatalf("mergeGlobalPullSecretCredential: %v", err)
	}
	if !changed {
		t.Fatal("expected changed=true")
	}
	if runner.getCalls != 2 || runner.replaceCalls != 2 {
		t.Fatalf("calls get=%d replace=%d, want 2/2", runner.getCalls, runner.replaceCalls)
	}
	var replacement struct {
		Metadata struct {
			ResourceVersion string `json:"resourceVersion"`
		} `json:"metadata"`
	}
	if err := json.Unmarshal(runner.replacements[1], &replacement); err != nil {
		t.Fatalf("decode second replacement: %v", err)
	}
	if replacement.Metadata.ResourceVersion != "2" {
		t.Fatalf("second replacement resourceVersion = %q, want 2", replacement.Metadata.ResourceVersion)
	}
}

func livePullSecretJSON(t *testing.T, resourceVersion string, config []byte) []byte {
	t.Helper()
	live, err := json.Marshal(map[string]any{
		"metadata": map[string]any{"name": "pull-secret", "namespace": "openshift-config", "resourceVersion": resourceVersion},
		"data":     map[string]string{".dockerconfigjson": base64.StdEncoding.EncodeToString(config)},
	})
	if err != nil {
		t.Fatalf("marshal live secret: %v", err)
	}
	return live
}

type conflictPullSecretRunner struct {
	gets         [][]byte
	replaceErrs  []error
	replacements [][]byte
	getCalls     int
	replaceCalls int
}

func (r *conflictPullSecretRunner) Run(_ context.Context, _ string, args []string, input []byte) ([]byte, error) {
	if len(args) == 0 {
		return nil, nil
	}
	switch args[0] {
	case "get":
		if r.getCalls >= len(r.gets) {
			return nil, errors.New("unexpected get")
		}
		out := r.gets[r.getCalls]
		r.getCalls++
		return out, nil
	case "replace":
		r.replacements = append(r.replacements, append([]byte(nil), input...))
		var err error
		if r.replaceCalls < len(r.replaceErrs) {
			err = r.replaceErrs[r.replaceCalls]
		}
		r.replaceCalls++
		return nil, err
	default:
		return nil, errors.New("unexpected oc command")
	}
}

func TestPullSecretCredentialTrimsTrailingNewline(t *testing.T) {
	got, err := pullSecretCredential("ibm-entitlement-key", "cp.icr.io", "cp", []byte("ENTITLEMENT\n"))
	if err != nil {
		t.Fatalf("pullSecretCredential: %v", err)
	}
	if got != "ENTITLEMENT" {
		t.Fatalf("credential = %q, want trimmed ENTITLEMENT", got)
	}
	merged, _, err := mergedDockerConfigAuth(dockerConfig(t, map[string]map[string]string{}), "cp.icr.io", "cp", got)
	if err != nil {
		t.Fatalf("mergedDockerConfigAuth: %v", err)
	}
	want := base64.StdEncoding.EncodeToString([]byte("cp:ENTITLEMENT"))
	if decodeAuths(t, merged)["cp.icr.io"]["auth"] != want {
		t.Fatal("a trailing newline in the stored key must not corrupt the cp.icr.io auth entry")
	}
}

func TestPullSecretCredentialRejectsCorruptValues(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{"empty", "   \n", "is empty"},
		{"interior newline", "AB\nCD", "whitespace inside"},
		{"interior space", "AB CD", "whitespace inside"},
		{"username prefix", "cp:ENTITLEMENT", "prepended automatically"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := pullSecretCredential("ibm-entitlement-key", "cp.icr.io", "cp", []byte(tc.raw))
			if err == nil {
				t.Fatalf("expected error for %s", tc.name)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want contains %q", err, tc.want)
			}
		})
	}
}
