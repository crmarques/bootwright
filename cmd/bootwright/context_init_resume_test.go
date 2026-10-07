//go:build linux && amd64

package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/workspace/contextfs"
	"github.com/crmarques/bootwright/internal/workspace/contexts"
)

// interruptInit leaves the initializing record an init that stopped after its
// reservation leaves, admitted from environment.
func interruptInit(t *testing.T, repository *contextfs.Store, environment string) {
	t.Helper()
	var roots []string
	if environment != "" {
		roots = []string{environment}
	}
	interrupted := contexts.StateError("interrupted after the reservation")
	err := repository.Transact(context.Background(), true, roots, func(tx contexts.Transaction) error {
		if _, err := tx.Reserve(context.Background(), "alpha", environment, contexts.DefaultConfiguration("alpha").Canonical()); err != nil {
			return err
		}
		return interrupted
	})
	if !errors.Is(err, interrupted) {
		t.Fatalf("the planted reservation failed: %v", err)
	}
	registry, err := repository.View(context.Background())
	if err != nil || len(registry.Contexts) != 1 || registry.Contexts[0].Mode != contexts.Initializing || registry.Contexts[0].EnvironmentDirectory != environment {
		t.Fatalf("no initializing record persisted: %+v %v", registry, err)
	}
}

func secondInput(t *testing.T) string {
	t.Helper()
	input := filepath.Join(t.TempDir(), "other")
	if err := os.Mkdir(input, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(input, "environment.yaml"), []byte(syntheticEnvironment), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(input, "controller.yaml"), []byte(serviceHost), 0600); err != nil {
		t.Fatal(err)
	}
	return input
}

// An init that stopped after reserving its name resumes from whatever input
// the retry admits, and until then every command over the name names both
// ways out: finishing the init and discarding it.
func TestAnInterruptedInitResumesFromAnotherDirectoryAndFromNoInput(t *testing.T) {
	for _, interrupted := range []struct {
		name      string
		reservedA bool
	}{
		{name: "reserved from another directory", reservedA: true},
		{name: "reserved without input"},
	} {
		t.Run(interrupted.name, func(t *testing.T) {
			services, repository, first, _ := contextFixture(t)
			environment := ""
			if interrupted.reservedA {
				environment = first
			}
			interruptInit(t, repository, environment)
			second := secondInput(t)
			contextRun(t, services, 0, "context", "init", "--name", "alpha", "--input-dir", second)
			registry, err := repository.View(context.Background())
			if err != nil || len(registry.Contexts) != 1 {
				t.Fatalf("registry %+v %v", registry, err)
			}
			if record := registry.Contexts[0]; record.Mode != contexts.Ready || record.Revision == "" || record.EnvironmentDirectory != second {
				t.Fatalf("the resumed init recorded %+v, want a ready record admitted from %s", record, second)
			}
			contextRun(t, services, 0, "validate", "--context", "alpha")
		})
	}
	t.Run("refusals over the reservation name both exits", func(t *testing.T) {
		services, repository, first, _ := contextFixture(t)
		interruptInit(t, repository, first)
		const want = "[FAIL] context.state: context alpha is still initializing: an earlier context init did not finish; " +
			"next: finish it with bootwright context init --name alpha and its original Context file, from any input directory, " +
			"or discard it with bootwright context delete --name alpha --purge\n"
		for _, args := range [][]string{{"context", "use", "--name", "alpha"}, {"validate", "--context", "alpha"}} {
			requireContextRefusal(t, services, want, args...)
		}
		_, errOut := contextRun(t, services, 0, "context", "delete", "--name", "alpha", "--purge", "--yes")
		if strings.Contains(errOut, "[FAIL]") {
			t.Fatal(errOut)
		}
		registry, err := repository.View(context.Background())
		if err != nil || len(registry.Contexts) != 0 {
			t.Fatalf("the discarded initialization left %+v %v", registry, err)
		}
	})
}
