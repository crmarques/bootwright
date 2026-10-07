package cli

import (
	"context"
	"encoding/json"
	"io"
	"strconv"
	"strings"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/managedos/media"
)

type mediaListPresentation struct {
	Media []mediaRowPresentation `json:"media"`
}

// mediaRowPresentation is one image of the media JSON result. Name, Frozen and
// ReservedBy are always present; Size, SHA256, Source and Added whenever the
// image's record could be read; Verified and Computed only when a
// verification or a computed digest exists; Reason exactly when the image
// failed.
type mediaRowPresentation struct {
	Name       string   `json:"name"`
	Size       *int64   `json:"size,omitempty"`
	SHA256     string   `json:"sha256,omitempty"`
	Source     string   `json:"source,omitempty"`
	Added      string   `json:"added,omitempty"`
	Frozen     bool     `json:"frozen"`
	ReservedBy []string `json:"reservedBy"`
	Verified   string   `json:"verified,omitempty"`
	Computed   string   `json:"computed,omitempty"`
	Reason     string   `json:"reason,omitempty"`
}

func (mediaListPresentation) documentedResult() {}

func validMediaMutation(result *media.MutationResult) bool {
	if result == nil || result.Name == "" || result.Size < 0 {
		return false
	}
	return result.Outcome == "stored" || result.Outcome == "replaced" || result.Outcome == "deleted"
}

func writeMediaMutation(out io.Writer, result *media.MutationResult) error {
	var text display
	text.headline("OK", "Media "+result.Outcome)
	text.section("")
	values := []field{{Label: "Name", Value: result.Name}}
	if result.Outcome != "deleted" || result.SHA256 != "" {
		values = append(values,
			field{Label: "Size", Value: strconv.FormatInt(result.Size, 10)},
			field{Label: "Digest", Value: "sha256:" + result.SHA256},
		)
	}
	text.fields(values...)
	return text.writeTo(out)
}

func writeMediaList(out io.Writer, command string, result *media.ListResult, jsonMode bool) error {
	if jsonMode {
		encoder := json.NewEncoder(out)
		encoder.SetEscapeHTML(false)
		return encoder.Encode(commandEnvelope{
			SchemaVersion: "v1alpha1", Command: escapeDisplayLine(command), OK: true, ExitCode: 0,
			Result: displayMediaList(result), Diagnostics: []diagnostic{}, Logs: []string{},
		})
	}
	var text display
	if len(result.Media) == 0 {
		text.headline("OK", "No stored media")
		return text.writeTo(out)
	}
	headers := []string{"NAME", "SIZE", "DIGEST", "ADDED", "RESERVED", "STATE"}
	if result.Checksums {
		headers = []string{"NAME", "SIZE", "DIGEST", "COMPUTED", "ADDED", "RESERVED", "STATE"}
	}
	rows := make([][]string, 0, len(result.Media))
	failed := []field{}
	for _, row := range result.Media {
		size, digest, added := "-", "-", "-"
		if row.SHA256 != "" {
			size, digest, added = strconv.FormatInt(row.Size, 10), "sha256:"+row.SHA256, row.Added
		}
		cells := []string{row.Name, size, digest}
		if result.Checksums {
			cells = append(cells, mediaDigest(row.Computed))
		}
		cells = append(cells, added, displayValue(strings.Join(row.ReservedBy, ",")), mediaStateToken(row))
		rows = append(rows, cells)
		if row.Verified == "failed" {
			failed = append(failed, field{Label: row.Name, Value: row.Failure})
		}
	}
	text.table(headers, rows)
	if len(failed) != 0 {
		text.section("Failed")
		text.fields(failed...)
		text.section("")
		text.lines([]string{"Remove a failed image with bootwright media delete --name <name> or replace it with " +
			"bootwright media add --name <name> --from-file <path>, or --from-url <url> --sha256 <digest>; repeat bootwright media list --checksums for one that changed while it was read."})
	}
	return text.writeTo(out)
}

func mediaDigest(digest string) string {
	if digest == "" {
		return "-"
	}
	return "sha256:" + digest
}

// mediaStateToken reports whether an image's bytes were just proved to match
// its record, were found not to, could not be read, or were not read. Whether
// a context reserves the image is its own column, so it never hides the
// verification.
func mediaStateToken(row media.MediaRow) string {
	switch row.Verified {
	case "mismatch":
		return "mismatch"
	case "failed":
		return "failed"
	case "ok":
		return "verified"
	}
	return "stored"
}

func displayMediaList(result *media.ListResult) mediaListPresentation {
	rows := make([]mediaRowPresentation, 0, len(result.Media))
	for _, row := range result.Media {
		reserving := make([]string, 0, len(row.ReservedBy))
		for _, name := range row.ReservedBy {
			reserving = append(reserving, escapeDisplayLine(name))
		}
		presented := mediaRowPresentation{
			Name: escapeDisplayLine(row.Name), SHA256: escapeDisplayLine(row.SHA256),
			Source: escapeDisplayLine(row.Source), Added: escapeDisplayLine(row.Added),
			Frozen: row.Frozen, ReservedBy: reserving,
			Verified: escapeDisplayLine(row.Verified), Computed: escapeDisplayLine(row.Computed),
			Reason: escapeDisplayLine(row.Failure),
		}
		if row.SHA256 != "" {
			size := row.Size
			presented.Size = &size
		}
		rows = append(rows, presented)
	}
	return mediaListPresentation{Media: rows}
}

// MediaChangePresenter shows the stored image a media confirmation would
// replace or delete, on standard output before the prompt on standard error.
type MediaChangePresenter struct{ out io.Writer }

func NewMediaChangePresenter(out io.Writer) *MediaChangePresenter {
	return &MediaChangePresenter{out: out}
}

func (p *MediaChangePresenter) PresentMediaChange(ctx context.Context, change media.Change) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if p == nil || p.out == nil {
		return diagnostics.NewFailure("runtime.internal", "media change presentation is not configured", "")
	}
	var text display
	if change.Action == media.ReplaceChange {
		text.headline("", "Media replacement")
	} else {
		text.headline("", "Media deletion")
	}
	text.section("")
	values := []field{{Label: "Name", Value: change.Name}}
	switch {
	case change.Stored && change.Readable:
		values = append(values,
			field{Label: "Size", Value: strconv.FormatInt(change.Entry.Size, 10)},
			field{Label: "Digest", Value: "sha256:" + change.Entry.SHA256},
			field{Label: "Added", Value: change.Entry.Added},
			field{Label: "Source", Value: change.Entry.Source},
		)
	case change.Stored:
		values = append(values, field{Label: "Record", Value: "unreadable"})
	}
	if change.Action == media.ReplaceChange {
		values = append(values, field{Label: "New source", Value: change.NewOrigin})
	} else if change.Retained {
		values = append(values, field{Label: "Retained", Value: "the stage an interrupted add kept, removed too"})
	}
	text.fields(values...)
	if err := text.writeTo(p.out); err != nil {
		return diagnostics.NewFailure("runtime.internal", "the stored image could not be shown before its confirmation", "")
	}
	return nil
}
