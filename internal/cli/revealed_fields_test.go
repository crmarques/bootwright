package cli

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// revealedHolders names the result types that hold cluster fields a flag
// reveals, by the path the output spec gives their value.
var revealedHolders = map[string]reflect.Type{
	"console":       reflect.TypeOf(consolePresentation{}),
	"dashboard":     reflect.TypeOf(dashboardPresentation{}),
	"monitoring[]":  reflect.TypeOf(monitoringPresentation{}),
	"objectUsers[]": reflect.TypeOf(objectUserPresentation{}),
}

// revealedByName are the fields that carry a revealed secret without a name
// ending in Value.
var revealedByName = []string{"monitoring[].user", "objectUsers[].accessKey", "objectUsers[].secretKey"}

func TestTheRevealedFieldsOfClusterInfoAreTheInventory(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "specs", "cli", "output.md"))
	if err != nil {
		t.Fatal(err)
	}
	_, section, found := strings.Cut(string(data), "\n### Revealed fields\n")
	if !found {
		t.Fatal("specs/cli/output.md has no revealed fields inventory")
	}
	section, _, _ = strings.Cut(section, "\n`TestTheRevealedFieldsOfClusterInfoAreTheInventory`")
	var documented []string
	for _, line := range strings.Split(section, "\n") {
		cells := strings.Split(strings.Trim(line, "| "), " | ")
		if strings.HasPrefix(line, "| `") && len(cells) == 2 {
			documented = append(documented, strings.Trim(cells[0], "`"))
		}
	}
	var derived []string
	for holder, typ := range revealedHolders {
		for index := 0; index < typ.NumField(); index++ {
			name, _, _ := strings.Cut(typ.Field(index).Tag.Get("json"), ",")
			path := holder + "." + name
			if strings.HasSuffix(name, "Value") || slices.Contains(revealedByName, path) {
				derived = append(derived, path)
			}
		}
	}
	slices.Sort(documented)
	slices.Sort(derived)
	if !slices.Equal(documented, derived) {
		t.Errorf("specs/cli/output.md lists revealed fields %v; the result types carry %v", documented, derived)
	}
}
