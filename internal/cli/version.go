package cli

import (
	"io"
	"strings"
)

type BuildInfo struct {
	Version          string
	Commit           string
	Source           string
	GoVersion        string
	GOOS             string
	GOARCH           string
	DependencyBundle string
}

func writeVersion(out io.Writer, info BuildInfo) error {
	commit := strings.ToLower(strings.TrimSpace(info.Commit))
	if len(commit) < 7 || len(commit) > 64 || !isHex(commit) {
		commit = "unknown"
	}
	source := strings.ToLower(strings.TrimSpace(info.Source))
	if source != "clean" && source != "modified" {
		source = "unknown"
	}
	bundle := strings.TrimSpace(info.DependencyBundle)
	if len(bundle) != 71 || !strings.HasPrefix(bundle, "sha256:") ||
		!isHex(bundle[7:]) || bundle != strings.ToLower(bundle) {
		bundle = "none"
	}
	var text display
	text.headline("", "Bootwright")
	text.section("")
	text.fields(
		field{Label: "Version", Value: buildValue(info.Version, "devel")},
		field{Label: "Commit", Value: commit},
		field{Label: "Source", Value: source},
		field{Label: "Go", Value: buildValue(info.GoVersion, "unknown")},
		field{Label: "Target", Value: buildValue(info.GOOS, "unknown") + "/" + buildValue(info.GOARCH, "unknown")},
		field{Label: "Dependency bundle", Value: bundle},
	)
	return text.writeTo(out)
}

func buildValue(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}

func isHex(value string) bool {
	for _, c := range value {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F') {
			return false
		}
	}
	return value != ""
}
