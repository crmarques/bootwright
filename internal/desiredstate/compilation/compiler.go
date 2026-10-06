package compilation

import (
	"context"
	"errors"
	"maps"
	"slices"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/desiredstate"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

// Rules are pure, component-owned transformations and admission constraints.
type Rules struct {
	Normalize            func(api.Object, api.Catalog) (api.Object, []api.Issue)
	NormalizationOrigins func(api.Object, api.Object, api.Catalog) []api.FieldOrigin
	ValidatePartial      func(api.Object, api.Catalog) []api.Issue
	ValidateAuthored     func(api.Object, api.Catalog) []api.Issue
	Validate             func(api.Object, api.Catalog) []api.Issue
}

type Selection struct {
	Catalog                   api.Catalog
	ExcludedContainerClusters []string
	ExcludedStorageClusters   []string
	Problems                  []ObjectIssue
}

type ObjectIssue struct {
	Object api.Object
	Issue  api.Issue
	Target string
}
type SelectGraph func(api.Catalog) Selection

type Compiler struct {
	parser      SyntaxParser
	rules       []Rules
	selectGraph SelectGraph
}

func NewCompiler(parser SyntaxParser, selection SelectGraph, rules ...Rules) Compiler {
	return Compiler{parser: parser, rules: slices.Clone(rules), selectGraph: selection}
}

// State exposes immutable authored and effective values separately. Provenance
// stays outside both representations and never conveys execution authority.
type State struct {
	authored  api.Catalog
	effective api.Catalog
	origins   map[string]diagnostics.SourceLocation
}

func NewState(authored, effective api.Catalog, origins map[string]diagnostics.SourceLocation) *State {
	return &State{
		authored:  canonicalCatalog(authored.Objects()),
		effective: canonicalCatalog(effective.Objects()),
		origins:   maps.Clone(origins),
	}
}

func (s *State) Authored() api.Catalog  { return s.authored }
func (s *State) Effective() api.Catalog { return s.effective }
func (s *State) Origin(identity string) (diagnostics.SourceLocation, bool) {
	origin, ok := s.origins[identity]
	return origin, ok
}

// Compile advances one desired-state input through the phases the API contract
// fixes: safe discovery and selection, strict decoding, authored validation,
// graph closure, normalization, effective validation, and the completed
// immutable model. No phase mutates what an earlier one produced, and a phase
// that cannot continue ends the compilation with the diagnostics it collected.
func (c Compiler) Compile(ctx context.Context, sources desiredstate.Sources) (*State, *Report, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	if c.parser == nil {
		return nil, nil, errors.New("syntax parser is not configured")
	}
	documents, parseDiagnostics, err := c.parser.Parse(ctx, slices.Clone(sources.Files))
	if ctx.Err() != nil {
		return nil, nil, ctx.Err()
	}
	ds := newDiagnostics(ctx)
	if err != nil {
		failures := diagnostics.Of(err)
		if len(failures) == 0 {
			return nil, nil, err
		}
		for _, diagnostic := range append(parseDiagnostics, failures...) {
			ds.add(diagnostic)
		}
		return compilationFailure(ctx, ds)
	}
	r := &run{
		compiler: c, sources: sources, documents: documents, parsed: parseDiagnostics, ds: ds,
		report: &Report{
			Counts:                    Counts{FilesSeen: len(sources.Files)},
			ExcludedContainerClusters: []string{}, ExcludedStorageClusters: []string{},
			ExcludedResourceFiles: []string{}, Advisories: []diagnostics.Diagnostic{},
		},
	}
	for _, phase := range []func() bool{
		r.selectEnvironment, r.decodeSelected, r.inheritDefaults, r.validateAuthored,
	} {
		if !phase() {
			return compilationFailure(ctx, ds)
		}
	}
	r.closeGraph()
	if !r.normalize() {
		return compilationFailure(ctx, ds)
	}
	catalog := catalogOf(r.records)
	if !r.validateEffective(catalog) {
		return compilationFailure(ctx, ds)
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	if ds.hasErrors() {
		return compilationFailure(ctx, ds)
	}
	state, report := r.complete(catalog)
	return state, report, nil
}

func compilationFailure(ctx context.Context, ds *diagnosticSink) (*State, *Report, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	return nil, nil, &diagnostics.Failure{Diagnostics: ds.sorted()}
}
func catalogOf(records []*objectRecord) api.Catalog {
	objects := make([]api.Object, 0, len(records))
	for _, record := range records {
		objects = append(objects, record.object)
	}
	return api.NewCatalog(objects)
}
func canonicalCatalog(objects []api.Object) api.Catalog {
	slices.SortStableFunc(objects, func(a, b api.Object) int {
		if n := api.KindIndex(a.Kind()) - api.KindIndex(b.Kind()); n != 0 {
			return n
		}
		return strings.Compare(a.Name(), b.Name())
	})
	return api.NewCatalog(objects)
}
func sortedNames(values []string) []string {
	values = append([]string{}, values...)
	slices.Sort(values)
	return slices.Compact(values)
}
