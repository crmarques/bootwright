package cli

import (
	"encoding/json"
	"io"
	"slices"
	"strconv"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/secrets/custody"
	"github.com/crmarques/bootwright/internal/secrets/encryption"
	"github.com/crmarques/bootwright/internal/secrets/secretstore"
)

func validSecretContext(value secretstore.Context) bool {
	return value.Name != "" && value.ID != "" && value.Mode == "ready"
}

func validSecretPart(part secrets.Part) bool {
	switch part {
	case secrets.ValuePart, secrets.UsernamePart, secrets.PasswordPart, secrets.CertificatePart, secrets.PrivateKeyPart, secrets.PublicKeyPart:
		return true
	default:
		return false
	}
}

func validSecretParts(parts []secrets.Part) bool {
	seen := make(map[secrets.Part]bool, len(parts))
	for _, part := range parts {
		if !validSecretPart(part) || seen[part] {
			return false
		}
		seen[part] = true
	}
	return true
}

func validSecretIdentity(secretType, source string) bool {
	switch secretType {
	case "opaque", "token", "usernamePassword", "dockerConfigJson", "caBundle", "tlsCertificate", "sshKeyPair":
	default:
		return false
	}
	switch source {
	case "contextStore", "file", "generated":
		return true
	default:
		return false
	}
}

func validOptionalIdentifier(value *string) bool { return value == nil || *value != "" }

func validSecretCheck(result *custody.CheckResult) bool {
	if result == nil || !validSecretContext(result.Context) {
		return false
	}
	seen := make(map[string]bool, len(result.Secrets))
	for _, row := range result.Secrets {
		if row.Name == "" || seen[row.Name] || !validSecretIdentity(row.Type, row.Source) || len(row.Parts) == 0 || !validSecretParts(row.Parts) || !validOptionalIdentifier(row.Version) {
			return false
		}
		seen[row.Name] = true
		switch row.Status {
		case "available", "missing", "stale", "invalid", "unreadable":
		default:
			return false
		}
	}
	return true
}

func availableSecretCheck(result *custody.CheckResult) bool {
	if !validSecretCheck(result) {
		return false
	}
	for _, row := range result.Secrets {
		if row.Status != "available" {
			return false
		}
	}
	return true
}

func negativeSecretCheck(result *custody.CheckResult) bool {
	if !validSecretCheck(result) {
		return false
	}
	for _, row := range result.Secrets {
		if row.Status != "available" {
			return true
		}
	}
	return false
}

func validSecretList(result *custody.ListResult) bool {
	if result == nil || !validSecretContext(result.Context) {
		return false
	}
	seen := make(map[string]bool, len(result.Secrets))
	for _, row := range result.Secrets {
		if row.Name == "" || seen[row.Name] || !validSecretIdentity(row.Type, row.Source) || row.Source == "file" || len(row.Parts) == 0 || !validSecretParts(row.Parts) || !validOptionalIdentifier(row.CurrentVersion) || row.BoundVersions < 0 {
			return false
		}
		seen[row.Name] = true
		switch row.State {
		case "current", "stale", "orphaned":
		default:
			return false
		}
	}
	return true
}

func validSecretMutation(path string, result *custody.MutationResult) bool {
	if result == nil || !validSecretContext(result.Context) || result.Changed < 0 || result.Unchanged < 0 || !validSecretParts(result.Parts) {
		return false
	}
	return path == "secret generate" || result.Name != ""
}

func validComponentRef(value secretstore.ComponentRef) bool {
	return value.ID != "" && value.InterfaceVersion > 0 && value.StateVersion > 0 && value.ConfigVersion > 0
}

func validComponentStatus(value encryption.ComponentStatus) bool {
	return value.ID != "" && value.InterfaceVersion > 0 && value.StateVersion > 0 && value.ConfigVersion > 0
}

func validSelection(value secretstore.Selection) bool {
	return api.ValidLexical("name", value.Type) && validComponentRef(value.Store) && validComponentRef(value.KeyCustody)
}

func validEncryptionMutation(result *encryption.MutationResult) bool {
	return result != nil && validSecretContext(result.Context) && validSelection(result.Implementation) && result.ActiveKey != ""
}

