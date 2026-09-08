package cli

import (
	"fmt"
	"io"
	"strings"
)

type BuildInfo struct {
	Version          string
	Commit           string
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
	bundle := strings.TrimSpace(info.DependencyBundle)
	if len(bundle) != 71 || !strings.HasPrefix(bundle, "sha256:") ||
		!isHex(bundle[7:]) || bundle != strings.ToLower(bundle) {
		bundle = "none"
	}
	_, err := fmt.Fprintf(out, "version: %s\ncommit: %s\ngo: %s\ntarget: %s/%s\ndependency bundle: %s\n",
		buildValue(info.Version, "devel"), commit,
		buildValue(info.GoVersion, "unknown"), buildValue(info.GOOS, "unknown"),
		buildValue(info.GOARCH, "unknown"), bundle)
	return err
}

func buildValue(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return escapeDisplayLine(value)
}

func isHex(value string) bool {
	for _, c := range value {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F') {
			return false
		}
	}
	return value != ""
}
