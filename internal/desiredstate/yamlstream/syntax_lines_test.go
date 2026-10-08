package yamlstream_test

import (
	"context"
	"testing"

	"github.com/crmarques/bootwright/internal/desiredstate/yamlstream"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

func TestParserStageErrorsNameTheirAuthoredLine(t *testing.T) {
	const (
		directive = "a %YAML 1.2 directive is not accepted"
		atLine    = "correct the YAML at that line; indent with spaces, not tabs"
		atOrBelow = "correct the YAML at or below that line; indent with spaces, not tabs"
		mapping   = "YAML syntax error: mapping values are not allowed in this context"
	)
	for _, row := range []struct {
		name, data, message, remediation string
		document, line                   int
	}{
		{"directive on the first line", "%YAML 1.2\n---\nvalue: true\n", directive, "remove the directive", 1, 1},
		{"directive on the second line", "# c\n%YAML 1.2\n---\nvalue: true\n", directive, "remove the directive", 1, 2},
		{"directive opening a second document", "a: 1\n%YAML 1.2\n---\nb: 2\n", directive, "remove the directive", 2, 2},
		{"sequence entry inside a mapping", "a: 1\nb: 2\n- c\n", "YAML syntax error: did not find expected key", atOrBelow, 1, 3},
		{"undefined tag handle", "a: !x!y 1\n", "YAML syntax error: found undefined tag handle", atOrBelow, 1, 1},
		{"mapping key inside a sequence", "x:\n  - a\n  b: c\n", "YAML syntax error: did not find expected '-' indicator", atOrBelow, 1, 2},
		{"scanner tab indent", "spec:\n\ttype: opaque\n", "YAML syntax error: found character that cannot start any token", atLine, 1, 2},
		{"scanner mis-indented key", "a: b\nkind: Secret\n  name: probe\n", mapping, atLine, 1, 3},
		{"scanner error on the first line", "a: b: c\n", mapping, atLine, 1, 0},
		{"composer alias without an anchor", "a: &x 1\nb: *y\n", "YAML syntax error: an alias names an anchor the document does not define", "write the value out in full instead of an anchor or alias", 1, 0},
	} {
		t.Run(row.name, func(t *testing.T) {
			documents, found, err := (yamlstream.Parser{}).Parse(context.Background(), sources("input.yaml", row.data))
			if err != nil || len(documents) != row.document-1 || len(found) != 1 {
				t.Fatalf("docs=%d diagnostics=%+v err=%v", len(documents), found, err)
			}
			want := diagnostics.SourceLocation{Path: "input.yaml", Document: row.document, Line: row.line}
			got := found[0]
			if got.Code != "yaml.syntax" || got.Message != row.message || got.Remediation != row.remediation || got.Source == nil || *got.Source != want {
				t.Fatalf("diagnostic = %+v at %+v, want %q / %q at %+v", got, got.Source, row.message, row.remediation, want)
			}
		})
	}
}
