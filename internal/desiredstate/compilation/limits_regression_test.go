package compilation_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/desiredstate"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/desiredstate/yamlstream"
)

func nestedNativeValue(levels int) api.Value {
	value := api.StringValue("leaf")
	for range levels {
		value = api.MapValue(api.FieldValue{Name: "child", Value: value})
	}
	return value
}

func TestExpandedDepthCeilingIsInclusiveAndStopsLaterNormalizers(t *testing.T) {
	input := sources(environmentYAML + "---\napiVersion: bootwright.io/v1alpha1\nkind: NetworkConfig\nmetadata: {name: network}\nspec:\n  machineNetwork: [{cidr: 192.0.2.0/24}]\n  nmstate: {}\n")
	for _, depth := range []int{desiredstate.MaxDepth, desiredstate.MaxDepth + 1} {
		t.Run(fmt.Sprint(depth), func(t *testing.T) {
			later := 0
			compiler := compilation.NewCompiler(yamlstream.Parser{}, nil,
				compilation.Rules{Normalize: func(object api.Object, _ api.Catalog) (api.Object, []api.Issue) {
					if object.Kind() == api.NetworkConfig {
						// Document=0, envelope=1, spec=2, nmstate=3.
						return object.WithSpec(object.Spec().With("nmstate", nestedNativeValue(depth-3))), nil
					}
					return object, nil
				}},
				compilation.Rules{Normalize: func(object api.Object, _ api.Catalog) (api.Object, []api.Issue) {
					if object.Kind() == api.NetworkConfig {
						later++
					}
					return object, nil
				}})
			state, report, err := compiler.Compile(context.Background(), input)
			if depth == desiredstate.MaxDepth {
				if err != nil || state == nil || report == nil || later != 1 {
					t.Fatalf("inclusive depth refused: %v, callbacks=%d", err, later)
				}
				return
			}
			diagnostics := requireCompilationFailure(t, state, report, err)
			if len(diagnostics) != 1 || diagnostics[0].Code != "input.limit" || !strings.Contains(diagnostics[0].Message, "64") || later != 0 {
				t.Fatalf("expanded depth failed to stop admission: %#v, callbacks=%d", diagnostics, later)
			}
		})
	}
}

func TestInheritedCopiesConsumeAggregateRepresentationBudget(t *testing.T) {
	const items = 20_000
	base := environmentYAML + "  defaults:\n    NetworkConfig:\n      nmstate: {items: [" + strings.Repeat("item,", items-1) + "item]}\n"
	for _, recipients := range []int{45, 51} {
		t.Run(fmt.Sprint(recipients), func(t *testing.T) {
			var documents strings.Builder
			documents.WriteString(base)
			for i := range recipients {
				fmt.Fprintf(&documents, "---\napiVersion: bootwright.io/v1alpha1\nkind: NetworkConfig\nmetadata: {name: network-%d}\nspec:\n  machineNetwork: [{cidr: 192.0.2.0/24}]\n", i)
			}
			calls := 0
			compiler := compilation.NewCompiler(yamlstream.Parser{}, nil, compilation.Rules{Normalize: func(object api.Object, _ api.Catalog) (api.Object, []api.Issue) { calls++; return object, nil }})
			state, report, err := compiler.Compile(context.Background(), sources(documents.String()))
			if recipients == 45 {
				if err != nil || state == nil || report == nil || calls != recipients+2 {
					t.Fatalf("admitted copies failed: %v, callbacks=%d", err, calls)
				}
				return
			}
			diagnostics := requireCompilationFailure(t, state, report, err)
			if len(diagnostics) != 1 || diagnostics[0].Code != "input.limit" || !strings.Contains(diagnostics[0].Message, "1000000") || calls != 0 {
				t.Fatalf("inherited copies escaped the pre-normalization ceiling: %#v, callbacks=%d", diagnostics, calls)
			}
		})
	}
}

func repeatedIssues(count int) []api.Issue {
	issues := make([]api.Issue, 0, count*2)
	for i := range count {
		issue := api.Issue{Code: "api.invariant", Field: "$.spec", Message: fmt.Sprintf("synthetic issue %04d", i)}
		issues = append(issues, issue, issue)
	}
	return issues
}

