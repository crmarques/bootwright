package cli

import (
	"reflect"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
)

func TestDiagnosticSortOrder(t *testing.T) {
	base := func() diagnostic {
		return diagnostic{Severity: "error", Code: "same", Message: "same", Field: "same", Source: &diagnostics.SourceLocation{Path: "same", Document: 1, Line: 1, Column: 1}, Object: &diagnostics.ObjectIdentity{APIVersion: "same", Kind: "same", Name: "same"}}
	}
	for _, test := range []struct {
		name           string
		earlier, later func(*diagnostic)
	}{
		{"source presence", func(d *diagnostic) {}, func(d *diagnostic) { d.Source = nil }},
		{"source path", func(d *diagnostic) { d.Source.Path = "a" }, func(d *diagnostic) { d.Source.Path = "b" }},
		{"document", func(d *diagnostic) { d.Source.Document = 1 }, func(d *diagnostic) { d.Source.Document = 2 }},
		{"line", func(d *diagnostic) { d.Source.Line = 1 }, func(d *diagnostic) { d.Source.Line = 2 }},
		{"column", func(d *diagnostic) { d.Source.Column = 1 }, func(d *diagnostic) { d.Source.Column = 2 }},
		{"code", func(d *diagnostic) { d.Code = "a" }, func(d *diagnostic) { d.Code = "b" }},
		{"object presence", func(d *diagnostic) { d.Object = nil }, func(d *diagnostic) {}},
		{"api version", func(d *diagnostic) { d.Object.APIVersion = "a" }, func(d *diagnostic) { d.Object.APIVersion = "b" }},
		{"kind", func(d *diagnostic) { d.Object.Kind = "a" }, func(d *diagnostic) { d.Object.Kind = "b" }},
		{"name", func(d *diagnostic) { d.Object.Name = "a" }, func(d *diagnostic) { d.Object.Name = "b" }},
		{"field", func(d *diagnostic) { d.Field = "a" }, func(d *diagnostic) { d.Field = "b" }},
		{"message", func(d *diagnostic) { d.Message = "a" }, func(d *diagnostic) { d.Message = "b" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			earlier, later := base(), base()
			earlier.Message, later.Message = "z", "a"
			test.earlier(&earlier)
			test.later(&later)
			input := []diagnostic{later, earlier}
			got := displayDiagnostics(input)
			want := []diagnostic{earlier, later}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("order = %#v; want %#v", got, want)
			}
			if input[0].Message != later.Message {
				t.Fatal("sorting mutated input")
			}
		})
	}
	t.Run("severity and remediation preserve stable ties", func(t *testing.T) {
		a, b := base(), base()
		a.Severity, a.Remediation = "warning", "z"
		b.Remediation = "a"
		input := []diagnostic{a, b}
		if got := displayDiagnostics(input); !reflect.DeepEqual(got, input) {
			t.Fatalf("stable tie changed: %#v", got)
		}
	})
}
