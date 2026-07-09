package desiredstate

import "strings"

type Finding struct {
	Object  string
	Field   string
	Value   string
	Message string
}

func diag(object, field, message string) Finding {
	return Finding{Object: object, Field: field, Message: message}
}

func diagValue(object, field, value, message string) Finding {
	return Finding{Object: object, Field: field, Value: value, Message: message}
}

func note(message string) Finding {
	return Finding{Message: message}
}

func notes(messages []string) []Finding {
	out := make([]Finding, len(messages))
	for i, message := range messages {
		out[i] = Finding{Message: message}
	}
	return out
}

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
