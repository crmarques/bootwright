package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// stubCommands names, for each service wireStubs binds, the commands that
// service backs and the arguments that reach it.
var stubCommands = map[string]map[string][]string{
	"AddOnPreflight":       {"preflight add-ons": {"preflight", "add-ons"}},
	"EnvironmentPreflight": {"preflight infra": {"preflight", "infra"}, "preflight all": {"preflight", "all"}},
	"Installer":            {"render installer": {"render", "installer"}},
	"StorageArtifacts":     {"render storage": {"render", "storage"}},
	"Artifacts":            {"render": {"render", "--input-dir", "inputs", "--output-dir", "artifacts"}},
}

// specs/cli/commands.md lists the commands whose use case is unavailable. The
// list is the commands the services wireStubs binds back.
func TestUnavailableCommandsMatchTheStubWiring(t *testing.T) {
	services := reflect.ValueOf(wireStubs())
	var bound, derived []string
	for index := range services.NumField() {
		if services.Field(index).IsNil() {
			continue
		}
		name := services.Type().Field(index).Name
		bound = append(bound, name)
		commands, known := stubCommands[name]
		if !known {
			t.Errorf("wireStubs binds %s, which no row of stubCommands maps to commands; add the row and list its commands in specs/cli/commands.md", name)
		}
		for command := range commands {
			derived = append(derived, command)
		}
	}
	for name := range stubCommands {
		if !slices.Contains(bound, name) {
			t.Errorf("stubCommands maps %s, which wireStubs no longer binds", name)
		}
	}
	slices.Sort(derived)
	listed := documentedUnavailableCommands(t)
	if !slices.Equal(derived, listed) || len(derived) != 6 {
		t.Fatalf("wireStubs backs %q; specs/cli/commands.md lists %q", derived, listed)
	}
}

// Each command a stub backs answers cli.not-implemented, whatever else is
// wired.
func TestStubCommandsAnswerNotImplemented(t *testing.T) {
	for _, commands := range stubCommands {
		for command, args := range commands {
			var stdout, stderr bytes.Buffer
			code := runServices(context.Background(), args, &stdout, &stderr, wireStubs())
			if code != 1 || stdout.Len() != 0 || !strings.HasPrefix(stderr.String(), "  error  cli.not-implemented\n    bootwright "+command+" is not implemented\n") {
				t.Errorf("%s: exit=%d stdout=%q stderr=%q, want cli.not-implemented", command, code, stdout.String(), stderr.String())
			}
		}
	}
}

func documentedUnavailableCommands(t *testing.T) []string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "specs", "cli", "commands.md"))
	if err != nil {
		t.Fatal(err)
	}
	_, rest, found := strings.Cut(string(data), "\nNot yet met: `preflight infra`")
	if !found {
		t.Fatal("specs/cli/commands.md has no Not yet met line listing the unavailable commands")
	}
	paragraph, _, _ := strings.Cut("`preflight infra`"+rest, "\n\n")
	listed, _, _ := strings.Cut(paragraph, " run no use case")
	var commands []string
	for index, part := range strings.Split(listed, "`") {
		if index%2 == 1 {
			commands = append(commands, strings.Join(strings.Fields(part), " "))
		}
	}
	slices.Sort(commands)
	return commands
}
