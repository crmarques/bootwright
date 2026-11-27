package cli

import (
	"context"
	"strconv"
	"strings"

	"github.com/crmarques/bootwright/internal/infrastructureservices/artifactcatalog"
	"github.com/spf13/cobra"
)

type ArtifactCatalogService interface {
	Add(context.Context, artifactcatalog.Entry, string) (artifactcatalog.Entry, bool, error)
	List(context.Context, string) ([]artifactcatalog.Row, error)
	Delete(context.Context, string, bool) (artifactcatalog.Entry, error)
}

func artifactCommands() []commandSpec {
	return []commandSpec{
		available(commandSpec{path: "catalog artifact add", contextFree: true, short: "Register an immutable file or deferred HTTPS origin",
			example: "--name helm-linux-amd64 --from-file ./helm.tar.gz", results: []jsonResult{artifactMutation{}}, render: renderArtifacts(),
			flags:    []flagSpec{nameFlag(), fileFlag("from-file", "Import local bytes now"), undisclosedFlag("origin-url", "Register an HTTPS origin for the mirror host to fetch at apply"), emptyFlag("sha256", "Expected SHA-256"), emptyFlag("size", "Expected byte count")},
			validate: validateArtifactAdd, invoke: Services.invokeArtifactAdd}),
		available(commandSpec{path: "catalog artifact list", contextFree: true, short: "List registered artifacts",
			results: []jsonResult{artifactList{}}, render: renderArtifacts(), invoke: Services.invokeArtifactList}),
		available(commandSpec{path: "catalog artifact show", contextFree: true, short: "Show one registered artifact",
			example: "--name helm-linux-amd64", results: []jsonResult{artifactList{}}, render: renderArtifacts(),
			flags: []flagSpec{nameFlag()}, validate: validateArtifactName, invoke: Services.invokeArtifactList}),
		available(commandSpec{path: "catalog artifact delete", contextFree: true, short: "Delete an unreserved artifact",
			example: "--name helm-linux-amd64 --yes", results: []jsonResult{artifactMutation{}}, render: renderArtifacts(),
			flags: []flagSpec{nameFlag(), confirmationFlag()}, validate: validateArtifactDelete, invoke: Services.invokeArtifactDelete}),
	}
}

func validateArtifactName(command *cobra.Command) string {
	if command.Flags().Changed("name") && !artifactcatalog.ValidName(stringValue(command.Flags(), "name")) {
		return "--name must be a valid Bootwright object name"
	}
	return ""
}

func validateArtifactAdd(command *cobra.Command) string {
	if issue := validateArtifactName(command); issue != "" {
		return issue
	}
	flags := command.Flags()
	local := flags.Changed("from-file")
	remote := flags.Changed("origin-url")
	if local == remote || local && stringValue(flags, "from-file") == "" {
		return "select exactly one of --from-file and --origin-url"
	}
	if flags.Changed("sha256") && !artifactcatalog.ValidDigest(stringValue(flags, "sha256")) {
		return "--sha256 must be 64 lowercase hexadecimal characters"
	}
	if flags.Changed("size") {
		size, err := strconv.ParseInt(stringValue(flags, "size"), 10, 64)
		if err != nil || size < 1 || size > artifactcatalog.MaxBytes {
			return "--size must be a positive byte count up to 512 MiB"
		}
	}
	if remote && (!artifactcatalog.ValidURL(stringValue(flags, "origin-url")) || !flags.Changed("sha256") || !flags.Changed("size")) {
		return "--origin-url requires credential-free HTTPS on port 443, --sha256 and --size"
	}
	return ""
}

func validateArtifactDelete(command *cobra.Command) string {
	if selectedJSON(command) && !boolValue(command.Flags(), "yes") {
		return structuredLabel(command) + " artifact deletion requires --yes"
	}
	return validateArtifactName(command)
}

type artifactMutation struct {
	Name    string `json:"name"`
	Outcome string `json:"outcome"`
	Size    int64  `json:"size"`
	SHA256  string `json:"sha256"`
}

