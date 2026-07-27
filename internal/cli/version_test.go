package cli

import (
	"runtime/debug"
	"strings"
	"testing"
)

func TestVersionMetadataUsesStampedValues(t *testing.T) {
	info := &debug.BuildInfo{
		Settings: []debug.BuildSetting{
			{Key: "vcs.revision", Value: "f3afce3c77a24eaa47895ba04aa5f607e0719fad"},
		},
	}
	got := versionMetadataFrom("v1.2.3", "abc1234", info)
	if got.version != "v1.2.3" {
		t.Fatalf("version = %q, want v1.2.3", got.version)
	}
	if got.gitCommit != "abc1234" {
		t.Fatalf("gitCommit = %q, want abc1234", got.gitCommit)
	}
}

func TestVersionMetadataFallsBackToBuildInfoRevision(t *testing.T) {
	info := &debug.BuildInfo{
		Settings: []debug.BuildSetting{
			{Key: "vcs.revision", Value: "f3afce3c77a24eaa47895ba04aa5f607e0719fad"},
		},
	}
	got := versionMetadataFrom("dev", "unknown", info)
	if got.version != "dev" {
		t.Fatalf("version = %q, want dev", got.version)
	}
	if got.gitCommit != "f3afce3" {
		t.Fatalf("gitCommit = %q, want f3afce3", got.gitCommit)
	}
}

func TestVersionMetadataUsesUnknownWhenNoRevisionExists(t *testing.T) {
	got := versionMetadataFrom("", "", &debug.BuildInfo{})
	if got.version != "dev" {
		t.Fatalf("version = %q, want dev", got.version)
	}
	if got.gitCommit != "unknown" {
		t.Fatalf("gitCommit = %q, want unknown", got.gitCommit)
	}
	if got.vcsModified != "unknown" {
		t.Fatalf("vcsModified = %q, want unknown", got.vcsModified)
	}
}

func TestVersionMetadataCarriesDirtyWorkingTree(t *testing.T) {
	info := &debug.BuildInfo{
		Settings: []debug.BuildSetting{
			{Key: "vcs.revision", Value: "f3afce3c77a24eaa47895ba04aa5f607e0719fad"},
			{Key: "vcs.modified", Value: "true"},
		},
	}
	got := versionMetadataFrom("dev", "unknown", info)
	if got.vcsModified != "true" {
		t.Fatalf("vcsModified = %q, want true", got.vcsModified)
	}
}

func TestBundleVersionMarkerSeparatesRebuiltBundlesAtTheSameCommit(t *testing.T) {
	metadata := versionMetadata{version: "dev", gitCommit: "abc1234", vcsModified: "true"}
	first := bundleVersionMarkerFrom(metadata, 18_000_000, "1111111111111111111111111111111111111111111111111111111111111111")
	second := bundleVersionMarkerFrom(metadata, 18_000_000, "2222222222222222222222222222222222222222222222222222222222222222")
	if first == second {
		t.Fatalf("bundle marker ignores archive content: %q", first)
	}
	third := bundleVersionMarkerFrom(metadata, 18_000_001, "1111111111111111111111111111111111111111111111111111111111111111")
	if first == third {
		t.Fatalf("bundle marker ignores archive length: %q", first)
	}
	clean := versionMetadata{version: "dev", gitCommit: "abc1234", vcsModified: "false"}
	if first == bundleVersionMarkerFrom(clean, 18_000_000, "1111111111111111111111111111111111111111111111111111111111111111") {
		t.Fatalf("bundle marker ignores a dirty working tree: %q", first)
	}
}

func TestBundleVersionMarkerKeepsVersionOnTheFirstLine(t *testing.T) {
	marker := bundleVersionMarkerFrom(versionMetadata{version: "v1.2.3", gitCommit: "abc1234", vcsModified: "false"}, 42, "deadbeef")
	first, _, ok := strings.Cut(marker, "\n")
	if !ok || first != "version=v1.2.3" {
		t.Fatalf("first marker line = %q, want version=v1.2.3", first)
	}
	if strings.ContainsAny(first, "/\\ ") {
		t.Fatalf("bundle cache key %q is not a safe directory name", first)
	}
}
