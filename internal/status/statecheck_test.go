package status

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/converge/workflow"
	"github.com/crmarques/bootwright/internal/ownership"
)

func TestStateCheckSurfacesSkippedOwnershipRecords(t *testing.T) {
	const contextName = "ctx"
	ownershipDir := t.TempDir()
	runsDir := t.TempDir()

	if err := ownership.SaveResource(ownershipDir, ownership.ResourceRecord{
		Kind:    "Machine",
		Name:    "good",
		Owner:   ownership.Owner,
		Context: contextName,
	}); err != nil {
		t.Fatalf("save valid ownership record: %v", err)
	}

	badDir := filepath.Join(ownershipDir, ownership.ResourceDirName, "Machine")
	if err := os.MkdirAll(badDir, 0o700); err != nil {
		t.Fatalf("mkdir ownership kind dir: %v", err)
	}
	badPath := filepath.Join(badDir, "corrupt.json")
	if err := os.WriteFile(badPath, []byte("{ this is not valid json"), 0o600); err != nil {
		t.Fatalf("write malformed ownership record: %v", err)
	}

	report, err := StateCheck(v1alpha1.State{}, "", "all", workflow.ApplyTarget{}, t.TempDir(), runsDir, ownershipDir, contextName)
	if err != nil {
		t.Fatalf("StateCheck: %v", err)
	}
	if len(report.LoadWarnings) != 1 {
		t.Fatalf("expected exactly one load warning for the corrupt record, got %v", report.LoadWarnings)
	}
	if !strings.Contains(report.LoadWarnings[0], "corrupt.json") {
		t.Fatalf("load warning must name the bad file, got %q", report.LoadWarnings[0])
	}
}

func TestStateCheckNoLoadWarningsWhenRecordsClean(t *testing.T) {
	const contextName = "ctx"
	ownershipDir := t.TempDir()
	runsDir := t.TempDir()

	if err := ownership.SaveResource(ownershipDir, ownership.ResourceRecord{
		Kind:    "Machine",
		Name:    "good",
		Owner:   ownership.Owner,
		Context: contextName,
	}); err != nil {
		t.Fatalf("save valid ownership record: %v", err)
	}

	report, err := StateCheck(v1alpha1.State{}, "", "all", workflow.ApplyTarget{}, t.TempDir(), runsDir, ownershipDir, contextName)
	if err != nil {
		t.Fatalf("StateCheck: %v", err)
	}
	if len(report.LoadWarnings) != 0 {
		t.Fatalf("a clean ownership store must yield no load warnings, got %v", report.LoadWarnings)
	}
}
