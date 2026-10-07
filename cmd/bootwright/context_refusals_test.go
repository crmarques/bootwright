//go:build linux && amd64

package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/cli"
	"github.com/crmarques/bootwright/internal/workspace/contexts"
)

const (
	absentGhost = "[FAIL] context.state: context ghost does not exist; next: create it with bootwright context init --name ghost, " +
		"or select an existing one with bootwright context use --name <context>; bootwright context list names them\n"
	absentEmpty = "[FAIL] context.state: context empty does not exist; next: create it with bootwright context init --name empty, " +
		"or select an existing one with bootwright context use --name <context>; bootwright context list names them\n"
	missingEmptyInput = "[FAIL] context.input: context empty has no desired state; next: import it with bootwright context update --name empty --input-dir <dir>\n"
	noSelection       = "[FAIL] context.state: no current context is selected; next: select one with bootwright context use --name <context>, " +
		"or create one with bootwright context init --name <context>\n"
)

// refusal runs one invocation that must refuse before any effect and returns
// what it printed on each stream.
func contextRefusal(t *testing.T, services cli.Services, args ...string) (int, string, string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := runServices(t.Context(), args, &out, &errOut, services)
	return code, out.String(), errOut.String()
}

func requireContextRefusal(t *testing.T, services cli.Services, want string, args ...string) {
	t.Helper()
	code, out, errOut := contextRefusal(t, services, args...)
	if code != 1 || out != "" || errOut != want {
		t.Fatalf("%v: code=%d stdout=%q\nstderr=%q\nwant code 1 and stderr %q", args, code, out, errOut, want)
	}
}

