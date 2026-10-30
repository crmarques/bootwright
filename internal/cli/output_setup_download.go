package cli

import (
	"io"
	"net/url"
	"strconv"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
)

func validSetupDownload(report *prerequisites.Report) bool {
	if report == nil || report.Export == nil || report.Outcome != "downloaded" || report.DryRun || report.Export.Directory == "" || len(report.Export.Files) == 0 {
		return false
	}
	for _, file := range report.Export.Files {
		if file.Filename == "" || file.SHA256 == "" {
			return false
		}
	}
	return true
}

func writeSetupDownload(out io.Writer, report *prerequisites.Report) error {
	var text display
	text.headline("", "Controller wheel download")
	scope := []field{{Label: "Directory", Value: report.Export.Directory}}
	if report.Route != "" {
		scope = append(scope, field{Label: "Route", Value: report.Route})
	}
	text.section("")
	text.fields(scope...)
	rows := make([][]string, 0, len(report.Export.Files)+1)
	counts := map[string]int{}
	for _, file := range report.Export.Files {
		counts[file.Origin]++
		rows = append(rows, []string{progressToken(prerequisites.ActionProgress{Phase: "observed", Outcome: downloadOutcome(file.Origin)}), file.Filename, downloadOrigin(file.Origin)})
	}
	rows = append(rows, []string{"[DONE]", exportedLockLabel, "copy of the lock this build embeds"})
	text.section("Files")
	text.rows(rows)
	text.section("")
	text.fields(
		field{Label: "Outcome", Value: "downloaded"},
		field{Label: "Written", Value: strconv.Itoa(len(report.Export.Files)+1) + " (" + strconv.Itoa(counts[prerequisites.OriginDownload]+counts[prerequisites.OriginMirror]) + " fetched, " +
			strconv.Itoa(counts[prerequisites.OriginCache]) + " from the wheel cache, " + strconv.Itoa(counts[prerequisites.OriginPresent]) + " already there)"},
		field{Label: "Next", Value: "bootwright controller install --wheel-mirror " + (&url.URL{Scheme: "file", Path: report.Export.Directory}).String()},
	)
	return text.writeTo(out)
}

const exportedLockLabel = "lock.json"

func downloadOutcome(origin string) string {
	if origin == prerequisites.OriginPresent {
		return "unchanged"
	}
	return "changed"
}

func downloadOrigin(origin string) string {
	switch origin {
	case prerequisites.OriginPresent:
		return "already there"
	case prerequisites.OriginCache:
		return "from the wheel cache"
	case prerequisites.OriginMirror:
		return "from the mirror"
	}
	return "downloaded"
}
