package contexts_test

import (
	"bytes"
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/desiredstate"
	"github.com/crmarques/bootwright/internal/diagnostics"
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

// A Context file refusal names the offending field and its line, what the
// document declares instead of a Context, and the file itself, also when no
// implementation serves its secret-store type; a file the reader refuses,
// such as a directory, gains the remedy that tells -f from --input-dir.
func TestContextFileRefusalsNameTheFieldTheLineAndTheFile(t *testing.T) {
	const remedy = "pass desired state with --input-dir <dir>; -f takes one standalone Context file"
	canonical := string(contexts.DefaultConfiguration("test").Canonical())
	for name, row := range map[string]struct {
		document, message, remediation string
	}{
		"unknown": {document: canonical + "    parameters: {}\n",
			message: "Context configuration field spec.secretStore.parameters at line 9 is unknown"},
		"unknown-top-level": {document: canonical + "unexpected: true\n",
			message: "Context configuration field unexpected at line 9 is unknown"},
		"duplicate": {document: canonical + "kind: Context\n",
			message: "Context configuration field kind at line 9 is duplicated"},
		"duplicate-metadata": {document: strings.Replace(canonical, "  name: \"test\"\n", "  name: \"test\"\n  name: \"test\"\n", 1),
			message: "Context configuration field metadata.name at line 5 is duplicated"},
		"environment": {document: strings.Replace(canonical, "kind: Context", "kind: Environment", 1),
			message:     "the file declares apiVersion bootwright.io/v1alpha1 and kind Environment; a Context file declares apiVersion bootwright.io/v1alpha1 and kind Context",
			remediation: remedy},
		"foreign-kind-with-its-own-fields": {document: "apiVersion: v1\nkind: ConfigMap\nmetadata: {name: test}\ndata: {key: value}\n",
			message:     "the file declares apiVersion v1 and kind ConfigMap; a Context file declares apiVersion bootwright.io/v1alpha1 and kind Context",
			remediation: remedy},
		"no-kind": {document: strings.Replace(canonical, "kind: Context\n", "", 1),
			message:     "the file declares apiVersion bootwright.io/v1alpha1 and no kind; a Context file declares apiVersion bootwright.io/v1alpha1 and kind Context",
			remediation: remedy},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := contexts.ParseConfiguration("test", []byte(row.document))
			reported := diagnostics.Of(err)
			if len(reported) != 1 || reported[0].Code != "context.configuration" || reported[0].Message != row.message || reported[0].Remediation != row.remediation {
				t.Fatalf("got %#v, want %q with remedy %q", reported, row.message, row.remediation)
			}
			r := newRepository(t)
			r.configInput = []byte(strings.ReplaceAll(row.document, `"test"`, "example"))
			_, err = service(t, r, desiredstate.Sources{}).Init(context.Background(), contexts.InitRequest{Name: "example", ConfigurationFile: "/synthetic/context.yaml"})
			reported = diagnostics.Of(err)
			if len(reported) != 1 || reported[0].Source == nil || reported[0].Source.Path != "/synthetic/context.yaml" || slices.Contains(r.calls, "transaction") {
				t.Fatalf("the refusal does not name the file: %#v", reported)
			}
		})
	}
	t.Run("unavailable-type", func(t *testing.T) {
		r := newRepository(t)
		r.configInput = []byte(strings.Replace(string(contexts.DefaultConfiguration("example").Canonical()), `"local-keyring"`, "vault", 1))
		_, err := service(t, r, desiredstate.Sources{}).Init(context.Background(), contexts.InitRequest{Name: "example", ConfigurationFile: "/synthetic/context.yaml"})
		reported := diagnostics.Of(err)
		if len(reported) != 1 || reported[0].Message != "unavailable implementation" || reported[0].Source == nil || reported[0].Source.Path != "/synthetic/context.yaml" || slices.Contains(r.calls, "transaction") {
			t.Fatalf("an unavailable secret-store type does not name the file: %#v", reported)
		}
	})
	t.Run("directory", func(t *testing.T) {
		r := newRepository(t)
		r.failure = "read-config"
		r.failureErr = diagnostics.NewFailure("input.read", "input source must be a regular file without symbolic links", "/synthetic/input")
		_, err := service(t, r, desiredstate.Sources{}).Init(context.Background(), contexts.InitRequest{Name: "example", ConfigurationFile: "/synthetic/input"})
		reported := diagnostics.Of(err)
		if len(reported) != 1 || reported[0].Code != "input.read" || reported[0].Source == nil || reported[0].Source.Path != "/synthetic/input" || reported[0].Remediation != remedy {
			t.Fatalf("a directory passed to -f lacks its remedy: %#v", reported)
		}
	})
}
