package compilation_test

import (
	"context"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/desiredstate/yamlstream"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

func TestAReferenceAtAFieldItsObjectRefusesIsNotRefusedAgain(t *testing.T) {
	const keyField = "$.spec.remoteMachinesAccessKey.keyRef"
	for _, row := range []struct {
		name, code, field string
		want              []string
	}{
		{"invariant at the reference field", "api.invariant", keyField, []string{"api.invariant"}},
		{"empty code at the reference field", "", keyField, []string{"api.invariant"}},
		{"another code at the reference field", "api.value", keyField, []string{"api.reference", "api.value"}},
		{"invariant at another field", "api.invariant", "$.spec.domains", []string{"api.invariant", "api.reference"}},
	} {
		t.Run(row.name, func(t *testing.T) {
			rule := compilation.Rules{Validate: func(object api.Object, _ api.Catalog) []api.Issue {
				if object.Kind() != api.Environment {
					return nil
				}
				return []api.Issue{{Code: row.code, Field: row.field, Message: "forbidden here", Remediation: "remove it"}}
			}}
			compiler := compilation.NewCompiler(yamlstream.Parser{}, environmentSelection, rule)
			_, _, err := compiler.Compile(context.Background(), refusalSources(refusalRow{environment: environmentYAML + "  remoteMachinesAccessKey: {keyRef: missing}\n"}))
			codes := []string{}
			for _, d := range diagnostics.Of(err) {
				codes = append(codes, d.Code)
			}
			if len(codes) != len(row.want) {
				t.Fatalf("diagnostics = %+v, want codes %v", diagnostics.Of(err), row.want)
			}
			for _, want := range row.want {
				found := false
				for _, code := range codes {
					found = found || code == want
				}
				if !found {
					t.Fatalf("diagnostics = %+v, want codes %v", diagnostics.Of(err), row.want)
				}
			}
			if row.want[0] == "api.invariant" && len(row.want) == 1 {
				if d := diagnostics.Of(err)[0]; d.Field != keyField || d.Message != "forbidden here" {
					t.Fatalf("diagnostic = %+v", d)
				}
			}
		})
	}
}
