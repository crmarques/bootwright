package compilation

import (
	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/desiredstate"
)

func selectingEnvironment(documents []desiredstate.Document, parseDiagnostics []desiredstate.Diagnostic, ds *diagnostics) *objectRecord {
	var selected *objectRecord
	candidates := []desiredstate.Document{}
	invalid := []desiredstate.Document{}
	for _, document := range documents {
		if ds.stopped() {
			return nil
		}
		if nodeText(mappingNode(documentBody(document), "kind")) != string(api.Environment) {
			continue
		}
		// A malformed or foreign-version scan candidate cannot supply scope. Its
		// errors are relevant only if its file is selected, or no Environment can
		// be admitted. The probe's diagnostics are bounded and discarded here.
		probe := newDiagnostics(ds.ctx)
		record := decodeDocument(document, probe)
		if record == nil {
			invalid = append(invalid, document)
			continue
		}
		selected = record
		candidates = append(candidates, document)
	}
	if ds.stopped() {
		return nil
	}
	if len(candidates) == 1 {
		return selected
	}
	for _, diagnostic := range parseDiagnostics {
		ds.add(diagnostic)
	}
	if len(candidates) == 0 {
		for _, document := range invalid {
			if ds.stopped() {
				break
			}
			decodeDocument(document, ds)
		}
		if len(invalid) == 0 {
			ds.add(desiredstate.Diagnostic{Severity: "error", Code: "api.environment", Message: "exactly one Environment is required"})
		}
	} else {
		for _, document := range candidates {
			location := desiredstate.SourceLocation{Path: document.Path, Document: document.Index}
			if !ds.add(desiredstate.Diagnostic{Severity: "error", Code: "api.environment", Message: "exactly one Environment is required", Source: &location}) {
				break
			}
		}
	}
	return nil
}
