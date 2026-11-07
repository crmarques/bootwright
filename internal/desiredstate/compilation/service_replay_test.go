package compilation_test

import (
	"context"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/desiredstate"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

type frozenContextInputs struct{ sources desiredstate.Sources }

func (f frozenContextInputs) ReadInputs(context.Context, string) (desiredstate.Sources, error) {
	return f.sources, nil
}

func TestServiceReplaysOriginalDeclarationPaths(t *testing.T) {
	const environment = `apiVersion: bootwright.io/v1alpha1
kind: Environment
metadata:
  name: frozen

spec:
  domains:
    base: example.test

  controller:
    machineRef: controller

  defaults:
    Secret:
      type: token
      source:
        generated:
          bytes: 48
`
	const secret = `apiVersion: bootwright.io/v1alpha1
kind: Secret
metadata:
  name: material

spec: {}
`
	input := desiredstate.Sources{
		Roots: []string{"/synthetic/original"},
		Files: []desiredstate.SourceFile{
			desiredstate.NewSourceFile("/synthetic/original/config/environment.yaml", []byte(environment+controllerYAML)),
			desiredstate.NewSourceFile("/synthetic/original/config/declarations/material.yaml", []byte(secret)),
		},
	}
	// No filesystem reader or payload capability exists on this service. These
	// original paths need not exist; only the frozen acquisition is compiled.
	service := compilation.New(nil, compiler(), frozenContextInputs{input})
	report, err := service.Validate(context.Background(), compilation.ValidateRequest{ContextName: "frozen"})
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	if report.Counts != (compilation.Counts{FilesSeen: 2, ObjectsDecoded: 3}) || len(report.Diagnostics) != 0 || len(report.Advisories) != 0 {
		t.Fatalf("replay lost counts: %#v", report)
	}
	result, err := service.RenderEffective(context.Background(), compilation.EffectiveRequest{ContextName: "frozen"})
	if err != nil || result.Counts != report.Counts || len(result.Effective.Objects()) != 3 {
		t.Fatalf("effective replay = %#v, %v", result, err)
	}
	material, ok := result.Effective.Find(api.Secret, "material")
	if bytes, _ := material.Spec().Get("source", "generated", "bytes").Int64(); !ok || bytes != 48 {
		t.Fatal("rendering changed the inherited Secret declaration")
	}
	objects := result.Effective.Objects()
	objects[0] = api.Object{}
	if len(result.Effective.Objects()) != 3 || result.Effective.Objects()[0].Name() == "" {
		t.Fatal("effective result exposed mutable catalog storage")
	}

	// A field an Environment default supplies is refused at the default that
	// supplied it, with the default's own source position.
	badEnvironment := strings.Replace(environment, "generated:\n          bytes: 48", "file:\n          path: ../secrets/material", 1)
	input.Files[0] = desiredstate.NewSourceFile(input.Files[0].Path(), []byte(badEnvironment+controllerYAML))
	service = compilation.New(nil, compiler(), frozenContextInputs{input})
	failed, err := service.RenderEffective(context.Background(), compilation.EffectiveRequest{})
	if failed != nil || err == nil {
		t.Fatal("an inherited unknown field compiled")
	}
	found := false
	for _, diagnostic := range diagnostics.Of(err) {
		if diagnostic.Code == "api.field" && diagnostic.Field == "$.spec.defaults.Secret.source.file" {
			found = true
			line := strings.Count(badEnvironment[:strings.Index(badEnvironment, "        file:")], "\n") + 1
			if diagnostic.Source == nil || diagnostic.Source.Path != input.Files[0].Path() || diagnostic.Source.Line != line {
				t.Fatal("inherited diagnostic lost original field provenance", diagnostic)
			}
		}
	}
	if !found {
		t.Fatal("missing inherited unknown field refusal", diagnostics.Of(err))
	}
}
