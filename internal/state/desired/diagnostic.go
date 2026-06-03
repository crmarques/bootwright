package desiredstate

import (
	"errors"
	"regexp"
	"strings"
)

// Diagnostic is the user-facing shape for a validation failure. Message keeps
// the exact historical text; the other fields make CLI and CI output easier to
// route back to the owning object and field.
type Diagnostic struct {
	Object      string `json:"object,omitempty"`
	Field       string `json:"field,omitempty"`
	Value       string `json:"value,omitempty"`
	Rule        string `json:"rule,omitempty"`
	Remediation string `json:"remediation,omitempty"`
	Message     string `json:"message"`
}

type ValidationError struct {
	Diagnostics []Diagnostic
}

func (e ValidationError) Error() string {
	messages := make([]string, 0, len(e.Diagnostics))
	for _, diagnostic := range e.Diagnostics {
		messages = append(messages, diagnostic.Message)
	}
	return strings.Join(messages, "; ")
}

func newValidationError(messages []string) ValidationError {
	diagnostics := make([]Diagnostic, 0, len(messages))
	for _, message := range messages {
		diagnostics = append(diagnostics, diagnosticFromMessage(message))
	}
	return ValidationError{Diagnostics: diagnostics}
}

// Diagnostics extracts structured validation diagnostics from err. Wrapped
// validation errors are supported so callers can add command context without
// losing machine-readable details.
func Diagnostics(err error) []Diagnostic {
	if err == nil {
		return nil
	}
	var validationErr ValidationError
	if errors.As(err, &validationErr) {
		return append([]Diagnostic(nil), validationErr.Diagnostics...)
	}
	return []Diagnostic{diagnosticFromMessage(err.Error())}
}

var (
	quotedValue        = regexp.MustCompile(`"([^"]*)"`)
	unknownDecodeField = regexp.MustCompile(`field ([A-Za-z0-9_]+) not found in type v1alpha1\.([A-Za-z0-9_]+)`)
)

var ocpInstallFieldRemediation = map[string]string{
	"baseDomain":             "set Environment.spec.baseDomain instead",
	"imageDigestSources":     "set Environment.spec.registries.imageDigestSources instead",
	"installConfigOverrides": "remove spec.install.installConfigOverrides; rendered installer files are generated output",
	"agentConfigOverrides":   "remove spec.install.agentConfigOverrides; rendered installer files are generated output",
	"sshKeyRef":              "set ContainerCluster.spec.install.nodeSSH instead",
	"clusterAdminSSH":        "set ContainerCluster.spec.install.nodeSSH instead",
}

func diagnosticFromMessage(message string) Diagnostic {
	if diagnostic, ok := diagnosticFromDecodeMessage(message); ok {
		return diagnostic
	}
	diagnostic := Diagnostic{
		Message:     message,
		Rule:        message,
		Remediation: "fix the desired-state input and rerun bootwright check syntax",
	}
	parts := strings.Fields(message)
	if len(parts) == 0 {
		return diagnostic
	}
	if strings.Contains(parts[0], "/") {
		diagnostic.Object = strings.TrimSuffix(parts[0], ":")
		if len(parts) > 1 && looksLikeFieldPath(parts[1]) {
			diagnostic.Field = strings.Trim(parts[1], ":,")
			diagnostic.Rule = strings.TrimSpace(strings.TrimPrefix(message, parts[0]+" "+parts[1]))
		}
	} else if object, field, ok := splitKindField(parts[0]); ok {
		diagnostic.Object = object
		diagnostic.Field = field
		diagnostic.Rule = strings.TrimSpace(strings.TrimPrefix(message, parts[0]))
	}
	if match := quotedValue.FindStringSubmatch(message); len(match) == 2 {
		diagnostic.Value = match[1]
	}
	if diagnostic.Object != "" && diagnostic.Field != "" {
		diagnostic.Remediation = "set " + diagnostic.Field + " on " + diagnostic.Object + " to a valid value"
	} else if diagnostic.Field != "" {
		diagnostic.Remediation = "set " + diagnostic.Field + " to a valid value"
	}
	if diagnostic.Rule == "" {
		diagnostic.Rule = message
	}
	return diagnostic
}

func diagnosticFromDecodeMessage(message string) (Diagnostic, bool) {
	match := unknownDecodeField.FindStringSubmatch(message)
	if len(match) != 3 {
		return Diagnostic{}, false
	}
	field, typeName := match[1], match[2]
	switch typeName {
	case "OCPInstallSpec":
		fieldPath := "spec.install." + field
		remediation := "remove " + fieldPath + " or move the fact to the desired-state object that owns it"
		if known, ok := ocpInstallFieldRemediation[field]; ok {
			remediation = known
		}
		return Diagnostic{
			Object:      "ContainerCluster",
			Field:       fieldPath,
			Value:       field,
			Rule:        fieldPath + " is not accepted on ContainerCluster install intent",
			Remediation: remediation,
			Message:     message,
		}, true
	default:
		return Diagnostic{}, false
	}
}

func splitKindField(token string) (string, string, bool) {
	for _, marker := range []string{".metadata.", ".spec."} {
		if idx := strings.Index(token, marker); idx > 0 {
			return token[:idx], token[idx+1:], true
		}
	}
	return "", "", false
}

func looksLikeFieldPath(token string) bool {
	token = strings.Trim(token, ":,")
	return strings.HasPrefix(token, "metadata.") || strings.HasPrefix(token, "spec.")
}
