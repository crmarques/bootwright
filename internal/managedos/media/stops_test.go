package media

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
)

func TestTheMediaStopsAreTheCatalogsRowsAndTheCatalogHoldsACopy(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "specs", "cli", "stops.md"))
	if err != nil {
		t.Fatal(err)
	}
	rows := map[string]string{}
	for _, line := range strings.Split(string(data), "\n") {
		if cells := strings.Split(strings.Trim(line, "|"), "|"); strings.HasPrefix(line, "| `media.") && len(cells) == 8 {
			for i := range cells {
				cells[i] = strings.Trim(strings.TrimSpace(cells[i]), "`")
			}
			rows[cells[0]] = strings.Join(cells, "\x00")
		}
	}
	declared := Stops()
	if len(rows) != len(declared) {
		t.Fatalf("the catalog lists %d media stops, the package declares %d", len(rows), len(declared))
	}
	for _, stop := range declared {
		fault := string(stop.Fault)
		if fault == "" {
			fault = "-"
		}
		want := strings.Join([]string{stop.Code, string(stop.Level), string(stop.Certainty), string(stop.Cause), fault, string(stop.Recovery), stop.Meaning, stop.Remedy}, "\x00")
		if rows[stop.Code] != want {
			t.Errorf("stop %s\n declared %q\n catalog  %q", stop.Code, want, rows[stop.Code])
		}
		if failed := diagnostics.Of(stop.Fail("x", "")); len(failed) != 1 || failed[0].Code != stop.Code {
			t.Errorf("stop %s fires as %+v", stop.Code, failed)
		}
	}
	declared[0] = diagnostics.Stop{}
	if Stops()[0].Code != "media.store-busy" {
		t.Fatal("a caller rewrote the package's stops")
	}
}

func TestTheMediaRuleChecksNamesAndNormalizesDigests(t *testing.T) {
	for name, want := range map[string]bool{"rhel-9.6-x86_64-dvd.iso": true, "a.iso": true, "": false, "image.img": false, "-lead.iso": false, "trail-.iso": false, "dir/image.iso": false, "Image.ISO": false, ".iso": false} {
		if got := ValidName(name); got != want {
			t.Errorf("ValidName(%q) = %v, want %v", name, got, want)
		}
	}
	pin := strings.Repeat("ab", 32)
	for value, want := range map[string]string{"": "", " sha256:" + strings.ToUpper(pin) + " ": pin, pin: pin} {
		if got, ok := NormalizeDigest(value); !ok || got != want {
			t.Errorf("NormalizeDigest(%q) = %q, %v, want %q", value, got, ok, want)
		}
	}
	for _, value := range []string{"sha256:abc", strings.Repeat("g", 64), "md5:" + pin} {
		if got, ok := NormalizeDigest(value); ok || got != "" {
			t.Errorf("NormalizeDigest(%q) = %q, %v", value, got, ok)
		}
	}
}
