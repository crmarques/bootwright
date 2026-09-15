package cli

import (
	"encoding/json"
	"io"
	"strconv"

	"github.com/crmarques/bootwright/internal/managedos/media"
)

type mediaListPresentation struct {
	Media []mediaRowPresentation `json:"media"`
}

type mediaRowPresentation struct {
	Name     string `json:"name"`
	Size     int64  `json:"size"`
	SHA256   string `json:"sha256"`
	Source   string `json:"source"`
	Added    string `json:"added"`
	Frozen   bool   `json:"frozen"`
	Verified string `json:"verified"`
}

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
	if result.Outcome != "deleted" {
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
	headers := []string{"NAME", "SIZE", "DIGEST", "ADDED", "STATE"}
	rows := make([][]string, 0, len(result.Media))
	for _, row := range result.Media {
		rows = append(rows, []string{row.Name, strconv.FormatInt(row.Size, 10), "sha256:" + row.SHA256, row.Added, mediaStateToken(row)})
	}
	text.table(headers, rows)
	return text.writeTo(out)
}

// mediaStateToken reports what an operator must know before changing an image:
// whether a context still needs it, and whether its bytes were just re-proved.
func mediaStateToken(row media.MediaRow) string {
	switch {
	case row.Verified == "mismatch":
		return "corrupt"
	case row.Frozen:
		return "reserved"
	case row.Verified == "ok":
		return "verified"
	}
	return "stored"
}

func displayMediaList(result *media.ListResult) mediaListPresentation {
	rows := make([]mediaRowPresentation, 0, len(result.Media))
	for _, row := range result.Media {
		rows = append(rows, mediaRowPresentation{
			Name: escapeDisplayLine(row.Name), Size: row.Size, SHA256: escapeDisplayLine(row.SHA256),
			Source: escapeDisplayLine(row.Source), Added: escapeDisplayLine(row.Added),
			Frozen: row.Frozen, Verified: escapeDisplayLine(row.Verified),
		})
	}
	return mediaListPresentation{Media: rows}
}
