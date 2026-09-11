package compilation_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/desiredstate"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/desiredstate/yamlstream"
	"github.com/crmarques/bootwright/internal/environment"
	"github.com/crmarques/bootwright/internal/secrets"
)

const environmentYAML = "apiVersion: bootwright.io/v1alpha1\nkind: Environment\nmetadata:\n  name: synthetic\nspec:\n  domains:\n    base: example.test\n  controller: {machineRef: controller}\n"

const controllerYAML = "\n---\napiVersion: bootwright.io/v1alpha1\nkind: Machine\nmetadata: {name: controller}\nspec:\n  capabilities: [container-runtime]\n  os: {provided: true}\n  access: {local: true}\n"

func compiler() compilation.Compiler {
	return compilation.NewCompiler(yamlstream.Parser{}, nil, compilation.Rules{Normalize: environment.Normalize, Validate: environment.Validate}, compilation.Rules{Normalize: secrets.Normalize, ValidateAuthored: secrets.ValidateAuthored, Validate: secrets.Validate})
}
func sources(content string) desiredstate.Sources {
	return desiredstate.Sources{Files: []desiredstate.SourceFile{desiredstate.NewSourceFile("/synthetic/environment.yaml", []byte(content+controllerYAML))}, Roots: []string{"/synthetic"}}
}

func TestCompilerPreservesAuthoredAndDerivedValues(t *testing.T) {
	input := sources(environmentYAML)
	before := input.Files[0].Bytes()
	state, report, err := compiler().Compile(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if report.Counts != (compilation.Counts{FilesSeen: 1, ObjectsDecoded: 2}) || report.Advisories == nil || report.ExcludedResourceFiles == nil {
		t.Fatalf("invalid report: %#v", report)
	}
	if state.Authored().Objects()[0].Spec().Has("domains", "machines") {
		t.Fatal("authored state mutated")
	}
	if state.Effective().Objects()[0].Spec().Get("domains", "machines").Text() != "example.test" {
		t.Fatal("domain fallback missing")
	}
	if !reflect.DeepEqual(before, input.Files[0].Bytes()) {
		t.Fatal("source bytes mutated")
	}
}

func TestCompilerRejectsStrictSyntaxAndScalarViolations(t *testing.T) {
	for _, test := range []struct{ name, body, code string }{
		{"duplicate", "type: opaque\n  type: token", "yaml.duplicate-key"},
		{"unknown", "type: opaque\n  unexpected: value", "api.field"},
		{"quoted integer", "type: token\n  source:\n    generated:\n      bytes: '32'", "api.type"},
		{"integer separator", "type: token\n  source:\n    generated:\n      bytes: 3_2", "api.type"},
		{"null", "type: opaque\n  source: null", "api.type"},
		{"alias", "type: opaque\n  source: &source {}", "yaml.alias"},
		{"wrong file arm", "type: token\n  source:\n    file:\n      cert: secrets/cert.pem", "api.invariant"},
		{"payload boundary", "type: opaque\n  source:\n    file:\n      path: payload.yaml", "api.invariant"},
	} {
		t.Run(test.name, func(t *testing.T) {
			content := environmentYAML + "---\napiVersion: bootwright.io/v1alpha1\nkind: Secret\nmetadata:\n  name: material\nspec:\n  " + test.body + "\n"
			state, report, err := compiler().Compile(context.Background(), sources(content))
			if state != nil || report != nil || err == nil {
				t.Fatalf("invalid success: %#v %#v %v", state, report, err)
			}
			found := false
			for _, d := range desiredstate.DiagnosticsOf(err) {
				if d.Code == test.code {
					found = true
				}
			}
			if !found {
				t.Fatalf("wanted %s: %#v", test.code, desiredstate.DiagnosticsOf(err))
			}
		})
	}
}

func TestCompilerDefaultPrecedenceAndRecipientPaths(t *testing.T) {
	env := environmentYAML + "  defaults:\n    Secret:\n      type: token\n      source:\n        generated:\n          bytes: 64\n"
	secret := "apiVersion: bootwright.io/v1alpha1\nkind: Secret\nmetadata:\n  name: material\nspec:\n  source: {}\n"
	input := desiredstate.Sources{Files: []desiredstate.SourceFile{desiredstate.NewSourceFile("/synthetic/environment.yaml", []byte(env+controllerYAML)), desiredstate.NewSourceFile("/synthetic/nested/secret.yaml", []byte(secret))}, Roots: []string{"/synthetic"}}
	state, _, err := compiler().Compile(context.Background(), input)
	if err != nil {
		t.Fatal(desiredstate.DiagnosticsOf(err))
	}
	object, _ := state.Effective().Find(api.Secret, "material")
	if !object.Spec().Has("source", "contextStore") || object.Spec().Has("source", "generated") {
		t.Fatal("explicit empty source lost suppression")
	}
	input.Files[1] = desiredstate.NewSourceFile("/synthetic/nested/secret.yaml", []byte(strings.Replace(secret, "source: {}", "source:\n    file:\n      path: secrets/material", 1)))
	if _, _, err := compiler().Compile(context.Background(), input); err != nil {
		t.Fatal(desiredstate.DiagnosticsOf(err))
	}
}

func TestCompilerResourceSelectionAndCancellation(t *testing.T) {
	env := environmentYAML + "  resources: [environment.yaml]\n"
	input := sources(env)
	input.Files = append(input.Files, desiredstate.NewSourceFile("/synthetic/excluded.yaml", []byte("apiVersion: bootwright.io/v1alpha1\nkind: Secret\nmetadata: {name: excluded}\nspec: {type: unsupported}\n")))
	state, report, err := compiler().Compile(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Effective().Objects()) != 2 || report.Counts.FilesSeen != 2 || report.Counts.ObjectsDecoded != 2 || len(report.Advisories) != 1 || !reflect.DeepEqual(report.ExcludedResourceFiles, []string{"excluded.yaml"}) {
		t.Fatalf("unexpected report: %#v", report)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	state, report, err = compiler().Compile(ctx, input)
	if state != nil || report != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation: %v", err)
	}
}
