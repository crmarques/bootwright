package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/cli"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

var stopCodeShape = regexp.MustCompile(`^[a-z]+(\.[a-z0-9-]+)+$`)

// The composed catalog is exactly the stop catalog's rows, and bootwright
// explain prints every one of them.
func TestEveryCatalogedStopIsComposedAndExplained(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "specs", "cli", "stops.md"))
	if err != nil {
		t.Fatal(err)
	}
	head, catalog, _ := strings.Cut(string(data), "\n## Catalog\n")
	_, attesting, _ := strings.Cut(head, "and no\nother, attest it:\n")
	attesting, _, _ = strings.Cut(attesting, "\n`TestEveryCatalogedStopIsComposedAndExplained`")
	confining := map[string]bool{}
	for _, line := range strings.Split(strings.TrimSpace(attesting), "\n") {
		if code, ok := strings.CutPrefix(line, "- `"); ok {
			confining[strings.TrimSuffix(code, "`")] = true
		}
	}
	if len(confining) == 0 {
		t.Fatal("specs/cli/stops.md lists no stop that confines a failed removal")
	}
	_, vocabulary, _ := strings.Cut(head, "## Stop vocabulary")
	listed := map[string]bool{}
	rows := map[string][]string{}
	for _, line := range strings.Split(catalog, "\n") {
		if code, ok := strings.CutPrefix(line, "| `"); ok {
			code, _, _ = strings.Cut(code, "`")
			listed[code] = true
			cells := strings.Split(strings.Trim(line, "|"), "|")
			for i := range cells {
				cells[i] = strings.Trim(strings.TrimSpace(cells[i]), "`")
			}
			rows[code] = cells
		}
	}
	composed := stopCatalog()
	seen := map[string]bool{}
	if len(composed) != len(listed) {
		t.Fatalf("the catalog lists %d stops and the executable composes %d", len(listed), len(composed))
	}
	for _, stop := range composed {
		if !listed[stop.Code] {
			t.Errorf("the executable composes %s, which specs/cli/stops.md does not list", stop.Code)
		}
		fault := string(stop.Fault)
		if fault == "" {
			fault = "-"
		}
		want := []string{stop.Code, string(stop.Level), string(stop.Certainty), string(stop.Cause), fault, string(stop.Recovery), stop.Meaning, stop.Remedy}
		if got := rows[stop.Code]; strings.Join(got, "\x00") != strings.Join(want, "\x00") {
			t.Errorf("stop %s\n declared %q\n catalog  %q", stop.Code, want, got)
		}
		if failed := diagnostics.Of(stop.Fail("x", "")); len(failed) != 1 || failed[0].Certainty != stop.Certainty || stop.Meaning == "" || stop.Remedy == "" ||
			!stopCodeShape.MatchString(stop.Code) || len(stop.Code) > 64 {
			t.Errorf("stop %s is malformed or fires as %+v", stop.Code, failed)
		}
		for _, value := range []string{string(stop.Level), string(stop.Certainty), string(stop.Cause), string(stop.Fault), string(stop.Recovery)} {
			if value != "" && !strings.Contains(vocabulary, "`"+value+"`") {
				t.Errorf("stop %s uses %q, which the stop vocabulary does not define", stop.Code, value)
			}
		}
		if stop.LocalRemovalFailure != confining[stop.Code] {
			t.Errorf("stop %s confines a failed removal: declared %t, catalog %t", stop.Code, stop.LocalRemovalFailure, confining[stop.Code])
		}
		if stop.LocalRemovalFailure && (stop.Certainty != diagnostics.CertaintyNone || stop.Cause != diagnostics.CauseTarget) {
			t.Errorf("stop %s confines a failed removal with certainty %s and cause %s", stop.Code, stop.Certainty, stop.Cause)
		}
		delete(confining, stop.Code)
		if seen[stop.Code] {
			t.Errorf("two packages declare the stop %s", stop.Code)
		}
		seen[stop.Code] = true
		var out, errOut bytes.Buffer
		if code := cli.New(cli.Config{Out: &out, ErrOut: &errOut, Stops: stopCatalog}).Run(context.Background(), []string{"explain", "--code", stop.Code}); code != 0 || !strings.Contains(strings.Join(strings.Fields(out.String()), " "), stop.Meaning) {
			t.Errorf("explain --code %s exited %d: %s%s", stop.Code, code, out.String(), errOut.String())
		}
	}
	for code := range confining {
		t.Errorf("specs/cli/stops.md says %s confines a failed removal, which no composed stop declares", code)
	}
}

// A stop's own remedy names a context-backed command with --context, as the
// output spec requires of every remediation that names one.
func TestEveryCatalogedRemedyNamingAContextBackedCommandCarriesTheContext(t *testing.T) {
	named := 0
	for _, stop := range stopCatalog() {
		for _, remedy := range regexp.MustCompile(`bootwright (?:status|plan|apply|destroy|diff|secret|machine|cluster)\b[^,;.]*`).FindAllString(stop.Remedy, -1) {
			named++
			if !strings.Contains(remedy, "--context <context>") {
				t.Errorf("stop %s: the remedy names %q without --context <context>", stop.Code, remedy)
			}
		}
	}
	if named == 0 {
		t.Fatal("no stop remedy names a context-backed command, so the check proves nothing")
	}
}
