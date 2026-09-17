package compilation

import (
	"slices"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/desiredstate"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

// run is one compilation in flight. The phases below advance it in the order
// [the API contract](../../../specs/api.md) fixes, and none of them mutates a
// representation an earlier phase produced.
type run struct {
	compiler    Compiler
	sources     desiredstate.Sources
	documents   []desiredstate.Document
	parsed      []diagnostics.Diagnostic
	ds          *diagnosticSink
	report      *Report
	environment *objectRecord
	defaults    api.Value
	selected    map[string]bool
	records     []*objectRecord
}

// selectEnvironment reads the one Environment that selects this compilation,
// validates the kind defaults it declares and inherits them into itself.
func (r *run) selectEnvironment() bool {
	r.environment = selectingEnvironment(r.documents, r.parsed, r.ds)
	if r.environment == nil {
		return false
	}
	r.defaults = r.environment.object.Spec().Get("defaults")
	validateShape(r.environment, r.defaults, &api.Shape{Type: api.Mapping, KindDefaults: true}, "$.spec.defaults", true, false, api.Catalog{}, r.ds)
	for _, entry := range r.defaults.Fields() {
		partial := api.NewObject(api.Kind(entry.Name), "", api.Value{}, entry.Value)
		for _, rules := range r.compiler.rules {
			if r.ds.stopped() {
				return false
			}
			if rules.ValidatePartial == nil {
				continue
			}
			for _, issue := range rules.ValidatePartial(partial, api.Catalog{}) {
				issue.Field = "$.spec.defaults." + entry.Name + strings.TrimPrefix(issue.Field, "$.spec")
				r.ds.issue(r.environment, issue)
			}
		}
	}
	r.environment.object = r.environment.object.WithSpec(inherit(r.environment.object.Spec(), r.defaults.Get(string(api.Environment)), api.Schema(api.Environment), "$.spec", true, r.environment, r.environment))
	return withinExpansionBudget([]*objectRecord{r.environment}, r.ds)
}

// decodeSelected reads every document the selected Environment admits, so a
// diagnostic from a file outside the selection never reaches the report.
func (r *run) decodeSelected() bool {
	selected, excluded := selectResources(r.sources, r.documents, r.environment, r.ds)
	r.selected, r.report.ExcludedResourceFiles = selected, excluded
	for _, d := range r.parsed {
		if d.Source == nil || selected[d.Source.Path] {
			r.ds.add(d)
		}
	}
	r.records = []*objectRecord{}
	for _, document := range r.documents {
		if r.ds.stopped() {
			break
		}
		if !selected[document.Path] {
			continue
		}
		record := r.environment
		if document.Path != r.environment.path || document.Index != r.environment.document {
			record = decodeDocument(document, r.ds)
		}
		if record != nil {
			r.records = append(r.records, record)
			r.report.Counts.ObjectsDecoded++
		}
	}
	return !r.ds.stopped()
}

// inheritDefaults gives every record the kind defaults its Environment
// declares. Inheritance preserves every explicit authored value, so the
// authored intent is checked next, before any normalizer can replace it.
func (r *run) inheritDefaults() bool {
	budget := expansionBudget{}
	for _, record := range r.records {
		if record != r.environment {
			record.object = record.object.WithSpec(inherit(record.object.Spec(), r.defaults.Get(string(record.object.Kind())), api.Schema(record.object.Kind()), "$.spec", true, record, r.environment))
		}
		if !budget.admit(record, r.ds) {
			return false
		}
	}
	return withinExpansionBudget(r.records, r.ds)
}

// validateAuthored checks what the operator actually wrote, then applies the
// schema's own built-in defaults.
func (r *run) validateAuthored() bool {
	intent := catalogOf(r.records)
	budget := expansionBudget{}
	for _, record := range r.records {
		for _, rules := range r.compiler.rules {
			if r.ds.stopped() {
				return false
			}
			if rules.ValidateAuthored == nil {
				continue
			}
			for _, issue := range rules.ValidateAuthored(record.object, intent) {
				r.ds.issue(record, issue)
			}
		}
		validateSourcePaths(record, r.sources, r.ds)
		record.object = record.object.WithSpec(builtInDefaults(record.object.Spec(), api.Schema(record.object.Kind())))
		if !budget.admit(record, r.ds) {
			return false
		}
	}
	return !r.ds.stopped()
}

// closeGraph narrows the records to the closure the Environment selects and
// reports what that selection excluded.
func (r *run) closeGraph() {
	if r.compiler.selectGraph == nil {
		return
	}
	selection := r.compiler.selectGraph(catalogOf(r.records))
	r.report.ExcludedContainerClusters = sortedNames(selection.ExcludedContainerClusters)
	r.report.ExcludedStorageClusters = sortedNames(selection.ExcludedStorageClusters)
	for _, problem := range selection.Problems {
		for _, record := range r.records {
			if record.object.Identity() == problem.Object.Identity() {
				r.ds.issue(record, problem.Issue)
			}
		}
	}
	retained := map[string]bool{}
	for _, o := range selection.Catalog.Objects() {
		retained[o.Identity()] = true
	}
	filtered := r.records[:0]
	for _, record := range r.records {
		if retained[record.object.Identity()] {
			filtered = append(filtered, record)
		}
	}
	r.records = filtered
}

// normalizationOrder is the kind order normalization follows, so a dependency
// provider is normalized before its consumers and the result never depends on
// source order or on a peer's normalized state.
func normalizationOrder() []api.Kind {
	return []api.Kind{
		api.Environment, api.Entitlement, api.Secret, api.NetworkConfig, api.MachineImage,
		api.MachineInstallProfile, api.InfraProvider, api.Machine, api.Proxy, api.DNSServer,
		api.NTPServer, api.ArtifactServer, api.Registry, api.LoadBalancer, api.ContainerCluster,
		api.StorageCluster, api.StoragePlacementPolicy, api.StoragePool, api.StorageFilesystem,
		api.StorageObjectGateway, api.StorageNFSExport, api.StorageExport, api.ClusterAddon,
		api.ClusterAddonProfile, api.ClusterAddonBinding, api.CustomPlaybook,
	}
}

// normalize applies each kind's normalizer in dependency order, recording where
// a normalized value came from and what it reordered.
func (r *run) normalize() bool {
	validateIdentities(r.records, r.ds)
	byIdentity := make(map[string]*objectRecord, len(r.records))
	for _, record := range r.records {
		identity := record.object.Identity()
		if _, exists := byIdentity[identity]; exists {
			byIdentity[identity] = nil
		} else {
			byIdentity[identity] = record
		}
	}
	budget := expansionBudget{}
	for _, record := range r.records {
		if !budget.admit(record, r.ds) {
			return false
		}
	}
	ranks := normalizationOrder()
	ordered := slices.Clone(r.records)
	slices.SortStableFunc(ordered, func(a, b *objectRecord) int {
		return slices.Index(ranks, a.object.Kind()) - slices.Index(ranks, b.object.Kind())
	})
	for _, kind := range ranks {
		catalog := catalogOf(r.records)
		for _, record := range ordered {
			if record.object.Kind() != kind {
				continue
			}
			if r.ds.stopped() {
				break
			}
			if !r.normalizeRecord(record, catalog, byIdentity, &budget) {
				return false
			}
		}
	}
	return withinExpansionBudget(r.records, r.ds)
}

func (r *run) normalizeRecord(record *objectRecord, catalog api.Catalog, byIdentity map[string]*objectRecord, budget *expansionBudget) bool {
	for _, rules := range r.compiler.rules {
		if r.ds.stopped() {
			return false
		}
		if rules.Normalize == nil {
			continue
		}
		previous := expansionBudget{}
		if !previous.admit(record, r.ds) {
			return false
		}
		before := record.object
		object, issues := rules.Normalize(before, catalog)
		previousSpec := record.object.Spec()
		record.object = object
		budget.nodes -= previous.nodes
		if !budget.admit(record, r.ds) {
			return false
		}
		record.recordReordering(previousSpec, object.Spec(), api.Schema(object.Kind()))
		if rules.NormalizationOrigins != nil {
			record.recordOrigins(rules.NormalizationOrigins(before, object, catalog), byIdentity)
		}
		for _, issue := range issues {
			r.ds.issue(record, issue)
		}
	}
	return true
}

// validateEffective checks the completed model every consumer receives.
func (r *run) validateEffective(catalog api.Catalog) bool {
	for _, record := range r.records {
		if r.ds.stopped() {
			break
		}
		if !api.ValidLexical("name", record.object.Name()) {
			r.ds.issue(record, api.Issue{Code: "api.value", Field: "$.metadata.name", Message: "object name must be a DNS label"})
		}
		validateShape(record, record.object.Spec(), api.Schema(record.object.Kind()), "$.spec", false, true, catalog, r.ds)
		for _, rules := range r.compiler.rules {
			if r.ds.stopped() {
				return false
			}
			if rules.Validate == nil {
				continue
			}
			for _, issue := range rules.Validate(record.object, catalog) {
				r.ds.issue(record, issue)
			}
		}
	}
	return true
}

// complete builds the immutable model and the report its warnings belong to.
func (r *run) complete(catalog api.Catalog) (*State, *Report) {
	for _, d := range r.ds.sorted() {
		if d.Severity != "warning" {
			continue
		}
		r.report.Diagnostics = append(r.report.Diagnostics, d)
		if d.Code == "api.deferred" {
			r.report.Advisories = append(r.report.Advisories, d)
		}
	}
	authored := []api.Object{}
	origins := map[string]diagnostics.SourceLocation{}
	for _, record := range r.records {
		authored = append(authored, record.authored)
		origins[record.object.Identity()] = diagnostics.SourceLocation{Path: record.path, Document: record.document}
	}
	return NewState(api.NewCatalog(authored), catalog, origins), r.report
}