func TestDiagnosticCeilingDeduplicatesAndStopsLaterValidators(t *testing.T) {
	for _, count := range []int{999, 1000, 1200} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			later := 0
			compiler := compilation.NewCompiler(yamlstream.Parser{}, nil,
				compilation.Rules{Validate: func(object api.Object, _ api.Catalog) []api.Issue {
					if object.Kind() == api.Environment {
						return repeatedIssues(count)
					}
					return nil
				}},
				compilation.Rules{Validate: func(object api.Object, _ api.Catalog) []api.Issue {
					if object.Kind() == api.Environment {
						later++
					}
					return nil
				}})
			state, report, err := compiler.Compile(context.Background(), sources(environmentYAML))
			diagnostics := requireCompilationFailure(t, state, report, err)
			if len(diagnostics) != min(count, desiredstate.MaxDiagnostics) {
				t.Fatalf("distinct diagnostic count=%d", len(diagnostics))
			}
			limits := 0
			for _, d := range diagnostics {
				if d.Code == "input.limit" {
					limits++
					if d.Source != nil || d.Object != nil {
						t.Fatal("reserved sentinel has an invented source")
					}
				}
			}
			if count == 999 && (limits != 0 || later != 1) {
				t.Fatal("duplicate issues prematurely exhausted the budget", limits, later)
			}
			if count >= 1000 && (limits != 1 || later != 0) {
				t.Fatal("diagnostic limit failed to stop validation", limits, later)
			}
		})
	}
}

func TestDiagnosticCeilingStopsLaterPartialValidators(t *testing.T) {
	later := 0
	compiler := compilation.NewCompiler(yamlstream.Parser{}, nil,
		compilation.Rules{ValidatePartial: func(api.Object, api.Catalog) []api.Issue { return repeatedIssues(1000) }},
		compilation.Rules{ValidatePartial: func(api.Object, api.Catalog) []api.Issue { later++; return nil }})
	state, report, err := compiler.Compile(context.Background(), sources(environmentYAML+"  defaults:\n    Secret: {type: opaque}\n"))
	diagnostics := requireCompilationFailure(t, state, report, err)
	if len(diagnostics) != desiredstate.MaxDiagnostics || later != 0 {
		t.Fatal("partial validation continued past its ceiling", len(diagnostics), later)
	}
}

type regressionParserFunc func(context.Context, []desiredstate.SourceFile) ([]desiredstate.Document, []desiredstate.Diagnostic, error)

func (f regressionParserFunc) Parse(ctx context.Context, files []desiredstate.SourceFile) ([]desiredstate.Document, []desiredstate.Diagnostic, error) {
	return f(ctx, files)
}

func TestCompilerCancellationStopsAtEveryCallbackBoundary(t *testing.T) {
	for _, phase := range []string{"parse", "partial", "authored", "normalize", "validate"} {
		t.Run(phase, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			later := 0
			first, second := compilation.Rules{}, compilation.Rules{}
			stop := func(api.Object, api.Catalog) []api.Issue { cancel(); return nil }
			next := func(api.Object, api.Catalog) []api.Issue { later++; return nil }
			var parser compilation.SyntaxParser = yamlstream.Parser{}
			switch phase {
			case "parse":
				parser = regressionParserFunc(func(ctx context.Context, files []desiredstate.SourceFile) ([]desiredstate.Document, []desiredstate.Diagnostic, error) {
					documents, diagnostics, err := (yamlstream.Parser{}).Parse(ctx, files)
					cancel()
					return documents, diagnostics, err
				})
				second.ValidatePartial = next
			case "partial":
				first.ValidatePartial, second.ValidatePartial = stop, next
			case "authored":
				first.ValidateAuthored, second.ValidateAuthored = stop, next
			case "normalize":
				first.Normalize = func(object api.Object, _ api.Catalog) (api.Object, []api.Issue) { cancel(); return object, nil }
				second.Normalize = func(object api.Object, _ api.Catalog) (api.Object, []api.Issue) { later++; return object, nil }
			case "validate":
				first.Validate, second.Validate = stop, next
			}
			compiler := compilation.NewCompiler(parser, nil, first, second)
			state, report, err := compiler.Compile(ctx, sources(environmentYAML+"  defaults:\n    Secret: {type: opaque}\n"))
			if state != nil || report != nil || !errors.Is(err, context.Canceled) || later != 0 {
				t.Fatalf("cancellation leaked later work or result: %v %#v %v callbacks=%d", state, report, err, later)
			}
		})
	}
}
