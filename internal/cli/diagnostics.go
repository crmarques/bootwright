package cli

import (
	"errors"
	"regexp"
	"strings"

	"github.com/crmarques/bootwright/internal/state/desired"
)

type Diagnostic struct {
	Object      string `json:"object,omitempty"`
	Field       string `json:"field,omitempty"`
	Value       string `json:"value,omitempty"`
	Rule        string `json:"rule,omitempty"`
	Remediation string `json:"remediation,omitempty"`
	Message     string `json:"message"`
}

func diagnosticsFromError(err error) []Diagnostic {
	if err == nil {
		return nil
	}
	var validationErr desiredstate.ValidationError
	if errors.As(err, &validationErr) {
		out := make([]Diagnostic, 0, len(validationErr.Findings))
		for _, finding := range validationErr.Findings {
			out = append(out, diagnosticFromFinding(finding))
		}
		return out
	}
	return []Diagnostic{diagnosticFromMessage(err.Error())}
}

func diagnosticFromFinding(finding desiredstate.Finding) Diagnostic {
	diagnostic := diagnosticFromMessage(finding.Message)
	if finding.Object != "" {
		diagnostic.Object = finding.Object
	}
	if finding.Field != "" {
		diagnostic.Field = finding.Field
	}
	if finding.Value != "" {
		diagnostic.Value = finding.Value
	}
	if (finding.Object != "" || finding.Field != "") && diagnostic.Field != "" {
		diagnostic.Remediation = fieldRemediation(diagnostic.Object, diagnostic.Field, finding.Message)
	}
	return diagnostic
}

func suggestsRemoval(message string) bool {
	return strings.Contains(message, "only supported") ||
		strings.Contains(message, "must be empty") ||
		strings.Contains(message, "is not supported")
}

func fieldRemediation(object, field, message string) string {
	if suggestsRemoval(message) {
		if object != "" {
			return "remove " + field + " from " + object
		}
		return "remove " + field
	}
	if object != "" {
		return "set " + field + " on " + object + " to a valid value"
	}
	return "set " + field + " to a valid value"
}

var (
	quotedValue        = regexp.MustCompile(`"([^"]*)"`)
	unknownDecodeField = regexp.MustCompile(`field ([A-Za-z0-9_]+) not found in type v1alpha1\.([A-Za-z0-9_]+)`)
)

var ocpInstallFieldRemediation = map[string]string{
	"baseDomain":             "set Environment.spec.domains.base instead",
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
		Remediation: "fix the desired-state input and rerun bootwright validate",
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
	if diagnostic.Field != "" {
		diagnostic.Remediation = fieldRemediation(diagnostic.Object, diagnostic.Field, message)
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
			Rule:        fieldPath + " is not accepted on ContainerCluster install intent",
			Remediation: remediation,
			Message:     message,
		}, true
	case "StorageCephManagement":
		if field != "dnsName" {
			return Diagnostic{}, false
		}
		return renamedToDNSLabel(message, "StorageCluster", "spec.ceph.management", "StorageCluster name"), true
	case "StorageObjectGatewayPublic":
		if field != "dnsName" {
			return Diagnostic{}, false
		}
		return renamedToDNSLabel(message, "StorageObjectGateway", "spec.public", "storageClusterRef name"), true
	default:
		return Diagnostic{}, false
	}
}

func renamedToDNSLabel(message, kind, block, zoneOwner string) Diagnostic {
	return Diagnostic{
		Object:      kind,
		Field:       block + ".dnsName",
		Rule:        block + ".dnsName was replaced by dnsLabel",
		Remediation: "set " + block + ".dnsLabel to the leftmost label only; the published name is composed as <dnsLabel>.<" + zoneOwner + ">.<Environment spec.domains.storageClusters>",
		Message:     message,
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