func (artifactMutation) documentedResult() {}

type artifactList struct {
	Artifacts []artifactRow `json:"artifacts"`
}

func (artifactList) documentedResult() {}

type artifactRow struct {
	Name       string   `json:"name"`
	Source     string   `json:"source"`
	OriginURL  string   `json:"originURL,omitempty"`
	Size       int64    `json:"size"`
	SHA256     string   `json:"sha256"`
	ReservedBy []string `json:"reservedBy"`
}

func (s Services) invokeArtifactAdd(ctx context.Context, values *requestValues, _ []string) (commandResult, error) {
	if s.ArtifactCatalog == nil {
		return commandResult{}, errMissingService
	}
	size, _ := strconv.ParseInt(values.text("size"), 10, 64)
	entry := artifactcatalog.Entry{Name: values.text("name"), SHA256: values.text("sha256"), Size: size, OriginURL: values.text("origin-url")}
	saved, created, err := s.ArtifactCatalog.Add(ctx, entry, values.text("from-file"))
	outcome := "unchanged"
	if created {
		outcome = "registered"
	}
	return artifactResult(saved, outcome, err)
}

func (s Services) invokeArtifactList(ctx context.Context, values *requestValues, _ []string) (commandResult, error) {
	if s.ArtifactCatalog == nil {
		return commandResult{}, errMissingService
	}
	rows, err := s.ArtifactCatalog.List(ctx, values.text("name"))
	if err != nil {
		return commandResult{}, err
	}
	result := artifactList{Artifacts: make([]artifactRow, 0, len(rows))}
	for _, row := range rows {
		result.Artifacts = append(result.Artifacts, artifactRow{
			Name: row.Name, Source: row.Source, OriginURL: row.OriginURL, Size: row.Size,
			SHA256: row.SHA256, ReservedBy: row.Holders,
		})
	}
	return commandResult{artifact: result}, nil
}

func (s Services) invokeArtifactDelete(ctx context.Context, values *requestValues, _ []string) (commandResult, error) {
	if s.ArtifactCatalog == nil {
		return commandResult{}, errMissingService
	}
	entry, err := s.ArtifactCatalog.Delete(ctx, values.text("name"), values.boolean("yes"))
	return artifactResult(entry, "deleted", err)
}

func artifactResult(entry artifactcatalog.Entry, outcome string, err error) (commandResult, error) {
	if err != nil {
		return commandResult{}, err
	}
	return commandResult{artifact: artifactMutation{Name: entry.Name, Outcome: outcome, Size: entry.Size, SHA256: entry.SHA256}}, nil
}

func renderArtifacts() commandRenderer {
	return commandRenderer{
		structured: structuredShown(func(result commandResult) (jsonResult, []diagnostic, bool) {
			return result.artifact, nil, result.artifact != nil
		}),
		human: func(r *Runner, call renderCall) (bool, error) {
			var text display
			switch result := call.result.artifact.(type) {
			case artifactMutation:
				text.headline("OK", "Artifact "+result.Outcome)
				text.fields(field{Label: "Name", Value: result.Name}, field{Label: "Size", Value: mediaSize(result.Size)}, field{Label: "SHA-256", Value: result.SHA256})
			case artifactList:
				text.headline("", "Artifacts on this controller")
				if len(result.Artifacts) == 0 {
					text.lines([]string{"No registered artifacts."})
				}
				for _, row := range result.Artifacts {
					text.section(row.Name)
					fields := []field{{Label: "Source", Value: row.Source}, {Label: "Size", Value: mediaSize(row.Size)}, {Label: "SHA-256", Value: row.SHA256}}
					if row.OriginURL != "" {
						fields = append(fields, field{Label: "Origin", Value: row.OriginURL})
					}
					if len(row.ReservedBy) != 0 {
						fields = append(fields, field{Label: "Reserved by", Value: strings.Join(row.ReservedBy, ", ")})
					}
					text.fields(fields...)
				}
			default:
				return false, nil
			}
			return true, text.writeTo(r.config.Out)
		},
	}
}
