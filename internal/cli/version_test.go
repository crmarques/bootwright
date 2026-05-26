package cli

import (
	"runtime/debug"
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
}
