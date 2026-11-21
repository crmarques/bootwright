package cli

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"unicode/utf8"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/workspace/contexts"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func writeStructuredResult(out io.Writer, command string, result jsonResult, reported []diagnostic, logs []string, code int) error {
	return writeEnvelope(out, commandEnvelope{SchemaVersion: "v1alpha1", Command: escapeDisplayLine(command), OK: code == 0, ExitCode: code, Result: result, Diagnostics: displayDiagnostics(reported), Logs: displayLines(logs)})
}

type contextSummaryPresentation struct {
	Name       string                    `json:"name"`
	Mode       string                    `json:"mode"`
	Current    bool                      `json:"current"`
	Configured bool                      `json:"configured"`
	Unfinished *unfinishedOperationShown `json:"unfinished,omitempty"`
	Unreadable bool                      `json:"operationsUnreadable,omitempty"`
}

type unfinishedOperationShown struct {
	ID    string `json:"id"`
	Verb  string `json:"verb"`
	State string `json:"state"`
	Build string `json:"build,omitempty"`
}

func contextSummary(summary contexts.Summary) contextSummaryPresentation {
	shown := contextSummaryPresentation{Name: escapeDisplayLine(summary.Name), Mode: escapeDisplayLine(string(summary.Mode)), Current: summary.Current, Configured: summary.Configured, Unreadable: summary.OperationsUnreadable}
	if unfinished := summary.Unfinished; unfinished != nil {
		shown.Unfinished = &unfinishedOperationShown{ID: escapeDisplayLine(unfinished.Operation), Verb: escapeDisplayLine(unfinished.Verb),
			State: escapeDisplayLine(unfinished.State), Build: escapeDisplayLine(unfinished.Build)}
	}
	return shown
}

type contextPresentation struct {
	Context contextSummaryPresentation `json:"context"`
}

func (contextPresentation) documentedResult() {}

type contextListPresentation struct {
	Contexts []contextSummaryPresentation `json:"contexts"`
}

func (contextListPresentation) documentedResult() {}

type contextAdmissionPresentation struct {
	Context      contextSummaryPresentation `json:"context"`
	Counts       admissionCounts            `json:"counts"`
	FilesCopied  int                        `json:"filesCopied"`
	InputChanged bool                       `json:"inputChanged"`
	Checked      bool                       `json:"checked,omitempty"`
}

func (contextAdmissionPresentation) documentedResult() {}

type contextDeletionPresentation struct {
	Name                 string   `json:"name"`
	Outcome              string   `json:"outcome"`
	CurrentCleared       bool     `json:"currentCleared"`
	OrphansAbandoned     bool     `json:"orphansAbandoned"`
	ReleasedReservations []string `json:"releasedReservations"`
}

func (contextDeletionPresentation) documentedResult() {}

type secretMutationPresentation struct {
	Context        resultContext `json:"context"`
	Name           string        `json:"name,omitempty"`
	Changed        int           `json:"changed"`
	Unchanged      int           `json:"unchanged"`
	ChangedNames   []string      `json:"changedNames"`
	UnchangedNames []string      `json:"unchangedNames"`
	Parts          []string      `json:"parts"`
}

func (secretMutationPresentation) documentedResult() {}

type rebindPresentation struct {
	Outcome      string `json:"outcome"`
	Bindings     int    `json:"bindings"`
	Reservations int    `json:"reservations"`
}

func (rebindPresentation) documentedResult() {}

type mediaMutationPresentation struct {
	Name    string `json:"name"`
	Outcome string `json:"outcome"`
	Size    int64  `json:"size"`
	SHA256  string `json:"sha256,omitempty"`
}

func (mediaMutationPresentation) documentedResult() {}

type encryptionSelectionPresentation struct {
	Type       string          `json:"type"`
	Store      componentStatus `json:"store"`
	KeyCustody componentStatus `json:"keyCustody"`
}

type encryptionMutationPresentation struct {
	Context             resultContext                   `json:"context"`
	Implementation      encryptionSelectionPresentation `json:"implementation"`
	ActiveKey           string                          `json:"activeKey"`
	Changed             bool                            `json:"changed"`
	RetiredKeys         []string                        `json:"retiredKeys"`
	ReencryptedVersions int                             `json:"reencryptedVersions"`
	ReencryptedParts    int                             `json:"reencryptedParts"`
}

func (encryptionMutationPresentation) documentedResult() {}

type exportPresentation struct {
	MediaType string `json:"mediaType"`
	Encoding  string `json:"encoding"`
	Content   string `json:"content"`
}

type exportResult struct {
	Export exportPresentation `json:"export"`
}

func (exportResult) documentedResult() {}

func exportedResult(mediaType string, value []byte) exportResult {
	encoding, content := "utf-8", string(value)
	if !exportableText(value) {
		encoding, content = "base64", base64.StdEncoding.EncodeToString(value)
	}
	return exportResult{Export: exportPresentation{MediaType: mediaType, Encoding: encoding, Content: content}}
}

func exportableText(value []byte) bool {
	if !utf8.Valid(value) {
		return false
	}
	for _, r := range string(value) {
		if r < 0x20 && r != '\t' && r != '\n' && r != '\r' || r >= 0x7f && r <= 0x9f {
			return false
		}
	}
	return true
}

type helpCommandPresentation struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type helpFlagPresentation struct {
	Name        string `json:"name"`
	Shorthand   string `json:"shorthand,omitempty"`
	Type        string `json:"type"`
	Default     string `json:"default"`
	Description string `json:"description"`
}

