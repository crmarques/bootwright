package yamlstream_test

import (
	"context"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/desiredstate/yamlstream"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

func TestSyntaxDiagnosticsSeparateEncodingFromYAML(t *testing.T) {
	const yamlRemedy = "correct the YAML at that line; indent with spaces, not tabs"
	for _, row := range []struct {
		name, data, message, remediation string
		document, line, column           int
	}{
		{"invalid byte", "a: b\né€z\xff\n", "input is not valid UTF-8", "save the file as UTF-8", 0, 2, 4},
		{"invalid first byte", "\xfe", "input is not valid UTF-8", "save the file as UTF-8", 0, 1, 1},
		{"mis-indented key", "apiVersion: bootwright.io/v1alpha1\nkind: Secret\n  name: probe\n", "YAML syntax error: mapping values are not allowed in this context", yamlRemedy, 1, 3, 0},
		{"tab indent", "spec:\n\ttype: opaque\n", "YAML syntax error: found character that cannot start any token", yamlRemedy, 1, 2, 0},
		{"YAML 1.2 directive", "%YAML 1.2\n---\nvalue: true\n", "a %YAML 1.2 directive is not accepted", "remove the directive", 1, 1, 0},
		{"unknown anchor", "value: *zz-authored-sentinel\n", "YAML syntax error: an alias names an anchor the document does not define", "write the value out in full instead of an anchor or alias", 1, 0, 0},
	} {
		t.Run(row.name, func(t *testing.T) {
			documents, found, err := (yamlstream.Parser{}).Parse(context.Background(), sources("input.yaml", row.data))
			if err != nil || len(documents) != 0 || len(found) != 1 {
				t.Fatalf("docs=%d diagnostics=%+v err=%v", len(documents), found, err)
			}
			want := diagnostics.Diagnostic{Severity: "error", Code: "yaml.syntax", Message: row.message, Remediation: row.remediation,
				Source: &diagnostics.SourceLocation{Path: "input.yaml", Document: row.document, Line: row.line, Column: row.column}}
			if got := found[0]; got.Severity != want.Severity || got.Code != want.Code || got.Message != want.Message || got.Remediation != want.Remediation || got.Source == nil || *got.Source != *want.Source {
				t.Fatalf("diagnostic = %+v at %+v, want %+v at %+v", got, got.Source, want, want.Source)
			}
			if strings.Contains(found[0].Message, "zz-authored-sentinel") {
				t.Fatalf("the reason repeats authored text: %q", found[0].Message)
			}
		})
	}
}
