package main

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/desiredstate"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

func TestAnUndecodableExampleDocumentIsItsOnlyRefusal(t *testing.T) {
	sources := exampleSources(t)
	compiler := wireCompiler()
	mutated := 0
	for fileIndex, file := range sources.Files {
		documents := strings.Split(string(file.Bytes()), "\n---\n")
		for index, document := range documents {
			if !strings.Contains(document, "\nspec:\n") {
				continue
			}
			mutated++
			changed := slices.Clone(documents)
			changed[index] = strings.Replace(document, "\nspec:\n", "\nspec:\n  zzUnknownField: x\n", 1)
			files := slices.Clone(sources.Files)
			files[fileIndex] = desiredstate.NewSourceFile(file.Path(), []byte(strings.Join(changed, "\n---\n")))
			input := desiredstate.Sources{Files: files, Markers: sources.Markers, Roots: sources.Roots}
			_, _, err := compiler.Compile(context.Background(), input)
			found := diagnostics.Of(err)
			if err == nil {
				t.Errorf("%s document %d: an unknown field was accepted", file.Path(), index+1)
				continue
			}
			own := false
			for _, d := range found {
				if d.Source == nil || d.Source.Path != file.Path() || d.Source.Document != index+1 {
					t.Errorf("%s document %d: a foreign diagnostic %s at %s: %s (source %+v)", file.Path(), index+1, d.Code, d.Field, d.Message, d.Source)
					continue
				}
				own = true
				if d.Severity == "error" && d.Remediation == "" {
					t.Errorf("%s document %d: %s at %s names no next step", file.Path(), index+1, d.Code, d.Field)
				}
			}
			if !own {
				t.Errorf("%s document %d: no diagnostic names the undecodable document: %v", file.Path(), index+1, found)
			}
		}
	}
	if mutated < 100 {
		t.Fatalf("only %d documents were mutated", mutated)
	}
}
