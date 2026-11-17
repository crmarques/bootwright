package tooltable_test

import (
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/controller/tooltable"
)

func TestTheEmbeddedToolTablePinsEachClientAtOrAboveItsMinimum(t *testing.T) {
	pins, err := tooltable.Pins()
	if err != nil {
		t.Fatal(err)
	}
	kinds := []string{}
	for _, pin := range pins {
		kinds = append(kinds, pin.Kind)
		if order, comparable := tooltable.CompareReleases(pin.Default.Version, pin.Minimum); !comparable || order < 0 {
			t.Errorf("%s defaults to %s below its minimum %s", pin.Kind, pin.Default.Version, pin.Minimum)
		}
	}
	if strings.Join(kinds, ",") != "govc,helm,kubectl,virtctl" {
		t.Fatalf("the table pins %v", kinds)
	}
}

func TestATableThatBreaksItsShapeIsRefused(t *testing.T) {
	entry := func(kind, minimum, version, sha string) string {
		return `{"kind":"` + kind + `","minimum":"` + minimum + `","license":"Apache-2.0","default":{"version":"` + version + `","bytes":10,"sha256":"` + sha + `"}}`
	}
	digest := strings.Repeat("a", 64)
	table := func(format string, entries ...string) string {
		return `{"format":"` + format + `","tools":[` + strings.Join(entries, ",") + `]}`
	}
	good := []string{entry("govc", "0.1.0", "0.1.0", digest), entry("helm", "1.0.0", "1.2.0", digest), entry("kubectl", "1.0.0", "1.0.0", digest), entry("virtctl", "1.0.0", "1.0.0", digest)}
	if _, err := tooltable.Parse([]byte(table(tooltable.Format, good...))); err != nil {
		t.Fatalf("a well-formed table was refused: %v", err)
	}
	for name, data := range map[string]string{
		"another format":         table("bootwright.controller.tool-table-v0", good...),
		"a missing client":       table(tooltable.Format, good[:3]...),
		"clients out of order":   table(tooltable.Format, good[1], good[0], good[2], good[3]),
		"a default below":        table(tooltable.Format, entry("govc", "0.2.0", "0.1.0", digest), good[1], good[2], good[3]),
		"a v prefix":             table(tooltable.Format, entry("govc", "v0.1.0", "v0.1.0", digest), good[1], good[2], good[3]),
		"a prerelease default":   table(tooltable.Format, entry("govc", "0.1.0", "0.1.1-rc.1", digest), good[1], good[2], good[3]),
		"an uppercase digest":    table(tooltable.Format, entry("govc", "0.1.0", "0.1.0", strings.Repeat("A", 64)), good[1], good[2], good[3]),
		"a short digest":         table(tooltable.Format, entry("govc", "0.1.0", "0.1.0", "abc"), good[1], good[2], good[3]),
		"no license":             strings.Replace(table(tooltable.Format, good...), `"license":"Apache-2.0"`, `"license":""`, 1),
		"a license in prose":     strings.Replace(table(tooltable.Format, good...), `"license":"Apache-2.0"`, `"license":"Apache License 2"`, 1),
		"an unknown field":       strings.Replace(table(tooltable.Format, good...), `"tools"`, `"extra":1,"tools"`, 1),
		"two documents":          table(tooltable.Format, good...) + "{}",
		"a size outside a bound": strings.Replace(table(tooltable.Format, good...), `"bytes":10`, `"bytes":0`, 1),
	} {
		if _, err := tooltable.Parse([]byte(data)); err == nil {
			t.Errorf("%s was admitted", name)
		}
	}
}

func TestReleasesCompareByTheirTripleWithAPrereleaseBelowItsRelease(t *testing.T) {
	for _, test := range []struct {
		a, b  string
		order int
	}{
		{"4.3.1", "4.3.1", 0}, {"v4.3.1", "4.3.1", 0}, {"4.3.2", "4.3.1", 1}, {"4.10.0", "4.9.9", 1},
		{"1.9.0-rc.1", "1.9.0", -1}, {"1.9.0rc1", "1.9.0", -1}, {"1.9.0+build", "1.9.0", 0}, {"0.56.1", "0.56.2", -1},
	} {
		if order, comparable := tooltable.CompareReleases(test.a, test.b); !comparable || order != test.order {
			t.Errorf("%s against %s orders %d (%t), want %d", test.a, test.b, order, comparable, test.order)
		}
	}
	for _, tag := range []string{"0.54", "1", "latest", "1.02.3"} {
		if _, comparable := tooltable.CompareReleases(tag, "1.0.0"); comparable {
			t.Errorf("%s compared as a release", tag)
		}
	}
}