type helpPresentation struct {
	Command     string                    `json:"command"`
	Usage       string                    `json:"usage"`
	Description string                    `json:"description"`
	Commands    []helpCommandPresentation `json:"commands"`
	Flags       []helpFlagPresentation    `json:"flags"`
}

func (helpPresentation) documentedResult() {}

func (r *Runner) writeSelectedHelp(command *cobra.Command) error {
	if !structuredFormat(r.config.Presentation.Format) {
		return writeHelp(r.config.Out, command)
	}
	path := strings.TrimSpace(strings.TrimPrefix(command.CommandPath(), "bootwright"))
	description := command.Long
	if description == "" {
		description = command.Short
	}
	shown := helpPresentation{Command: escapeDisplayLine(path), Usage: escapeDisplayLine(helpUsage(command)), Description: escapeDisplayLine(description), Commands: []helpCommandPresentation{}, Flags: []helpFlagPresentation{}}
	for _, child := range command.Commands() {
		if !child.Hidden {
			shown.Commands = append(shown.Commands, helpCommandPresentation{Name: escapeDisplayLine(child.Name()), Description: escapeDisplayLine(child.Short)})
		}
	}
	command.Flags().AddFlagSet(command.InheritedFlags())
	command.Flags().VisitAll(func(flag *pflag.Flag) {
		if !flag.Hidden && acceptsFlag(command.Annotations, flag.Name) {
			description := flag.Usage
			shown.Flags = append(shown.Flags, helpFlagPresentation{Name: flag.Name, Shorthand: flag.Shorthand, Type: flag.Value.Type(), Default: escapeDisplayLine(flag.DefValue), Description: escapeDisplayLine(description)})
		}
	})
	return writeStructuredResult(r.config.Out, r.config.Presentation.Command, shown, nil, nil, 0)
}

type versionPresentation struct {
	Version            string `json:"version"`
	Commit             string `json:"commit"`
	Source             string `json:"source"`
	Go                 string `json:"go"`
	Target             string `json:"target"`
	DependencyBundle   string `json:"dependencyBundle"`
	VulnerabilityCheck string `json:"vulnerabilityCheck"`
}

func (versionPresentation) documentedResult() {}

func (r *Runner) writeSelectedVersion() error {
	if !structuredFormat(r.config.Presentation.Format) {
		return writeVersion(r.config.Out, r.config.BuildInfo)
	}
	i := r.config.BuildInfo
	commit := strings.ToLower(strings.TrimSpace(i.Commit))
	if len(commit) < 7 || len(commit) > 64 || !isHex(commit) {
		commit = "unknown"
	}
	source := strings.ToLower(strings.TrimSpace(i.Source))
	if source != "clean" && source != "modified" {
		source = "unknown"
	}
	bundle := strings.TrimSpace(i.DependencyBundle)
	if len(bundle) != 71 || !strings.HasPrefix(bundle, "sha256:") || !isHex(bundle[7:]) || bundle != strings.ToLower(bundle) {
		bundle = "none"
	}
	shown := versionPresentation{Version: escapeDisplayLine(buildValue(i.Version, "devel")), Commit: commit, Source: source, Go: escapeDisplayLine(buildValue(i.GoVersion, "unknown")), Target: escapeDisplayLine(buildValue(i.GOOS, "unknown") + "/" + buildValue(i.GOARCH, "unknown")), DependencyBundle: bundle, VulnerabilityCheck: vulnerabilityVerdict(i.VulnerabilityCheck)}
	return writeStructuredResult(r.config.Out, "version", shown, nil, nil, 0)
}

type schemaPresentation struct {
	Format string          `json:"format"`
	Schema json.RawMessage `json:"schema"`
}

func (schemaPresentation) documentedResult() {}

func (r *Runner) writeSelectedSchema() error {
	data, err := api.JSONSchema()
	if err != nil {
		return err
	}
	if !structuredFormat(r.config.Presentation.Format) {
		_, err = r.config.Out.Write(data)
		return err
	}
	return writeStructuredResult(r.config.Out, "catalog schema", schemaPresentation{Format: "json-schema", Schema: json.RawMessage(bytes.TrimSpace(data))}, nil, nil, 0)
}

type billOfMaterialsPresentation struct {
	Format string          `json:"format"`
	BOM    json.RawMessage `json:"bom"`
}

func (billOfMaterialsPresentation) documentedResult() {}

func (r *Runner) writeSelectedBillOfMaterials() error {
	if r.config.BillOfMaterials == nil {
		return errors.New("this executable composes no bill of materials")
	}
	bom, err := r.config.BillOfMaterials()
	if err != nil {
		return err
	}
	data, err := cycloneDX(bom)
	if err != nil {
		return err
	}
	if !structuredFormat(r.config.Presentation.Format) {
		_, err = r.config.Out.Write(data)
		return err
	}
	return writeStructuredResult(r.config.Out, "sbom", billOfMaterialsPresentation{Format: "cyclonedx-json", BOM: json.RawMessage(bytes.TrimSpace(data))}, nil, nil, 0)
}

func (r *Runner) writeSelectedCompletion(root *cobra.Command, path string, noDescriptions bool) error {
	if !structuredFormat(r.config.Presentation.Format) {
		return writeCompletion(r.config.Out, root, strings.TrimPrefix(path, "completion "), noDescriptions)
	}
	var script bytes.Buffer
	if err := writeCompletion(&script, root, strings.TrimPrefix(path, "completion "), noDescriptions); err != nil {
		return err
	}
	return writeStructuredResult(r.config.Out, path, exportedResult("text/plain", script.Bytes()), nil, nil, 0)
}