func validEncryptionStatus(result *encryption.StatusResult) bool {
	if result == nil || result.Items.CurrentVersions < 0 || result.Items.BoundVersions < 0 || result.Items.MaterialParts < 0 || result.Items.RetainedArtifacts < 0 {
		return false
	}
	if !result.Initialized {
		return result.Implementation == nil && result.ActiveKey == nil && len(result.Keys) == 0 && result.Items == (encryption.ItemStatus{})
	}
	if result.Implementation == nil || result.Implementation.State == "" || result.ActiveKey == nil || *result.ActiveKey == "" {
		return false
	}
	if !api.ValidLexical("name", result.Implementation.Type) || !validComponentStatus(result.Implementation.Store) || !validComponentStatus(result.Implementation.KeyCustody) {
		return false
	}
	seen := make(map[string]bool, len(result.Keys))
	for _, key := range result.Keys {
		if key.ID == "" || key.State == "" || seen[key.ID] {
			return false
		}
		seen[key.ID] = true
	}
	return true
}

func writeSecretMutation(out io.Writer, path string, result *custody.MutationResult) error {
	action := strings.TrimPrefix(path, "secret ")
	name := result.Name
	if name == "" {
		name = "all"
	}
	var text display
	text.headline("OK", "Secret "+action+" complete")
	text.section("")
	text.fields(
		field{Label: "Name", Value: name},
		field{Label: "Changed", Value: strconv.Itoa(result.Changed)},
		field{Label: "Unchanged", Value: strconv.Itoa(result.Unchanged)},
		field{Label: "Parts", Value: displaySecretParts(result.Parts)},
	)
	return text.writeTo(out)
}

func writeSecretCheck(out, errOut io.Writer, command string, result *custody.CheckResult, diagnostics []diagnostic, exitCode int, jsonMode bool) error {
	presentation := displaySecretCheck(result)
	diagnostics = displayDiagnostics(diagnostics)
	if jsonMode {
		encoder := json.NewEncoder(out)
		encoder.SetEscapeHTML(false)
		return encoder.Encode(commandEnvelope{SchemaVersion: "v1alpha1", Command: escapeDisplayLine(command), OK: exitCode == 0, ExitCode: exitCode, Result: presentation, Diagnostics: diagnostics, Logs: []string{}})
	}
	if err := writeHumanDiagnostics(errOut, diagnostics); err != nil {
		return err
	}
	return writeSecretCheckText(out, result)
}

func writeSecretList(out io.Writer, command string, result *custody.ListResult, jsonMode bool) error {
	presentation := displaySecretList(result)
	if jsonMode {
		encoder := json.NewEncoder(out)
		encoder.SetEscapeHTML(false)
		return encoder.Encode(commandEnvelope{SchemaVersion: "v1alpha1", Command: escapeDisplayLine(command), OK: true, ExitCode: 0, Result: presentation, Diagnostics: []diagnostic{}, Logs: []string{}})
	}
	return writeSecretListText(out, result)
}

func writeSecretCheckText(out io.Writer, result *custody.CheckResult) error {
	var text display
	if len(result.Secrets) == 0 {
		text.headline("OK", "No declared secrets")
		return text.writeTo(out)
	}
	rows := make([][]string, 0, len(result.Secrets))
	for _, row := range sortedSecretCheckRows(result.Secrets) {
		rows = append(rows, []string{row.Name, row.Type, row.Source, displaySecretParts(row.Parts), row.Status, displayVersionOrdinal(row.Version, row.Sequence)})
	}
	text.table([]string{"NAME", "TYPE", "SOURCE", "PARTS", "STATUS", "VERSION"}, rows)
	return text.writeTo(out)
}

func writeSecretListText(out io.Writer, result *custody.ListResult) error {
	var text display
	if len(result.Secrets) == 0 {
		text.headline("OK", "No stored secrets")
		return text.writeTo(out)
	}
	rows := make([][]string, 0, len(result.Secrets))
	for _, row := range sortedSecretListRows(result.Secrets) {
		rows = append(rows, []string{row.Name, row.Type, row.Source, displaySecretParts(row.Parts), row.State, displayVersionOrdinal(row.CurrentVersion, row.CurrentSequence), strconv.Itoa(row.BoundVersions)})
	}
	text.table([]string{"NAME", "TYPE", "SOURCE", "PARTS", "STATE", "CURRENT-VERSION", "BOUND-VERSIONS"}, rows)
	return text.writeTo(out)
}

