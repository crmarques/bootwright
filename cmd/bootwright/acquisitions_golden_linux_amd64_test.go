package main

import (
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/desiredstate"
)

// The acquisition list names every origin an example's graph reaches, so each
// example's rows are pinned beside its effective state: where the list is
// derived must not change what it names.
func TestEveryExampleAcquisitionListMatchesItsGolden(t *testing.T) {
	t.Parallel()
	directories := exampleDirectories(t)
	var examples []string
	for name := range directories {
		examples = append(examples, name)
	}
	slices.Sort(examples)
	for _, name := range examples {
		t.Run(name, func(t *testing.T) {
			var sources desiredstate.Sources
			if name == refusedExample {
				sources = admissibleExampleSources(t, true)
			} else {
				sources = exampleDirectory(t, name)
			}
			state, _ := compileAcceptance(t, sources)
			acquired, err := listAcquisitions(state.Effective())
			var out strings.Builder
			if err != nil {
				out.WriteString("refused: " + strings.ReplaceAll(err.Error(), "\n", " ") + "\n")
			}
			for _, row := range acquired {
				out.WriteString(strings.Join([]string{row.Kind, row.Artifact, row.Version, row.Origin, row.Class, row.SHA256, strconv.FormatInt(row.Bytes, 10), strings.Join(row.Machines, ","), row.Consumer}, "|"))
				out.WriteString("\n")
			}
			if out.Len() == 0 {
				out.WriteString("none\n")
			}
			matchesTextGolden(t, "acquisitions/"+name, []byte(out.String()))
		})
	}
	goldens, err := filepath.Glob(filepath.Join("testdata", "acquisitions", "*.golden"))
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range goldens {
		if name := strings.TrimSuffix(filepath.Base(path), ".golden"); !slices.Contains(examples, name) {
			t.Errorf("%s pins missing example %s; delete the golden with its example", path, name)
		}
	}
}
