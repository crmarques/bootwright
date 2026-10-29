//go:build linux && amd64

package main

import (
	"context"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

const reconciliationSpec = "../../specs/state-reconciliation.md"

// edgeRow is one row of the reference-edge catalog: the requiring block's
// family and kind, the required kind and family, and the field paths on the
// requiring object that name the target, when the row names it by fields.
type edgeRow struct {
	family, kind, target, targetFamily string
	paths                              []string
}

func (r edgeRow) key() string {
	return r.family + " " + r.kind + " -> " + r.target + " " + r.targetFamily
}

var edgeCode = regexp.MustCompile("`([^`]+)`")

func edgeCatalog(t *testing.T) []edgeRow {
	t.Helper()
	var rows []edgeRow
	for _, cells := range markdownTable(t, reconciliationSpec, "### Reference edges", []string{"Block", "Requires", "Named by"}) {
		block, requires := edgeCode.FindAllStringSubmatch(cells["Block"], -1), edgeCode.FindAllStringSubmatch(cells["Requires"], -1)
		if len(block) != 2 || len(requires) < 1 || len(requires) > 2 {
			t.Fatalf("edge row %v names no family, kind and target", cells)
		}
		row := edgeRow{family: block[0][1], kind: block[1][1], target: requires[0][1]}
		if len(requires) == 2 {
			row.targetFamily = requires[1][1]
		}
		for _, match := range edgeCode.FindAllStringSubmatch(cells["Named by"], -1) {
			if strings.HasPrefix(match[1], "spec.") {
				row.paths = append(row.paths, match[1])
			}
		}
		rows = append(rows, row)
	}
	return rows
}

// edgeTargets reads the names one field path holds on an object: a path
// segment ending in [] walks a list, and a record where a name is expected
// yields its name field.
func edgeTargets(value api.Value, segments []string) []string {
	if len(segments) == 0 {
		switch value.Type() {
		case api.String:
			return []string{value.Text()}
		case api.Mapping:
			return []string{value.Get("name").Text()}
		}
		return nil
	}
	field, list := strings.CutSuffix(segments[0], "[]")
	next := value.Get(field)
	if !list {
		return edgeTargets(next, segments[1:])
	}
	var names []string
	for _, item := range next.Items() {
		names = append(names, edgeTargets(item, segments[1:])...)
	}
	return names
}

type familyBlock struct {
	family     string
	definition reconciliation.BlockDefinition
}

type plannedExample struct {
	name   string
	state  *compilation.State
	blocks []familyBlock
}

// plannedExamples plans every accepted example through this build's
// composition and keeps each capability family's block definitions.
func plannedExamples(t *testing.T) []plannedExample {
	t.Helper()
	resolver := buildCapabilities(systemClock{}, exampleControllerPorts(t), exampleMediaRecords{})
	names := []string{}
	for name := range exampleDirectories(t) {
		if name != refusedExample {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	var planned []plannedExample
	for _, name := range names {
		state, _ := compileAcceptance(t, exampleDirectory(t, name))
		input := lifecycle.PlanInput{Verb: reconciliation.Apply, Context: lifecycle.ContextIdentity{Name: name}, State: state, Controller: "controller"}
		var blocks []familyBlock
		for _, binding := range resolver.Bindings() {
			capability, _ := resolver.Resolve(binding.Kind, binding.Implementation)
			contribution, err := capability.Plan(context.Background(), input)
			if err != nil {
				continue
			}
			for _, definition := range contribution.Definitions {
				blocks = append(blocks, familyBlock{binding.Family, definition})
			}
		}
		planned = append(planned, plannedExample{name, state, blocks})
	}
	return planned
}

// TestEveryRequirementMatchesTheEdgeCatalog plans every example through this
// build's composition and holds each capability's requirements to the
// reference-edge catalog (specs/state-reconciliation.md, Reference edges):
// a requirement no row names fails, and so does a target a row's field path
// names on the requiring object, realized by some block, that the family's
// blocks for that object neither require nor depend on.
func TestEveryRequirementMatchesTheEdgeCatalog(t *testing.T) {
	rows := edgeCatalog(t)
	catalog := map[string]edgeRow{}
	for _, row := range rows {
		if _, repeated := catalog[row.key()]; repeated {
			t.Errorf("the edge catalog repeats %s", row.key())
		}
		catalog[row.key()] = row
	}
	used := map[string]bool{}
	for _, planned := range plannedExamples(t) {
		name, state, blocks := planned.name, planned.state, planned.blocks
		realizers := func(kind, object, family string) []string {
			var ids []string
			for _, block := range blocks {
				if block.definition.Kind == kind && block.definition.Object == object && (family == "" || block.family == family) {
					ids = append(ids, block.definition.ID)
				}
			}
			return ids
		}
		for _, block := range blocks {
			for _, requirement := range block.definition.Requires {
				key := edgeRow{family: block.family, kind: block.definition.Kind, target: requirement.Kind, targetFamily: requirement.Family}.key()
				if _, found := catalog[key]; !found {
					t.Errorf("%s: %s %s/%s requires %s/%s, which no edge catalog row names (%s)", name, block.family, block.definition.Kind, block.definition.Object, requirement.Kind, requirement.Object, key)
				}
				used[key] = true
			}
		}
		for _, row := range rows {
			if len(row.paths) == 0 {
				continue
			}
			for _, object := range state.Effective().OfKind(api.Kind(row.kind)) {
				var family []familyBlock
				for _, block := range blocks {
					if block.family == row.family && block.definition.Kind == row.kind && block.definition.Object == object.Name() {
						family = append(family, block)
					}
				}
				if len(family) == 0 {
					continue
				}
				for _, path := range row.paths {
					for _, target := range edgeTargets(object.Spec(), strings.Split(strings.TrimPrefix(path, "spec."), ".")) {
						ids := realizers(row.target, target, row.targetFamily)
						if target == "" || len(ids) == 0 {
							continue
						}
						if !edgeSatisfied(family, row, target, ids) {
							t.Errorf("%s: %s %s/%s omits its %s requirement on %s/%s named by %s", name, row.family, row.kind, object.Name(), row.target, row.target, target, path)
						}
					}
				}
			}
		}
	}
	for _, row := range rows {
		if !used[row.key()] {
			t.Errorf("no example plans the edge catalog row %s; drop it or add an example that needs it", row.key())
		}
	}
}

func edgeSatisfied(family []familyBlock, row edgeRow, target string, realizers []string) bool {
	for _, block := range family {
		if slices.Contains(block.definition.Requires, reconciliation.ObjectRef{Kind: row.target, Object: target, Family: row.targetFamily}) {
			return true
		}
		for _, id := range realizers {
			if id != block.definition.ID && slices.Contains(block.definition.Dependencies, id) {
				return true
			}
		}
	}
	return false
}

// referenceField is one schema field whose name ends in Ref or Refs: the kind
// that declares it, its spec path in the catalog's grammar (a list segment
// ends in []) and its shape.
type referenceField struct {
	kind  api.Kind
	path  string
	shape *api.Shape
}

func referenceFields(kind api.Kind, shape *api.Shape, prefix string, depth int) []referenceField {
	if shape == nil || depth > 16 {
		return nil
	}
	var found []referenceField
	for _, alternative := range shape.Alternatives {
		found = append(found, referenceFields(kind, alternative, prefix, depth+1)...)
	}
	for _, field := range shape.Fields {
		path, inner := prefix+"."+field.Name, field.Shape
		if inner != nil && inner.Type == api.Sequence {
			path, inner = path+"[]", inner.Element
		}
		if strings.HasSuffix(field.Name, "Ref") || strings.HasSuffix(field.Name, "Refs") {
			found = append(found, referenceField{kind, path, inner})
		}
		found = append(found, referenceFields(kind, inner, path, depth+1)...)
	}
	return found
}

// unorderedReferences names each reference field that may name an object
// some block realizes, that no edge catalog row names and that the example
// corpus does not show its family ordering, by "<Kind> <path>", with why it
// orders nothing (specs/state-reconciliation.md, Reference edges).
func unorderedReferences() map[string]string {
	placement := "a managed service's placement Machine is OS-ready (os.provided: true), so no block of the plan readies it (specs/infrastructure-services.md, Selection and refusal)"
	unplanned := "the storage capability requires it, but no example plans it and a catalog row needs one that does"
	return map[string]string{
		"ArtifactServer spec.machineRef":                                                     placement,
		"DNSServer spec.machineRef":                                                          placement,
		"NTPServer spec.machineRef":                                                          placement,
		"Proxy spec.machineRef":                                                              placement,
		"InfraProvider spec.libvirt.machineRef":                                              "the libvirt provider host is an OS-ready Machine (specs/substrates.md), so no block of the plan readies it",
		"Environment spec.controller.machineRef":                                             "the controller Machine is the host that runs Bootwright, which no block of its own plan realizes",
		"Environment spec.lifecycle.rescue.artifactServerEndpoint.serverRef":                 "admission refuses a present rescue until a rescue journey exists (specs/api/environment.md, Lifecycle rescue declaration)",
		"Machine spec.network.inline.dns[].serverRef":                                        "the os-install and physical-erase rows order the dns selections of the Machine's network configuration, inline or through spec.network.configRef",
		"Machine spec.proxy.proxyRef":                                                        "a Bootwright-installed Machine may select only an external Proxy, which no block realizes, and an OS-ready Machine has no block (specs/api/machines.md, Machine proxy)",
		"MachineInstallProfile spec.proxy.proxyRef":                                          "a profile's proxy must be external, which no block realizes (specs/api/machines.md, Installation network services)",
		"MachineInstallProfile spec.ntp[].serverRef":                                         "the package tree needs no time service; each installing Machine inherits the list into spec.os.install.ntp, whose os-install row orders it",
		"OpenShiftCluster spec.install.agent.bootArtifacts.artifactServerEndpoint.serverRef": "only a disconnected cluster selects it, and this executable installs none (specs/openshift-clusters.md, Refusal table)",
		"ClusterAddonBinding spec.afterRefs[]":                                               "the add-on capability orders the instances a binding's afterRefs select among its own blocks (specs/add-ons.md)",
		"CephClient spec.filesystemRef":                                                      unplanned,
		"CephNFSExport spec.filesystemRef":                                                   unplanned,
		"CephSMBResource spec.filesystemRef":                                                 unplanned,
		"CephObjectGateway spec.pools.indexPoolRef":                                          unplanned,
		"CephObjectGateway spec.pools.dataPoolRef":                                           unplanned,
		"CephObjectGateway spec.pools.dataExtraPoolRef":                                      unplanned,
		"CephObjectBucket spec.objectGatewayRef":                                             "admission requires the bucket's owner to use the same gateway, and the bucket requires its owner, which requires that gateway",
		"CephSubvolume spec.poolLayoutRef":                                                   "admission requires a data pool of the subvolume's filesystem, which the subvolume requires and which requires its data pools",
	}
}

// referenceOrdering counts, over the example corpus, the occurrences of each
// reference field that names an object a block realizes, and those where a
// block of the referencing object requires or depends on that target's block.
func referenceOrdering(fields []referenceField, planned []plannedExample) (occurring, ordered map[string]int) {
	occurring, ordered = map[string]int{}, map[string]int{}
	for _, example := range planned {
		for _, field := range fields {
			if field.shape == nil || field.shape.Nested != "" || len(field.shape.Reference) == 0 {
				continue
			}
			key := string(field.kind) + " " + field.path
			for _, object := range example.state.Effective().OfKind(field.kind) {
				var own []familyBlock
				for _, block := range example.blocks {
					if block.definition.Kind == string(field.kind) && block.definition.Object == object.Name() {
						own = append(own, block)
					}
				}
				for _, target := range edgeTargets(object.Spec(), strings.Split(strings.TrimPrefix(field.path, "spec."), ".")) {
					var ids []string
					for _, block := range example.blocks {
						if slices.Contains(field.shape.Reference, api.Kind(block.definition.Kind)) && block.definition.Object == target {
							ids = append(ids, block.definition.ID)
						}
					}
					if target == "" || len(own) == 0 || len(ids) == 0 {
						continue
					}
					occurring[key]++
					if slices.ContainsFunc(own, func(block familyBlock) bool { return orders(block.definition, field.shape.Reference, target, ids) }) {
						ordered[key]++
					}
				}
			}
		}
	}
	return occurring, ordered
}

func orders(definition reconciliation.BlockDefinition, kinds []api.Kind, target string, realizers []string) bool {
	for _, requirement := range definition.Requires {
		if requirement.Object == target && slices.Contains(kinds, api.Kind(requirement.Kind)) {
			return true
		}
	}
	return slices.ContainsFunc(realizers, func(id string) bool { return slices.Contains(definition.Dependencies, id) })
}

// referenceViolations is each reference field that names a kind some block
// realizes and that neither a catalog row, an ordering the corpus shows on
// every occurrence, nor unorderedReferences accounts for; then each field the
// corpus shows ordered on some occurrences but not all; then each
// unorderedReferences entry that excuses nothing.
func referenceViolations(fields []referenceField, rows []edgeRow, realized map[api.Kind]bool, occurring, ordered map[string]int, excused map[string]string) []string {
	listed := map[string]bool{}
	for _, row := range rows {
		for _, path := range row.paths {
			listed[row.kind+" "+path] = true
		}
	}
	var violations []string
	needed := map[string]bool{}
	for _, field := range fields {
		key := string(field.kind) + " " + field.path
		if field.shape == nil || field.shape.Nested != "" || listed[key] || !realized[field.kind] || !slices.ContainsFunc(field.shape.Reference, func(kind api.Kind) bool { return realized[kind] }) {
			continue
		}
		if ordered[key] > 0 && ordered[key] < occurring[key] {
			violations = append(violations, fmt.Sprintf("%s orders %d of its %d occurrences in the examples; give it an edge catalog row", key, ordered[key], occurring[key]))
			continue
		}
		if ordered[key] > 0 {
			continue
		}
		needed[key] = true
		if excused[key] == "" {
			violations = append(violations, fmt.Sprintf("%s names a kind a block realizes, but no edge catalog row names it, none of its %d example occurrences is ordered and unorderedReferences states no reason", key, occurring[key]))
		}
	}
	for _, key := range slices.Sorted(maps.Keys(excused)) {
		if !needed[key] {
			violations = append(violations, "unorderedReferences excuses "+key+", which a row names, the examples show ordered, or which names no realized kind; remove it")
		}
	}
	slices.Sort(violations)
	return slices.Compact(violations)
}

// TestEveryReferenceFieldHasAnEdgeRowOrAReason enumerates every Ref and Refs
// field the API schema registry declares and holds each that can name an
// object some block of the example corpus realizes to an edge catalog row, to
// its family's own ordering on every example occurrence, or to a stated
// reason, so a new reference field is never silently unordered.
func TestEveryReferenceFieldHasAnEdgeRowOrAReason(t *testing.T) {
	var fields []referenceField
	for _, kind := range api.Kinds() {
		fields = append(fields, referenceFields(kind, api.Schema(kind), "spec", 0)...)
	}
	planned := plannedExamples(t)
	realized := map[api.Kind]bool{}
	for _, example := range planned {
		for _, block := range example.blocks {
			realized[api.Kind(block.definition.Kind)] = true
		}
	}
	if len(fields) == 0 || len(realized) == 0 {
		t.Fatalf("the rule reads %d reference fields and %d realized kinds", len(fields), len(realized))
	}
	occurring, ordered := referenceOrdering(fields, planned)
	for _, violation := range referenceViolations(fields, edgeCatalog(t), realized, occurring, ordered, unorderedReferences()) {
		t.Error(violation)
	}
}

// A new reference to a realized kind fails until a row, a shown ordering or a
// reason accounts for it; a partly ordered one fails; and a reason for a
// field that needs none fails as stale.
func TestTheReferenceFieldRuleRejectsAnUnorderedReference(t *testing.T) {
	shape := func(kind api.Kind) *api.Shape { return &api.Shape{Type: api.String, Reference: []api.Kind{kind}} }
	fields := []referenceField{
		{api.Machine, "spec.example.serverRef", shape(api.NTPServer)},
		{api.Machine, "spec.example.secretRef", shape(api.Secret)},
		{api.Machine, "spec.example.poolRef", shape(api.CephPool)},
		{api.Machine, "spec.example.partialRef", shape(api.CephPool)},
		{api.Machine, "spec.substrate.providerRef", shape(api.InfraProvider)},
	}
	rows := []edgeRow{{family: "substrate-machine", kind: "Machine", target: "InfraProvider", paths: []string{"spec.substrate.providerRef"}}}
	realized := map[api.Kind]bool{api.Machine: true, api.NTPServer: true, api.InfraProvider: true, api.CephPool: true}
	occurring := map[string]int{"Machine spec.example.poolRef": 2, "Machine spec.example.partialRef": 2}
	ordered := map[string]int{"Machine spec.example.poolRef": 2, "Machine spec.example.partialRef": 1}
	if got := referenceViolations(fields, rows, realized, occurring, ordered, nil); !slices.Equal(got, []string{
		"Machine spec.example.partialRef orders 1 of its 2 occurrences in the examples; give it an edge catalog row",
		"Machine spec.example.serverRef names a kind a block realizes, but no edge catalog row names it, none of its 0 example occurrences is ordered and unorderedReferences states no reason",
	}) {
		t.Fatalf("violations = %q", got)
	}
	excused := map[string]string{"Machine spec.example.serverRef": "seeded", "Machine spec.example.secretRef": "seeded"}
	if got := referenceViolations(fields, rows, realized, occurring, ordered, excused); !slices.Equal(got, []string{
		"Machine spec.example.partialRef orders 1 of its 2 occurrences in the examples; give it an edge catalog row",
		"unorderedReferences excuses Machine spec.example.secretRef, which a row names, the examples show ordered, or which names no realized kind; remove it",
	}) {
		t.Fatalf("excused violations = %q", got)
	}
}