func writeSecretReveal(out io.Writer, result *custody.RevealResult, requested secrets.Part) error {
	value, present := result.Material.Part(requested)
	if !present || len(value) > secrets.MaxPartBytes || result.Material.Size() > secrets.MaxVersionBytes {
		clear(value)
		return &resultFailure{"runtime.internal", "application service returned an unsupported result", 1}
	}
	defer clear(value)
	n, err := out.Write(value)
	if err == nil && n != len(value) {
		err = io.ErrShortWrite
	}
	return err
}

func writeEncryptionMutation(out io.Writer, path string, result *encryption.MutationResult) error {
	action := "initialized"
	if path == "secret encryption rotate" {
		action = "rotated"
	} else if !result.Changed {
		action = "already initialized"
	}
	var text display
	text.headline("OK", "Secret encryption "+action)
	text.section("")
	text.fields(
		field{Label: "Type", Value: result.Implementation.Type},
		field{Label: "Store", Value: result.Implementation.Store.ID},
		field{Label: "Key custody", Value: result.Implementation.KeyCustody.ID},
		field{Label: "Active key", Value: result.ActiveKey},
	)
	return text.writeTo(out)
}

func writeEncryptionStatus(out io.Writer, command string, result *encryption.StatusResult, jsonMode bool) error {
	presentation := displayEncryptionStatus(result)
	if jsonMode {
		encoder := json.NewEncoder(out)
		encoder.SetEscapeHTML(false)
		return encoder.Encode(commandEnvelope{SchemaVersion: "v1alpha1", Command: escapeDisplayLine(command), OK: true, ExitCode: 0, Result: presentation, Diagnostics: []diagnostic{}, Logs: []string{}})
	}
	var text display
	text.headline("", "Secret encryption")
	text.section("")
	status := []field{{Label: "Initialized", Value: strconv.FormatBool(presentation.Initialized)}}
	if presentation.Implementation == nil {
		status = append(status, field{Label: "Implementation", Value: "-"}, field{Label: "Active key", Value: "-"})
	} else {
		implementation := presentation.Implementation
		activeKey := "-"
		if presentation.ActiveKey != nil {
			activeKey = *presentation.ActiveKey
		}
		status = append(status,
			field{Label: "Implementation", Value: implementation.Type},
			field{Label: "State", Value: implementation.State},
			field{Label: "Store", Value: implementation.Store.ID},
			field{Label: "Key custody", Value: implementation.KeyCustody.ID},
			field{Label: "Active key", Value: activeKey},
		)
	}
	text.fields(status...)
	text.section("Keys")
	if len(presentation.Keys) == 0 {
		text.lines([]string{"none"})
	} else {
		rows := make([][]string, 0, len(presentation.Keys))
		for _, key := range presentation.Keys {
			rows = append(rows, []string{key.ID, key.State, strconv.FormatUint(key.Seals, 10)})
		}
		text.rows(rows)
	}
	text.section("Items")
	text.fields(
		field{Label: "Current versions", Value: strconv.Itoa(presentation.Items.CurrentVersions)},
		field{Label: "Bound versions", Value: strconv.Itoa(presentation.Items.BoundVersions)},
		field{Label: "Material parts", Value: strconv.Itoa(presentation.Items.MaterialParts)},
		field{Label: "Retained artifacts", Value: strconv.Itoa(presentation.Items.RetainedArtifacts)},
		field{Label: "Cleanup required", Value: strconv.FormatBool(presentation.Items.CleanupRequired)},
	)
	return text.writeTo(out)
}

func displaySecretCheck(result *custody.CheckResult) *custody.CheckResult {
	out := &custody.CheckResult{Context: displaySecretContext(result.Context), Secrets: make([]custody.CheckRow, len(result.Secrets))}
	for i, row := range sortedSecretCheckRows(result.Secrets) {
		out.Secrets[i] = custody.CheckRow{Name: escapeDisplayLine(row.Name), Type: escapeDisplayLine(row.Type), Source: escapeDisplayLine(row.Source), Parts: displaySecretPartValues(row.Parts), Status: escapeDisplayLine(row.Status), Version: displayOptionalIdentifierPointer(row.Version)}
	}
	return out
}

