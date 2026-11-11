package substrate

import (
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func TestRealizedTargetFieldTableMatchesTarget(t *testing.T) {
	data, err := os.ReadFile("../../specs/substrates.md")
	if err != nil {
		t.Fatal(err)
	}
	_, section, ok := strings.Cut(string(data), "### Realized target fields\n")
	if !ok {
		t.Fatal("realized-target field table is absent")
	}
	section, _, _ = strings.Cut(section, "\n### ")
	fields := []string{}
	for _, line := range strings.Split(section, "\n") {
		if !strings.HasPrefix(line, "| "+string(rune(96))) {
			continue
		}
		cells := strings.Split(line, "|")
		fields = append(fields, strings.Trim(strings.TrimSpace(cells[1]), string(rune(96))))
	}
	want := []string{}
	typ := reflect.TypeFor[Target]()
	for i := range typ.NumField() {
		if typ.Field(i).IsExported() {
			want = append(want, typ.Field(i).Name)
		}
	}
	if !slices.Equal(fields, want) {
		t.Fatalf("realized-target fields = %v, want Target fields %v", fields, want)
	}
}
