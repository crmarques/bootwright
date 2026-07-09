package desiredstate

import (
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"go.yaml.in/yaml/v3"
)

func TestLevenshtein(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"", "", 0},
		{"abc", "abc", 0},
		{"baseDomainn", "baseDomain", 1},
		{"kitten", "sitting", 3},
		{"role", "artifacts", 8},
	}
	for _, tc := range cases {
		if got := levenshtein(tc.a, tc.b); got != tc.want {
			t.Errorf("levenshtein(%q,%q)=%d want %d", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestNearestFieldName(t *testing.T) {
	fields := []string{"baseDomain", "resources", "safety"}
	if got := nearestFieldName("baseDomainn", fields); got != "baseDomain" {
		t.Errorf("close typo: got %q want baseDomain", got)
	}
	if got := nearestFieldName("completelyUnrelatedKey", fields); got != "" {
		t.Errorf("distant key: got %q want empty", got)
	}
}

func TestRewriteKnownFieldErrorAppendsSuggestion(t *testing.T) {
	var node yaml.Node
	doc := "apiVersion: bootwright.io/v1alpha1\nkind: Environment\nmetadata:\n  name: env\nspec:\n  baseDomainn: example.com\n"
	if err := yaml.Unmarshal([]byte(doc), &node); err != nil {
		t.Fatalf("unmarshal node: %v", err)
	}
	var env v1alpha1.Environment
	err := decodeKnown(node, &env)
	if err == nil {
		t.Fatal("expected unknown-field rejection, got nil")
	}
	msg := err.Error()
	if !strings.Contains(msg, "field baseDomainn not found") {
		t.Errorf("reject wording changed, breaking the pinned contract: %q", msg)
	}
	if !strings.Contains(msg, `did you mean "baseDomain"?`) {
		t.Errorf("expected a did-you-mean suggestion, got %q", msg)
	}
}

func TestRewriteKnownFieldErrorNoSuggestionForDistantField(t *testing.T) {
	var node yaml.Node
	doc := "apiVersion: bootwright.io/v1alpha1\nkind: Environment\nmetadata:\n  name: env\nspec:\n  zzzzzzzzzzTotallyUnrelated: x\n"
	if err := yaml.Unmarshal([]byte(doc), &node); err != nil {
		t.Fatalf("unmarshal node: %v", err)
	}
	var env v1alpha1.Environment
	err := decodeKnown(node, &env)
	if err == nil {
		t.Fatal("expected unknown-field rejection, got nil")
	}
	if strings.Contains(err.Error(), "did you mean") {
		t.Errorf("distant field should get no suggestion, got %q", err.Error())
	}
}
