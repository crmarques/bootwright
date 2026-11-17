package compilation_test

import (
	"context"
	"strconv"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/desiredstate/yamlstream"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/environment"
	"github.com/crmarques/bootwright/internal/secrets"
)

func BenchmarkCompileWithRulesThatIgnoreMostKinds(b *testing.B) {
	unchanged := func(o api.Object, _ api.Catalog) (api.Object, []api.Issue) { return o, nil }
	rules := []compilation.Rules{{Normalize: environment.Normalize, Validate: testEnvironmentValidate}, {Normalize: secrets.Normalize, ValidateAuthored: secrets.ValidateAuthored, Validate: secrets.Validate}}
	for range 11 {
		rules = append(rules, compilation.Rules{Normalize: unchanged})
	}
	compiler := compilation.NewCompiler(yamlstream.Parser{}, nil, rules...)
	var content strings.Builder
	content.WriteString(environmentYAML)
	for i := range 250 {
		content.WriteString("---\napiVersion: bootwright.io/v1alpha1\nkind: Secret\nmetadata:\n  name: material-" + strconv.Itoa(i) + "\nspec:\n  type: token\n  source:\n    generated:\n      bytes: 32\n")
	}
	input := sources(content.String())
	b.ResetTimer()
	for b.Loop() {
		if _, _, err := compiler.Compile(context.Background(), input); err != nil {
			b.Fatal(diagnostics.Of(err))
		}
	}
}
