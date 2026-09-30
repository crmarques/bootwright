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
	if report.Counts != (compilation.Counts{FilesSeen: 3, ObjectsDecoded: 3}) || !reflect.DeepEqual(report.ExcludedResourceFiles, []string{"excluded.yaml"}) || len(report.Diagnostics) != 1 || report.Diagnostics[0].Code != "api.selection" || len(report.Advisories) != 0 {
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
	if bytes, _ := material.Spec().Get("source", "generated", "bytes").Int64(); !ok || bytes != 48 {
		t.Fatal("rendering changed the inherited Secret declaration")
	}
	objects := result.Effective.Objects()
	objects[0] = api.Object{}
	if len(result.Effective.Objects()) != 3 || result.Effective.Objects()[0].Name() == "" {
		t.Fatal("effective result exposed mutable catalog storage")
	}
	if len(report.Diagnostics) != 1 {
		t.Fatal("rendering erased validation warnings from a prior result")
	}

	// A retired file source an Environment default supplies is refused for
	// each recipient, at the default that supplied it, naming the command
	// that loads context custody and the default to correct.
	badEnvironment := strings.Replace(environment, "generated:\n          bytes: 48", "file:\n          path: ../secrets/material", 1)
	input.Files[0] = desiredstate.NewSourceFile(input.Files[0].Path(), []byte(badEnvironment+controllerYAML))
	service = compilation.New(nil, compiler(), frozenContextInputs{input})
	failed, err := service.RenderEffective(context.Background(), compilation.EffectiveRequest{})
	if failed != nil || err == nil {
		t.Fatal("an inherited file source compiled")
	}
	found := false
	for _, diagnostic := range diagnostics.Of(err) {
		if diagnostic.Code == "api.field" && diagnostic.Field == "$.spec.source.file" {
			found = true
			line := strings.Count(badEnvironment[:strings.Index(badEnvironment, "          path:")], "\n") + 1
			if diagnostic.Source == nil || diagnostic.Source.Path != input.Files[0].Path() || diagnostic.Source.Line != line || !strings.Contains(diagnostic.Message, "Environment defaults") {
				t.Fatal("inherited diagnostic lost original field provenance", diagnostic)
			}
			if !strings.Contains(diagnostic.Message, "bootwright secret set") || !strings.Contains(diagnostic.Remediation, "Environment kind default") {
				t.Fatal("inherited refusal lost its remedy", diagnostic)
			}
		}
	}
	if !found {
		t.Fatal("missing inherited file source refusal", diagnostics.Of(err))
	}
}
