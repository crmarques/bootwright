package hooks

import (
	"fmt"
	"testing"
)

func TestParseToken(t *testing.T) {
	cases := []struct {
		in       string
		wantKind string
		wantArg  string
		wantOK   bool
	}{
		{"{{ cluster }}", "cluster", "", true},
		{"{{ output externalDetails }}", "output", "externalDetails", true},
		{"{{ input external-storage }}", "input", "external-storage", true},
		{"{{ exportDetails external-storage }}", "exportDetails", "external-storage", true},
		{"{{secret foo}}", "secret", "foo", true},
		{"literal", "", "", false},
		{"prefix {{ output x }}", "", "", false},
		{"{{ output x }} suffix", "", "", false},
		{"{{ }}", "", "", false},
	}
	for _, tc := range cases {
		got, ok := ParseToken(tc.in)
		if ok != tc.wantOK {
			t.Errorf("ParseToken(%q) ok=%v want %v", tc.in, ok, tc.wantOK)
			continue
		}
		if ok && (got.Kind != tc.wantKind || got.Arg != tc.wantArg) {
			t.Errorf("ParseToken(%q) = %+v want kind=%q arg=%q", tc.in, got, tc.wantKind, tc.wantArg)
		}
	}
}

func TestStrayTokenScalars(t *testing.T) {
	raw := []byte(`apiVersion: v1
kind: Secret
metadata:
  name: "prefix-{{ output foo }}-suffix"
  namespace: openshift-storage
stringData:
  clean: "{{ output foo }}"
  literal: plain-text
`)
	stray, err := StrayTokenScalars(raw)
	if err != nil {
		t.Fatalf("StrayTokenScalars: %v", err)
	}
	if len(stray) != 1 || stray[0] != "prefix-{{ output foo }}-suffix" {
		t.Fatalf("StrayTokenScalars = %v, want exactly the embedded-token scalar flagged", stray)
	}
}

func TestRenderManifestWholeScalarReplacement(t *testing.T) {
	raw := []byte(`apiVersion: v1
kind: Secret
metadata:
  name: rook-ceph-external-cluster-details
  annotations:
    bootwright.io/container-cluster-ref: "{{ cluster }}"
stringData:
  external_cluster_details: "{{ output externalDetails }}"
`)
	payload := "[{\"name\":\"rook-ceph-mon-endpoints\"},\n{\"name\":\"rook-ceph-mon\"}]"
	object, err := RenderManifest(raw, func(token Token) (string, error) {
		switch {
		case token.Kind == TokenCluster:
			return "metal-ocp", nil
		case token.Kind == TokenOutput && token.Arg == "externalDetails":
			return payload, nil
		}
		return "", fmt.Errorf("unexpected token %+v", token)
	})
	if err != nil {
		t.Fatalf("RenderManifest: %v", err)
	}
	stringData := object["stringData"].(map[string]any)
	if got := stringData["external_cluster_details"]; got != payload {
		t.Errorf("payload = %q want %q", got, payload)
	}
	annotations := object["metadata"].(map[string]any)["annotations"].(map[string]any)
	if got := annotations["bootwright.io/container-cluster-ref"]; got != "metal-ocp" {
		t.Errorf("cluster annotation = %q want metal-ocp", got)
	}
}

func TestRenderManifestUnknownTokenErrors(t *testing.T) {
	raw := []byte("kind: X\nvalue: \"{{ output missing }}\"\n")
	_, err := RenderManifest(raw, func(Token) (string, error) {
		return "", fmt.Errorf("unknown output")
	})
	if err == nil {
		t.Fatal("expected error for unresolved token")
	}
}

func TestExtractTokens(t *testing.T) {
	raw := []byte(`kind: Secret
metadata:
  name: "{{ cluster }}"
data:
  a: "{{ output one }}"
  b: literal
  c: "{{ secret two }}"
`)
	tokens, err := ExtractTokens(raw)
	if err != nil {
		t.Fatalf("ExtractTokens: %v", err)
	}
	if len(tokens) != 3 {
		t.Fatalf("got %d tokens want 3: %+v", len(tokens), tokens)
	}
}
