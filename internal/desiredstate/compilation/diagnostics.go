package compilation

import (
	"context"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/desiredstate"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

type diagnosticSink struct {
	ctx     context.Context
	items   []diagnostics.Diagnostic
	seen    map[diagnosticKey]bool
	limited bool
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
	return &diagnosticSink{ctx: ctx, seen: map[diagnosticKey]bool{}}
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
		d.items = append(d.items, diagnostics.Diagnostic{Severity: "error", Code: "input.limit", Message: "diagnostics exceed the ceiling of 1000"})
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
			if _, authored := record.locations[record.sourceField(issue.Field)]; !authored {
				item.Message += " (defaulted)"
				item.Remediation = "set this field explicitly on the recipient object or declare the default reference target"
			}
		}
	}
	return d.add(item)
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
