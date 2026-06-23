package desiredstate

import "strings"

// Finding is a structured validation failure. The validator names the owning
// object and field (and the offending value where natural) at the point the
// rule fires, so the CLI renders routable diagnostics without re-parsing the
// message text. Message keeps the exact human text and is what ValidationError
// joins for Error().
type Finding struct {
	Object  string
	Field   string
	Value   string
	Message string
}

// diag builds a structured finding naming the owning object and field. The
// message still carries the full human text; passing object/field explicitly
// is what lets the CLI route the diagnostic reliably instead of guessing from
// the message.
func diag(object, field, message string) Finding {
	return Finding{Object: object, Field: field, Message: message}
}

// diagValue is diag with an explicit offending value (used for messages whose
// value is not naturally the first quoted token, and to keep the value
// structured rather than re-extracted).
func diagValue(object, field, value, message string) Finding {
	return Finding{Object: object, Field: field, Value: value, Message: message}
}

// note is an unstructured finding: the CLI reconstructs object/field from the
// message text. Used where the owning object/field are not cleanly available
// at the emission site, and by validators not yet migrated to structured
// findings (via notes).
func note(message string) Finding {
	return Finding{Message: message}
}

// notes adapts a legacy []string of validator messages to unstructured
// findings, so a validator family can be migrated to structured findings one
// at a time while the rest keep returning plain messages.
func notes(messages []string) []Finding {
	out := make([]Finding, len(messages))
	for i, message := range messages {
		out[i] = Finding{Message: message}
	}
	return out
}

// ValidationError aggregates the validator's findings. It is the error type
// Validate returns; the CLI builds routable Object/Field/Value diagnostics from
// these findings for human and JSON output (see internal/cli/diagnostics.go).
type ValidationError struct {
	Findings []Finding
}

func (e ValidationError) Error() string {
	messages := make([]string, len(e.Findings))
	for i, finding := range e.Findings {
		messages[i] = finding.Message
	}
	return strings.Join(messages, "; ")
}

func newValidationError(findings []Finding) ValidationError {
	return ValidationError{Findings: append([]Finding(nil), findings...)}
}
