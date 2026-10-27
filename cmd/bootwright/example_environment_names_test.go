package main

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"go.yaml.in/yaml/v3"
)

// specs/api/environment.md says an Environment's name labels the effective
// graph and is no identity, so two examples sharing one would print the same
// label for different graphs. Each categorized example names its Environment
// differently.
func TestExampleEnvironmentNamesAreDistinct(t *testing.T) {
	owners := map[string][]string{}
	for example, directory := range exampleDirectories(t) {
		data, err := os.ReadFile(filepath.Join(directory, "environment.yaml"))
		if err != nil {
			t.Fatal(err)
		}
		decoder := yaml.NewDecoder(bytes.NewReader(data))
		for {
			var document struct {
				Kind     string `yaml:"kind"`
				Metadata struct {
					Name string `yaml:"name"`
				} `yaml:"metadata"`
			}
			if err := decoder.Decode(&document); errors.Is(err, io.EOF) {
				break
			} else if err != nil {
				t.Fatalf("%s: %v", example, err)
			}
			if document.Kind == "Environment" {
				owners[document.Metadata.Name] = append(owners[document.Metadata.Name], example)
			}
		}
	}
	if len(owners) < 25 {
		t.Fatalf("read %d Environment names; the examples have changed shape", len(owners))
	}
	for name, examples := range owners {
		slices.Sort(examples)
		if len(examples) > 1 {
			t.Errorf("the Environment name %q is shared by the examples %v; give each its own", name, examples)
		}
	}
}
