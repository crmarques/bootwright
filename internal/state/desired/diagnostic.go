package desiredstate

import "strings"

// ValidationError aggregates the validator's failure messages. It is the error
// type Validate returns; the CLI reconstructs routable Object/Field/Value
// diagnostics from these messages for human and JSON output (see
// internal/cli/diagnostics.go), so the validator stays free of presentation
// shaping.
type ValidationError struct {
	Messages []string
}

func (e ValidationError) Error() string {
	return strings.Join(e.Messages, "; ")
}

func newValidationError(messages []string) ValidationError {
	return ValidationError{Messages: append([]string(nil), messages...)}
}