// Every refusal of a context the store does not hold names that context and
// both ways forward, whether the store is absent, holds other contexts, or the
// selection names a context that is gone.
func TestEveryCommandNamesAnAbsentContextAndItsNextCommand(t *testing.T) {
	explicit := func(input string) [][]string {
		return [][]string{
			{"context", "use", "--name", "ghost"},
			{"context", "update", "--name", "ghost", "--input-dir", input, "--yes"},
			{"context", "delete", "--name", "ghost", "--purge", "--yes"},
			{"validate", "--context", "ghost"},
			{"render", "effective", "--context", "ghost"},
			{"status", "--context", "ghost"},
			{"plan", "--context", "ghost"},
			{"secret", "list", "--context", "ghost"},
			{"machine", "list", "--context", "ghost"},
		}
	}
	implicit := [][]string{
		{"context", "current"},
		{"validate"},
		{"render", "effective"},
		{"status"},
		{"plan"},
		{"secret", "list"},
		{"machine", "list"},
	}
	for _, store := range []struct {
		name     string
		prepare  func(t *testing.T, services cli.Services, input, root string)
		implicit bool
	}{
		{name: "no store", prepare: func(*testing.T, cli.Services, string, string) {}},
		{name: "a store", prepare: func(t *testing.T, services cli.Services, input, _ string) {
			contextRun(t, services, 0, "context", "init", "--name", "alpha", "--input-dir", input)
		}},
		{name: "a stale selection", implicit: true, prepare: func(t *testing.T, services cli.Services, input, root string) {
			contextRun(t, services, 0, "context", "init", "--name", "alpha", "--input-dir", input)
			if err := testContextWiring(t, root).Selection.Write(context.Background(), contexts.Selection{Version: contexts.SelectionVersion, Name: "ghost"}); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(store.name, func(t *testing.T) {
			services, _, input, root := contextFixture(t)
			store.prepare(t, services, input, root)
			rows := explicit(input)
			if store.implicit {
				rows = append(rows, implicit...)
			}
			for _, args := range rows {
				t.Run(strings.ReplaceAll(strings.Join(args, " "), input, "<dir>"), func(t *testing.T) {
					requireContextRefusal(t, services, absentGhost, args...)
				})
			}
		})
	}
}

// A context that exists without desired state is refused under one code and
// one remedy by every command that needs that state.
func TestOneCodeNamesAMissingInput(t *testing.T) {
	services, _, _, _ := contextFixture(t)
	contextRun(t, services, 0, "context", "init", "--name", "empty")
	for _, args := range [][]string{
		{"validate", "--context", "empty"},
		{"render", "effective", "--context", "empty"},
		{"secret", "check", "--context", "empty"},
		{"status", "--context", "empty"},
		{"plan", "--context", "empty"},
		{"machine", "list", "--context", "empty"},
		{"validate"},
		{"status"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			requireContextRefusal(t, services, missingEmptyInput, args...)
		})
	}
	t.Run("preflight controller --context empty", func(t *testing.T) {
		controllerServices, _, _ := labContextServices(t)
		requireContextRefusal(t, controllerServices, absentEmpty, "preflight", "controller", "--context", "empty")
		contextRun(t, controllerServices, 0, "context", "init", "--name", "empty")
		requireContextRefusal(t, controllerServices, missingEmptyInput, "preflight", "controller", "--context", "empty")
	})
}

// With nothing selected, every command that would read the selection refuses
// in the same words, and three packages that cannot share the constructor
// keep byte-identical copies of it.
func TestNoSelectionReadsTheSameEverywhere(t *testing.T) {
	for _, store := range []struct {
		name    string
		prepare func(t *testing.T, services cli.Services, input string)
	}{
		{name: "no store", prepare: func(*testing.T, cli.Services, string) {}},
		{name: "a store without a selection", prepare: func(t *testing.T, services cli.Services, input string) {
			contextRun(t, services, 0, "context", "init", "--name", "alpha", "--input-dir", input)
			contextRun(t, services, 0, "context", "delete", "--name", "alpha", "--purge", "--yes")
		}},
	} {
		t.Run(store.name, func(t *testing.T) {
			services, _, input, _ := contextFixture(t)
			store.prepare(t, services, input)
			for _, args := range [][]string{
				{"context", "current"},
				{"validate"},
				{"render", "effective"},
				{"secret", "list"},
				{"status"},
				{"plan"},
				{"machine", "list"},
			} {
				t.Run(strings.Join(args, " "), func(t *testing.T) {
					requireContextRefusal(t, services, noSelection, args...)
				})
			}
		})
	}
}

// An unsafe selection marker, or an unsafe directory holding it, is refused
// naming the object and how to repair it, including a symlink the no-follow
// open itself refuses; an init that cannot select its new context names the
// same object.
func TestAnUnsafeSelectionFileIsNamedWithItsRepair(t *testing.T) {
	const (
		marker    = "selection file ~/.bootwright/context has an unsafe owner, type, link count or mode"
		markerFix = "remove it and select again with bootwright context use --name <context>"
		directory = "~/.bootwright has an unsafe owner, type or mode"
		dirFix    = "make ~/.bootwright a directory you own with mode 0700"
	)
	symlink := func(path string) error {
		if err := os.Rename(path, path+".elsewhere"); err != nil {
			return err
		}
		return os.Symlink(path+".elsewhere", path)
	}
	for _, unsafe := range []struct {
		name          string
		damage        func(home string) error
		message, next string
	}{
		{name: "marker", damage: func(home string) error { return os.Chmod(filepath.Join(home, ".bootwright", "context"), 0644) },
			message: marker, next: markerFix},
		{name: "marker-symlink", damage: func(home string) error { return symlink(filepath.Join(home, ".bootwright", "context")) },
			message: marker, next: markerFix},
		{name: "directory", damage: func(home string) error { return os.Chmod(filepath.Join(home, ".bootwright"), 0755) },
			message: directory, next: dirFix},
		{name: "directory-symlink", damage: func(home string) error { return symlink(filepath.Join(home, ".bootwright")) },
			message: directory, next: dirFix},
	} {
		t.Run(unsafe.name, func(t *testing.T) {
			services, _, input, root := contextFixture(t)
			contextRun(t, services, 0, "context", "init", "--name", "alpha", "--input-dir", input)
			if err := unsafe.damage(filepath.Dir(root)); err != nil {
				t.Fatal(err)
			}
			want := "[FAIL] context.state: " + unsafe.message + "; next: " + unsafe.next + "\n"
			for _, args := range [][]string{{"context", "current"}, {"validate"}, {"context", "use", "--name", "alpha"}} {
				requireContextRefusal(t, services, want, args...)
			}
			requireContextRefusal(t, services, "[FAIL] context.state: context beta was created, but the current selection could not be updated: "+
				unsafe.message+"; next: select it with bootwright context use --name beta\n", "context", "init", "--name", "beta")
			requireContextRefusal(t, services, want, "context", "use", "--name", "beta")
		})
	}
}

// A Context file whose secret-store type no implementation serves is refused
// under that file's path, as every other Context file refusal is.
func TestAnUnavailableSecretStoreTypeIsRefusedUnderItsContextFile(t *testing.T) {
	services, _, _, root := contextFixture(t)
	file := filepath.Join(filepath.Dir(root), "context.yaml")
	if err := os.WriteFile(file, []byte("apiVersion: bootwright.io/v1alpha1\nkind: Context\nmetadata:\n  name: demo\nspec:\n  secretStore:\n    type: vault\n"), 0600); err != nil {
		t.Fatal(err)
	}
	requireContextRefusal(t, services, "[FAIL] secret.store.implementation "+file+": requested secret store type is unavailable\n", "context", "init", "--name", "demo", "-f", file)
	requireContextRefusal(t, services, strings.ReplaceAll(absentGhost, "ghost", "demo"), "context", "use", "--name", "demo")
}
