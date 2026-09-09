package compilation

import (
	"context"
	"errors"
	"maps"
	"slices"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/desiredstate"
)

type SyntaxParser interface {
	Parse(context.Context, []desiredstate.SourceFile) ([]desiredstate.Document, []desiredstate.Diagnostic, error)
}

// Rules are pure, component-owned transformations and admission constraints.
type Rules struct {
	Normalize        func(api.Object, api.Catalog) (api.Object, []api.Issue)
	ValidatePartial  func(api.Object, api.Catalog) []api.Issue
	ValidateAuthored func(api.Object, api.Catalog) []api.Issue
	Validate         func(api.Object, api.Catalog) []api.Issue
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

type Counts struct {
	FilesSeen      int `json:"filesSeen"`
	ObjectsDecoded int `json:"objectsDecoded"`
}
type Report struct {
	Diagnostics               []desiredstate.Diagnostic `json:"-"`
	Counts                    Counts                    `json:"counts"`
	ExcludedContainerClusters []string                  `json:"excludedContainerClusters"`
	ExcludedStorageClusters   []string                  `json:"excludedStorageClusters"`
	ExcludedResourceFiles     []string                  `json:"excludedResourceFiles"`
	Advisories                []desiredstate.Diagnostic `json:"advisories"`
}

// State exposes immutable authored and effective values separately. Provenance
// stays outside both representations and never conveys execution authority.
type State struct {
	authored  api.Catalog
	effective api.Catalog
	origins   map[string]desiredstate.SourceLocation
}

func NewState(authored, effective api.Catalog, origins map[string]desiredstate.SourceLocation) *State {
	return &State{
		authored:  canonicalCatalog(authored.Objects()),
		effective: canonicalCatalog(effective.Objects()),
		origins:   maps.Clone(origins),
	}
}

func (s *State) Authored() api.Catalog  { return s.authored }
func (s *State) Effective() api.Catalog { return s.effective }
func (s *State) Origin(identity string) (desiredstate.SourceLocation, bool) {
	origin, ok := s.origins[identity]
	return origin, ok
}

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
		failures := desiredstate.DiagnosticsOf(err)
		if len(failures) == 0 {
			return nil, nil, err
		}
		for _, diagnostic := range parseDiagnostics {
			ds.add(diagnostic)
		}
		for _, diagnostic := range failures {
			ds.add(diagnostic)
		}
		return compilationFailure(ctx, ds)
	}
	report := &Report{Counts: Counts{FilesSeen: len(sources.Files)}, ExcludedContainerClusters: []string{}, ExcludedStorageClusters: []string{}, ExcludedResourceFiles: []string{}, Advisories: []desiredstate.Diagnostic{}}
	environment := selectingEnvironment(documents, parseDiagnostics, ds)
	if environment == nil {
		return compilationFailure(ctx, ds)
	}
	defaults := environment.object.Spec().Get("defaults")
	validateShape(environment, defaults, &api.Shape{Type: api.Mapping, KindDefaults: true}, "$.spec.defaults", true, false, api.Catalog{}, ds)
	for _, entry := range defaults.Fields() {
		partial := api.NewObject(api.Kind(entry.Name), "", api.Value{}, entry.Value)
		for _, rules := range c.rules {
			if ds.stopped() {
				return compilationFailure(ctx, ds)
			}
			if rules.ValidatePartial != nil {
				for _, issue := range rules.ValidatePartial(partial, api.Catalog{}) {
					issue.Field = "$.spec.defaults." + entry.Name + strings.TrimPrefix(issue.Field, "$.spec")
					ds.issue(environment, issue)
				}
			}
		}
	}
	environment.object = environment.object.WithSpec(inherit(environment.object.Spec(), defaults.Get(string(api.Environment)), api.Schema(api.Environment), "$.spec", true, environment, environment))
	if !withinExpansionBudget([]*objectRecord{environment}, ds) {
		return compilationFailure(ctx, ds)
	}
	selected, excluded := selectResources(sources, documents, environment, ds)
	report.ExcludedResourceFiles = excluded
	for _, d := range parseDiagnostics {
		if d.Source == nil || selected[d.Source.Path] {
			ds.add(d)
		}
	}
	records := []*objectRecord{}
	for _, document := range documents {
		if ds.stopped() {
			break
		}
		if !selected[document.Path] {
			continue
		}
		var record *objectRecord
		if document.Path == environment.path && document.Index == environment.document {
			record = environment
		} else {
			record = decodeDocument(document, ds)
		}
		if record != nil {
			records = append(records, record)
			report.Counts.ObjectsDecoded++
		}
	}
	if ds.stopped() {
		return compilationFailure(ctx, ds)
	}
	// Inheritance preserves every explicit authored value. Check the resulting
	// authored intent before any normalizer can erase or replace it.
	inheritBudget := expansionBudget{}
	for _, record := range records {
		if record != environment {
			record.object = record.object.WithSpec(inherit(record.object.Spec(), defaults.Get(string(record.object.Kind())), api.Schema(record.object.Kind()), "$.spec", true, record, environment))
		}
		if !inheritBudget.admit(record, ds) {
			return compilationFailure(ctx, ds)
		}
	}
	if !withinExpansionBudget(records, ds) {
		return compilationFailure(ctx, ds)
	}
	intent := catalogOf(records)
	intrinsicBudget := expansionBudget{}
	for _, record := range records {
		for _, rules := range c.rules {
			if ds.stopped() {
				return compilationFailure(ctx, ds)
			}
			if rules.ValidateAuthored != nil {
				for _, issue := range rules.ValidateAuthored(record.object, intent) {
					ds.issue(record, issue)
				}
			}
		}
		validateSourcePaths(record, sources, ds)
		record.object = record.object.WithSpec(builtInDefaults(record.object.Spec(), api.Schema(record.object.Kind())))
		if !intrinsicBudget.admit(record, ds) {
			return compilationFailure(ctx, ds)
		}
	}
	if ds.stopped() {
		return compilationFailure(ctx, ds)
	}
	if c.selectGraph != nil {
		selection := c.selectGraph(catalogOf(records))
		report.ExcludedContainerClusters = sortedNames(selection.ExcludedContainerClusters)
		report.ExcludedStorageClusters = sortedNames(selection.ExcludedStorageClusters)
		for _, problem := range selection.Problems {
			for _, record := range records {
				if record.object.Identity() == problem.Object.Identity() {
					ds.issue(record, problem.Issue)
				}
			}
		}
		retained := map[string]bool{}
		for _, o := range selection.Catalog.Objects() {
			retained[o.Identity()] = true
		}
		filtered := records[:0]
		for _, record := range records {
			if retained[record.object.Identity()] {
				filtered = append(filtered, record)
			}
		}
		records = filtered
	}
	validateIdentities(records, ds)
	// Normalize dependency providers before their consumers. Within a kind the
	// result must not depend on source order or another peer's normalized state.
	ordered := slices.Clone(records)
	normalizationBudget := expansionBudget{}
	for _, record := range records {
		if !normalizationBudget.admit(record, ds) {
			return compilationFailure(ctx, ds)
		}
	}
	ranks := []api.Kind{api.Environment, api.Entitlement, api.Secret, api.NetworkConfig, api.MachineImage, api.MachineInstallProfile, api.InfraProvider, api.Machine, api.InfraComponent, api.ContainerCluster, api.StorageCluster, api.StoragePlacementPolicy, api.StoragePool, api.StorageFilesystem, api.StorageObjectGateway, api.StorageNFSExport, api.StorageExport, api.ClusterAddon, api.ClusterAddonProfile, api.ClusterAddonBinding, api.CustomPlaybook}
	slices.SortStableFunc(ordered, func(a, b *objectRecord) int {
		return slices.Index(ranks, a.object.Kind()) - slices.Index(ranks, b.object.Kind())
	})
	for _, kind := range ranks {
		catalog := catalogOf(records)
		for _, record := range ordered {
			if record.object.Kind() != kind {
				continue
			}
			if ds.stopped() {
				break
			}
			for _, rules := range c.rules {
				if ds.stopped() {
					return compilationFailure(ctx, ds)
				}
				if rules.Normalize != nil {
					previous := expansionBudget{}
					if !previous.admit(record, ds) {
						return compilationFailure(ctx, ds)
					}
					object, issues := rules.Normalize(record.object, catalog)
					previousSpec := record.object.Spec()
					record.object = object
					normalizationBudget.nodes -= previous.nodes
					if !normalizationBudget.admit(record, ds) {
						return compilationFailure(ctx, ds)
					}
					record.recordReordering(previousSpec, object.Spec(), api.Schema(object.Kind()))
					for _, issue := range issues {
						ds.issue(record, issue)
					}
				}
			}
		}
	}
	if !withinExpansionBudget(records, ds) {
		return compilationFailure(ctx, ds)
	}
	catalog := catalogOf(records)
	for _, record := range records {
		if ds.stopped() {
			break
		}
		if !api.ValidLexical("name", record.object.Name()) {
			ds.issue(record, api.Issue{Code: "api.value", Field: "$.metadata.name", Message: "object name must be a DNS label"})
		}
		validateShape(record, record.object.Spec(), api.Schema(record.object.Kind()), "$.spec", false, true, catalog, ds)
		for _, rules := range c.rules {
			if ds.stopped() {
				return compilationFailure(ctx, ds)
			}
			if rules.Validate != nil {
				for _, issue := range rules.Validate(record.object, catalog) {
					ds.issue(record, issue)
				}
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	if ds.hasErrors() {
		return compilationFailure(ctx, ds)
	}
	for _, d := range ds.sorted() {
		if d.Severity == "warning" {
			report.Diagnostics = append(report.Diagnostics, d)
			if d.Code == "api.deferred" {
				report.Advisories = append(report.Advisories, d)
			}
		}
	}
	authored := []api.Object{}
	origins := map[string]desiredstate.SourceLocation{}
	for _, record := range records {
		authored = append(authored, record.authored)
		origins[record.object.Identity()] = desiredstate.SourceLocation{Path: record.path, Document: record.document}
	}
	return NewState(api.NewCatalog(authored), catalog, origins), report, nil
}

func compilationFailure(ctx context.Context, ds *diagnostics) (*State, *Report, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	return nil, nil, &desiredstate.Failure{Diagnostics: ds.sorted()}
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
