package contexts_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/workspace/contexts"
)

func TestStandaloneContextConfiguration(t *testing.T) {
	want := contexts.DefaultConfiguration("test")
	for _, document := range []string{
		string(want.Canonical()),
		"apiVersion: bootwright.io/v1alpha1\nkind: Context\nmetadata: {name: test}\n",
		"{apiVersion: bootwright.io/v1alpha1, kind: Context, metadata: {name: test}, spec: {secretStore: {}}}",
	} {
		got, err := contexts.ParseConfiguration("test", []byte(document))
		if err != nil || got != want || !bytes.Equal(got.Canonical(), want.Canonical()) {
			t.Fatalf("configuration did not normalize: %+v %v", got, err)
		}
	}
	custom := strings.ReplaceAll(string(want.Canonical()), "local-keyring", "test-session")
	got, err := contexts.ParseConfiguration("test", []byte(custom))
	if err != nil || got.SecretStore.Type != "test-session" {
		t.Fatal("schema selected a concrete implementation instead of admitting its identifier", got, err)
	}
}

func TestCanonicalContextPreservesNamesThatResembleYAMLScalars(t *testing.T) {
	for _, name := range []string{"true", "false", "null", "123", "0x12", "2001-12-15", "test"} {
		t.Run(name, func(t *testing.T) {
			want := contexts.DefaultConfiguration(name)
			want.SecretStore.Type = name
			got, err := contexts.ParseConfiguration(name, want.Canonical())
			if err != nil || got != want {
				t.Fatal("canonical YAML changed a valid string identifier", got, err)
			}
		})
	}
}

func TestContextConfigurationRefusesAmbiguousOrUnsupportedInput(t *testing.T) {
	canonical := string(contexts.DefaultConfiguration("test").Canonical())
	for name, document := range map[string]string{
		"empty": "", "multiple": canonical + "---\n" + canonical,
		"environment":  strings.Replace(canonical, "kind: Context", "kind: Environment", 1),
		"version":      strings.Replace(canonical, "v1alpha1", "v2", 1),
		"wrong-name":   strings.Replace(canonical, "name: \"test\"", "name: other", 1),
		"unknown":      canonical + "unexpected: true\n",
		"duplicate":    canonical + "kind: Context\n",
		"null-spec":    strings.Split(canonical, "\nspec:")[0] + "\nspec: null\n",
		"null-store":   strings.Split(canonical, "  secretStore:")[0] + "  secretStore: null\n",
		"null-type":    strings.Replace(canonical, "type: \"local-keyring\"", "type: null", 1),
		"parameters":   canonical + "    parameters: {}\n",
		"path-type":    strings.Replace(canonical, "local-keyring", "../../store", 1),
		"alias":        strings.Replace(canonical, "name: \"test\"", "name: &n test", 1),
		"tag":          strings.Replace(canonical, "name: \"test\"", "name: !!str test", 1),
		"invalid-utf8": canonical + "#\xff\n",
		"too-large":    canonical + strings.Repeat("#", contexts.MaxConfigurationBytes),
		"nested":       canonical + "unexpected: " + strings.Repeat("[", 32) + "x" + strings.Repeat("]", 32),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := contexts.ParseConfiguration("test", []byte(document)); err == nil {
				t.Fatal("invalid configuration admitted")
			} else {
				requireCode(t, err, "context.configuration")
			}
		})
	}
}

func FuzzContextConfiguration(f *testing.F) {
	f.Add(contexts.DefaultConfiguration("test").Canonical())
	f.Add([]byte("apiVersion: bootwright.io/v1alpha1\nkind: Context\nmetadata: {name: test}\n"))
	f.Fuzz(func(t *testing.T, data []byte) {
		got, err := contexts.ParseConfiguration("test", data)
		if err != nil {
			return
		}
		again, err := contexts.ParseConfiguration("test", got.Canonical())
		if err != nil || again != got {
			t.Fatal("admitted configuration is not stable", got, again, err)
		}
	})
}
