package compilation

import (
	"context"
	"strconv"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/desiredstate"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

type diagnosticSink struct {
	ctx         context.Context
	items       []diagnostics.Diagnostic
	seen        map[diagnosticKey]bool
	limited     bool
	undecodable map[string]bool
}

// Comparable keys retain shared strings rather than re-encoding whole
// diagnostics, so repeated provenance does not multiply its byte footprint.
type diagnosticKey struct {
	severity, code, message, field, remediation string
	source                                      diagnostics.SourceLocation
	object                                      diagnostics.ObjectIdentity
	hasSource, hasObject                        bool
}

func newDiagnostics(ctx context.Context) *diagnosticSink {
	return &diagnosticSink{ctx: ctx, seen: map[diagnosticKey]bool{}, undecodable: map[string]bool{}}
}

func (d *diagnosticSink) add(item diagnostics.Diagnostic) bool {
	if d.limited || d.ctx.Err() != nil {
		return false
	}
	key := diagnosticKey{severity: item.Severity, code: item.Code, message: item.Message, field: item.Field, remediation: item.Remediation, hasSource: item.Source != nil, hasObject: item.Object != nil}
	if item.Source != nil {
		key.source = *item.Source
	}
	if item.Object != nil {
		key.object = *item.Object
	}
	if d.seen[key] {
		return true
	}
	if len(d.items) >= desiredstate.MaxDiagnostics-1 {
		d.items = append(d.items, diagnostics.Diagnostic{Severity: "error", Code: "input.limit", Message: desiredstate.LimitMessage("returned diagnostics", desiredstate.MaxDiagnostics)})
		d.limited = true
		return false
	}
	d.seen[key] = true
	d.items = append(d.items, item)
	if item.Code == "input.limit" {
		d.limited = true
		return false
	}
	return true
}

func (d *diagnosticSink) issue(record *objectRecord, issue api.Issue) bool {
	if issue.Code == "api.reference" && record != nil && d.namesUndecodable(record.object, issue.Field) {
		return !d.stopped()
	}
	item := diagnostics.Diagnostic{Severity: "error", Code: issue.Code, Message: issue.Message, Field: issue.Field, Remediation: issue.Remediation}
	if item.Code == "" {
		item.Code = "api.invariant"
	}
	if issue.Code == "api.deferred" || issue.Code == "api.selection" {
		item.Severity = "warning"
	}
	if record != nil {
		if api.ValidLexical("name", record.object.Name()) {
			item.Object = &diagnostics.ObjectIdentity{APIVersion: api.APIVersion, Kind: string(record.object.Kind()), Name: record.object.Name()}
		}
		if location, ok := record.location(issue.Field); ok {
			item.Source = &location
		}
		if origin, sourceField, ok := record.derivedOrigin(issue.Field); ok {
			item.Message += " (from " + string(origin.object.Kind())
			if origin.isInherited(sourceField) {
				item.Message += " via Environment defaults"
			}
			item.Message += ")"
			item.Remediation = "override the field on the recipient object or correct the referenced object's source field"
		} else if record.isInherited(issue.Field) {
			item.Message += " (from Environment defaults)"
			item.Remediation = "override the field on the recipient object or correct its Environment kind default"
		} else if issue.Code == "api.reference" {
			if _, authored := record.locations[record.sourceField(issue.Field)]; !authored && valueAt(record.object.Value(), issue.Field).Present() {
				item.Message += " (defaulted)"
				item.Remediation = "set this field explicitly on the recipient object or declare the default reference target"
			}
		}
	}
	return d.add(item)
}

func (d *diagnosticSink) namesUndecodable(object api.Object, field string) bool {
	if len(d.undecodable) == 0 {
		return false
	}
	shape, value := referenceAt(object, field)
	if shape == nil || value.Type() != api.String {
		return false
	}
	for _, kind := range shape.Reference {
		if d.undecodable[string(kind)+"/"+value.Text()] {
			return true
		}
	}
	return false
}

func referenceAt(object api.Object, field string) (*api.Shape, api.Value) {
	if !strings.HasPrefix(field, "$.spec") {
		return nil, api.Value{}
	}
	shape, value, rest := api.Schema(object.Kind()), object.Spec(), field[len("$.spec"):]
	for segments := 0; rest != "" && shape != nil; segments++ {
		if segments >= fieldPathSegmentsBound {
			return nil, api.Value{}
		}
		shape = shapeForValue(shape, value)
		switch rest[0] {
		case '.':
			end := strings.IndexAny(rest[1:], ".[")
			if end < 0 {
				end = len(rest) - 1
			}
			name := rest[1 : 1+end]
			if child, ok := shape.Field(name); ok {
				shape = child.Shape
			} else if shape.Open {
				shape = shape.Element
			} else {
				return nil, api.Value{}
			}
			value, rest = value.Get(name), rest[1+end:]
		case '[':
			end := strings.IndexByte(rest, ']')
			if end < 2 || value.Type() != api.Sequence {
				return nil, api.Value{}
			}
			index, err := strconv.Atoi(rest[1:end])
			if err != nil || index < 0 || index >= value.Len() {
				return nil, api.Value{}
			}
			shape, value, rest = shape.Element, value.Items()[index], rest[end+1:]
		default:
			return nil, api.Value{}
		}
	}
	if shape == nil || rest != "" {
		return nil, api.Value{}
	}
	shape = shapeForValue(shape, value)
	if len(shape.Reference) == 0 {
		return nil, api.Value{}
	}
	return shape, value
}

func (r *objectRecord) isInherited(field string) bool {
	field = r.sourceField(field)
	for {
		if _, ok := r.inherited[field]; ok {
			return true
		}
		at := strings.LastIndexAny(field, ".[")
		if at < 0 {
			return false
		}
		field = field[:at]
	}
}

func (d *diagnosticSink) hasErrors() bool {
	for _, item := range d.items {
		if item.Severity == "error" {
			return true
		}
	}
	return false
}
func (d *diagnosticSink) stopped() bool { return d.limited || d.ctx.Err() != nil }
func (d *diagnosticSink) sorted() []diagnostics.Diagnostic {
	out := append([]diagnostics.Diagnostic{}, d.items...)
	diagnostics.Sort(out)
	return out
}

type objectRecord struct {
	object               api.Object
	authored             api.Object
	path                 string
	document             int
	locations            map[string]diagnostics.SourceLocation
	inherited            map[string]diagnostics.SourceLocation
	inheritedEnvironment *objectRecord
	remappings           []map[string]string
	derived              map[string]fieldSource
}

func (r *objectRecord) location(field string) (diagnostics.SourceLocation, bool) {
	if origin, sourceField, ok := r.derivedOrigin(field); ok {
		return origin.location(sourceField)
	}
	field = r.sourceField(field)
	requested := field
	for {
		if p, ok := r.inherited[field]; ok {
			if r.inheritedEnvironment != nil {
				origin := "$.spec.defaults." + string(r.object.Kind()) + strings.TrimPrefix(requested, "$.spec")
				if source, exists := r.inheritedEnvironment.locations[origin]; exists {
					return source, true
				}
			}
			return p, true
		}
		if p, ok := r.locations[field]; ok {
			return p, true
		}
		at := strings.LastIndexAny(field, ".[")
		if at < 0 {
			break
		}
		field = field[:at]
	}
	return diagnostics.SourceLocation{Path: r.path, Document: r.document}, true
}

const listedFactsBound = 16
const fieldPathSegmentsBound = 256
const renameReach = 2

func typeName(t api.ValueType) string {
	switch t {
	case api.String:
		return "string"
	case api.Boolean:
		return "boolean"
	case api.Integer:
		return "integer"
	case api.Number:
		return "number"
	case api.Mapping:
		return "mapping"
	case api.Sequence:
		return "list"
	}
	return "value"
}

func shapeTypeName(shape *api.Shape) string {
	if shape == nil {
		return typeName(api.Absent)
	}
	if len(shape.Alternatives) > 0 {
		names := make([]string, 0, len(shape.Alternatives))
		for _, alternative := range shape.Alternatives {
			names = append(names, typeName(alternative.Type))
		}
		return boundedList(names, " or ")
	}
	return typeName(shape.Type)
}

func withArticle(noun string) string {
	if noun != "" && strings.ContainsRune("aeiou", rune(noun[0])) {
		return "an " + noun
	}
	return "a " + noun
}

func boundedList(items []string, separator string) string {
	if len(items) <= listedFactsBound {
		return strings.Join(items, separator)
	}
	return strings.Join(items[:listedFactsBound], separator) + ", and " + strconv.Itoa(len(items)-listedFactsBound) + " more"
}

func nearestName(name string, candidates []string) (string, bool) {
	folded, foldedCount := "", 0
	for _, candidate := range candidates {
		if strings.EqualFold(candidate, name) {
			folded = candidate
			foldedCount++
		}
	}
	if foldedCount > 0 {
		return folded, foldedCount == 1
	}
	nearest, best, count := "", renameReach+1, 0
	for _, candidate := range candidates {
		switch distance := editDistance(name, candidate); {
		case distance > renameReach:
		case distance < best:
			nearest, best, count = candidate, distance, 1
		case distance == best:
			count++
		}
	}
	return nearest, count == 1
}

func editDistance(a, b string) int {
	previous := make([]int, len(b)+1)
	current := make([]int, len(b)+1)
	for j := range previous {
		previous[j] = j
	}
	for i := 1; i <= len(a); i++ {
		current[0] = i
		for j := 1; j <= len(b); j++ {
			substitution := previous[j-1]
			if a[i-1] != b[j-1] {
				substitution++
			}
			current[j] = min(previous[j]+1, current[j-1]+1, substitution)
		}
		previous, current = current, previous
	}
	return previous[len(b)]
}

func valueAt(envelope api.Value, field string) api.Value {
	if !strings.HasPrefix(field, "$.spec") && !strings.HasPrefix(field, "$.metadata") {
		return api.Value{}
	}
	value, rest := envelope, field[1:]
	for segments := 0; rest != ""; segments++ {
		if segments >= fieldPathSegmentsBound {
			return api.Value{}
		}
		switch rest[0] {
		case '.':
			end := strings.IndexAny(rest[1:], ".[")
			if end < 0 {
				end = len(rest) - 1
			}
			if end == 0 || value.Type() != api.Mapping {
				return api.Value{}
			}
			value, rest = value.Get(rest[1:1+end]), rest[1+end:]
		case '[':
			end := strings.IndexByte(rest, ']')
			if end < 2 || value.Type() != api.Sequence {
				return api.Value{}
			}
			index, err := strconv.Atoi(rest[1:end])
			if err != nil || index < 0 || index >= value.Len() {
				return api.Value{}
			}
			value, rest = value.Items()[index], rest[end+1:]
		default:
			return api.Value{}
		}
	}
	return value
}
