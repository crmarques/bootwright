package compilation_test

import (
	"context"
	"reflect"
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

func TestServiceReplaysOriginalDeclarationPathsAndExcludedStreams(t *testing.T) {
	const environment = `apiVersion: bootwright.io/v1alpha1
kind: Environment
metadata:
  name: frozen

spec:
  domains:
    base: example.test

  controller:
    machineRef: controller

  resources:
    - declarations

  defaults:
    Secret:
      type: opaque
      source:
        file:
          path: ../secrets/material
`
	const secret = `apiVersion: bootwright.io/v1alpha1
kind: Secret
metadata:
  name: material

spec: {}
`
	const excluded = `apiVersion: bootwright.io/v1alpha1
kind: Secret
metadata:
  name: excluded

spec:
  type: unsupported
`
	input := desiredstate.Sources{
		Roots: []string{"/synthetic/original"},
		Files: []desiredstate.SourceFile{
			desiredstate.NewSourceFile("/synthetic/original/config/environment.yaml", []byte(environment+controllerYAML)),
			desiredstate.NewSourceFile("/synthetic/original/config/declarations/material.yaml", []byte(secret)),
			desiredstate.NewSourceFile("/synthetic/original/config/excluded.yaml", []byte(excluded)),
		},
	}
	// No filesystem reader or payload capability exists on this service. These
	// original paths need not exist; only the frozen acquisition is compiled.
	service := compilation.New(nil, compiler(), frozenContextInputs{input})
	report, err := service.Validate(context.Background(), compilation.ValidateRequest{ContextName: "frozen"})
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	if report.Counts != (compilation.Counts{FilesSeen: 3, ObjectsDecoded: 3}) || !reflect.DeepEqual(report.ExcludedResourceFiles, []string{"excluded.yaml"}) || len(report.Diagnostics) != 1 || len(report.Advisories) != 1 {
		t.Fatalf("replay lost counts or exclusions: %#v", report)
	}
	if report.Diagnostics[0].Source.Path != "/synthetic/original/config/excluded.yaml" {
		t.Fatal("replay substituted the storage path for original provenance")
	}
	result, err := service.RenderEffective(context.Background(), compilation.EffectiveRequest{ContextName: "frozen"})
	if err != nil || result.Counts != report.Counts || len(result.Effective.Objects()) != 3 {
		t.Fatalf("effective replay = %#v, %v", result, err)
	}
	material, ok := result.Effective.Find(api.Secret, "material")
	if !ok || material.Spec().Get("source", "file", "path").Text() != "../secrets/material" {
		t.Fatal("rendering changed the inherited Secret declaration")
	}
	objects := result.Effective.Objects()
	objects[0] = api.Object{}
	if len(result.Effective.Objects()) != 3 || result.Effective.Objects()[0].Name() == "" {
		t.Fatal("effective result exposed mutable catalog storage")
	}
	if len(report.Diagnostics) != 1 || len(report.Advisories) != 1 {
		t.Fatal("rendering erased validation warnings from a prior result")
	}

	// Relative defaults use the recipient descriptor's directory. This path is
	// external relative to the Environment, but inside the input tree relative
	// to the recipient, where a secrets segment is required.
	badEnvironment := strings.Replace(environment, "../secrets/material", "../../operator-material/value", 1)
	input.Files[0] = desiredstate.NewSourceFile(input.Files[0].Path(), []byte(badEnvironment+controllerYAML))
	service = compilation.New(nil, compiler(), frozenContextInputs{input})
	failed, err := service.RenderEffective(context.Background(), compilation.EffectiveRequest{})
	if failed != nil || err == nil {
		t.Fatal("recipient-relative payload containment was lost")
	}
	found := false
	for _, diagnostic := range diagnostics.Of(err) {
		if diagnostic.Code == "api.invariant" && diagnostic.Field == "$.spec.source.file.path" {
			found = true
			line := strings.Count(badEnvironment[:strings.Index(badEnvironment, "          path:")], "\n") + 1
			if diagnostic.Source == nil || diagnostic.Source.Path != input.Files[0].Path() || diagnostic.Source.Line != line || !strings.Contains(diagnostic.Message, "Environment defaults") {
				t.Fatal("inherited diagnostic lost original field provenance", diagnostic)
			}
		}
	}
	if !found {
		t.Fatal("missing recipient path diagnostic", diagnostics.Of(err))
	}
}