func displaySecretList(result *custody.ListResult) *custody.ListResult {
	out := &custody.ListResult{Context: displaySecretContext(result.Context), Secrets: make([]custody.ListRow, len(result.Secrets))}
	for i, row := range sortedSecretListRows(result.Secrets) {
		out.Secrets[i] = custody.ListRow{Name: escapeDisplayLine(row.Name), Type: escapeDisplayLine(row.Type), Source: escapeDisplayLine(row.Source), Parts: displaySecretPartValues(row.Parts), State: escapeDisplayLine(row.State), CurrentVersion: displayOptionalIdentifierPointer(row.CurrentVersion), BoundVersions: row.BoundVersions}
	}
	return out
}

func sortedSecretCheckRows(rows []custody.CheckRow) []custody.CheckRow {
	rows = slices.Clone(rows)
	slices.SortStableFunc(rows, func(a, b custody.CheckRow) int { return strings.Compare(a.Name, b.Name) })
	return rows
}

func sortedSecretListRows(rows []custody.ListRow) []custody.ListRow {
	rows = slices.Clone(rows)
	slices.SortStableFunc(rows, func(a, b custody.ListRow) int { return strings.Compare(a.Name, b.Name) })
	return rows
}

func displaySecretContext(value secretstore.Context) secretstore.Context {
	return secretstore.Context{Name: escapeDisplayLine(value.Name), ID: escapeDisplayLine(value.ID), Mode: escapeDisplayLine(value.Mode)}
}

func displaySecretParts(parts []secrets.Part) string {
	values := displaySecretPartValues(parts)
	if len(values) == 0 {
		return "-"
	}
	text := make([]string, len(values))
	for i, value := range values {
		text[i] = string(value)
	}
	return strings.Join(text, ",")
}

func displaySecretPartValues(parts []secrets.Part) []secrets.Part {
	values := make([]secrets.Part, len(parts))
	for i, part := range parts {
		values[i] = secrets.Part(escapeDisplayLine(string(part)))
	}
	slices.Sort(values)
	return values
}

// A version is shown to a person by its per-secret ordinal. The durable
// identifier stays in JSON, which is what any tool should reference.
func displayVersionOrdinal(value *string, sequence int) string {
	if value == nil || sequence <= 0 {
		return "-"
	}
	return "v" + strconv.Itoa(sequence)
}

func displayOptionalIdentifierPointer(value *string) *string {
	if value == nil {
		return nil
	}
	display := escapeDisplayLine(*value)
	return &display
}

type componentStatus struct {
	ID               string `json:"id"`
	InterfaceVersion int    `json:"interfaceVersion"`
	StateVersion     int    `json:"stateVersion"`
	ConfigVersion    int    `json:"configVersion"`
}

type implementationStatus struct {
	Type       string          `json:"type"`
	Store      componentStatus `json:"store"`
	KeyCustody componentStatus `json:"keyCustody"`
	State      string          `json:"state"`
}

type encryptionStatusResult struct {
	Initialized    bool                  `json:"initialized"`
	Implementation *implementationStatus `json:"implementation"`
	ActiveKey      *string               `json:"activeKey"`
	Keys           []secretstore.Key     `json:"keys"`
	Items          encryption.ItemStatus `json:"items"`
}

func displayEncryptionStatus(result *encryption.StatusResult) *encryptionStatusResult {
	out := &encryptionStatusResult{Initialized: result.Initialized, ActiveKey: displayOptionalIdentifierPointer(result.ActiveKey), Keys: make([]secretstore.Key, len(result.Keys)), Items: result.Items}
	if result.Implementation != nil {
		implementation := result.Implementation
		out.Implementation = &implementationStatus{Type: escapeDisplayLine(implementation.Type), Store: displayComponent(implementation.Store), KeyCustody: displayComponent(implementation.KeyCustody), State: escapeDisplayLine(implementation.State)}
	}
	for i, key := range result.Keys {
		out.Keys[i] = secretstore.Key{ID: escapeDisplayLine(key.ID), State: escapeDisplayLine(key.State), Seals: key.Seals}
	}
	slices.SortStableFunc(out.Keys, func(a, b secretstore.Key) int { return strings.Compare(a.ID, b.ID) })
	return out
}

func displayComponent(value encryption.ComponentStatus) componentStatus {
	return componentStatus{ID: escapeDisplayLine(value.ID), InterfaceVersion: value.InterfaceVersion, StateVersion: value.StateVersion, ConfigVersion: value.ConfigVersion}
}
