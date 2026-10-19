package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStatusOpensWithTheContextHeadline(t *testing.T) {
	for _, name := range []string{"cli-status", "cli-status-another-build", "cli-status-unresolved", "cli-status-idle", "cli-status-destroyed", "cli-status-unprepared"} {
		data, err := os.ReadFile(filepath.Join("testdata", name+".golden"))
		if err != nil {
			t.Fatal(err)
		}
		lines := strings.Split(string(data), "\n")
		if lines[0] != "  ✓ Context lab" {
			t.Errorf("%s opens with %q, want %q", name, lines[0], "  ✓ Context lab")
		}
		for _, line := range lines {
			if strings.Contains(line, "· ok") {
				t.Errorf("%s keeps the hard-coded state: %q", name, line)
			}
		}
	}
}
