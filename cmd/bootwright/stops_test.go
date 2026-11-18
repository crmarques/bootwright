package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/cli"
)

// The composed catalog is exactly the stop catalog's rows, and bootwright
// explain prints every one of them.
func TestEveryCatalogedStopIsComposedAndExplained(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "specs", "cli", "stops.md"))
	if err != nil {
		t.Fatal(err)
	}
	_, catalog, _ := strings.Cut(string(data), "\n## Catalog\n")
	listed := map[string]bool{}
	for _, line := range strings.Split(catalog, "\n") {
		if code, ok := strings.CutPrefix(line, "| `"); ok {
			code, _, _ = strings.Cut(code, "`")
			listed[code] = true
		}
	}
	composed := stopCatalog()
	if len(composed) != len(listed) {
		t.Fatalf("the catalog lists %d stops and the executable composes %d", len(listed), len(composed))
	}
	for _, stop := range composed {
		if !listed[stop.Code] {
			t.Errorf("the executable composes %s, which specs/cli/stops.md does not list", stop.Code)
		}
		var out, errOut bytes.Buffer
		if code := cli.New(cli.Config{Out: &out, ErrOut: &errOut, Stops: stopCatalog}).Run(context.Background(), []string{"explain", "--code", stop.Code}); code != 0 || !strings.Contains(strings.Join(strings.Fields(out.String()), " "), stop.Meaning) {
			t.Errorf("explain --code %s exited %d: %s%s", stop.Code, code, out.String(), errOut.String())
		}
	}
}
