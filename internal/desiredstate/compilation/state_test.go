package compilation_test

import (
	"context"
	"reflect"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/desiredstate"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
)

type alternativeCompiler struct {
	compile func(context.Context, desiredstate.Sources) (*compilation.State, *compilation.Report, error)
}

func (c alternativeCompiler) Compile(ctx context.Context, sources desiredstate.Sources) (*compilation.State, *compilation.Report, error) {
	return c.compile(ctx, sources)
}

func TestServiceAcceptsIndependentlyConstructedCompilerState(t *testing.T) {
	environment := api.NewObject(api.Environment, "sample", api.Value{}, api.MapValue())
	effective := environment.WithSpec(api.MapValue(api.FieldValue{Name: "domains", Value: api.MapValue(api.FieldValue{Name: "base", Value: api.StringValue("example.test")})}))
	origin := desiredstate.SourceLocation{Path: "/synthetic/environment.yaml", Document: 1}
	origins := map[string]desiredstate.SourceLocation{environment.Identity(): origin}
	counts := compilation.Counts{FilesSeen: 1, ObjectsDecoded: 1}
	sources := desiredstate.Sources{Files: []desiredstate.SourceFile{desiredstate.NewSourceFile(origin.Path, []byte("alternate input"))}}
	ctx := context.Background()
	called := false
	compiler := alternativeCompiler{compile: func(got context.Context, input desiredstate.Sources) (*compilation.State, *compilation.Report, error) {
		called = true
		if got != ctx || !reflect.DeepEqual(input, sources) {
			t.Fatal("compiler did not receive the invocation context and frozen input")
		}
		return compilation.NewState(api.NewCatalog([]api.Object{environment}), api.NewCatalog([]api.Object{effective}), origins), &compilation.Report{Counts: counts}, nil
	}}
	service := compilation.New(nil, compiler, frozenContextInputs{sources})
	result, err := service.RenderEffective(ctx, compilation.EffectiveRequest{ContextName: "sample"})
	if err != nil {
		t.Fatal(err)
	}
	if !called || result.Counts != counts || !reflect.DeepEqual(result.Effective.Objects(), []api.Object{effective}) {
		t.Fatalf("alternate compilation result = %#v", result)
	}
}

func TestConstructedStateOwnsCanonicalCatalogsAndProvenance(t *testing.T) {
	environment := api.NewObject(api.Environment, "sample", api.Value{}, api.MapValue())
	secret := api.NewObject(api.Secret, "material", api.Value{}, api.MapValue())
	authored := api.NewCatalog([]api.Object{secret, environment})
	effectiveEnvironment := environment.WithSpec(api.MapValue(api.FieldValue{Name: "domains", Value: api.MapValue(api.FieldValue{Name: "base", Value: api.StringValue("example.test")})}))
	effective := api.NewCatalog([]api.Object{secret, effectiveEnvironment})
	origin := desiredstate.SourceLocation{Path: "/synthetic/environment.yaml", Document: 1}
	origins := map[string]desiredstate.SourceLocation{environment.Identity(): origin}
	state := compilation.NewState(authored, effective, origins)

	origins[environment.Identity()] = desiredstate.SourceLocation{Path: "/synthetic/changed.yaml"}
	origins[secret.Identity()] = desiredstate.SourceLocation{Path: "/synthetic/secret.yaml"}
	state.Authored().Objects()[0] = api.Object{}
	state.Effective().Objects()[0] = api.Object{}
	if got, ok := state.Origin(environment.Identity()); !ok || got != origin {
		t.Fatalf("state retained mutable provenance: %#v, %v", got, ok)
	}
	if _, ok := state.Origin(secret.Identity()); ok {
		t.Fatal("state acquired provenance added after construction")
	}
	if !reflect.DeepEqual(state.Authored().Objects(), []api.Object{environment, secret}) || !reflect.DeepEqual(state.Effective().Objects(), []api.Object{effectiveEnvironment, secret}) {
		t.Fatal("state did not preserve separate immutable canonical catalogs")
	}
	if !reflect.DeepEqual(authored.Objects(), []api.Object{secret, environment}) || !reflect.DeepEqual(effective.Objects(), []api.Object{secret, effectiveEnvironment}) {
		t.Fatal("state construction reordered its caller's catalogs")
	}
}
