//go:build linux && amd64

package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/crmarques/bootwright/internal/controller/privilege"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/workspace/contexts"
)

type fixedAccount privilege.Account

func (a fixedAccount) Resolve(context.Context) (privilege.Account, error) {
	return privilege.Account(a), nil
}

// A root-squashed network home refuses root the search of the directory that
// holds the executable. The selection helper re-executes the running image
// through procfs, so reading the selection never resolves that path: it reads
// from a program whose directory this process can no longer search.
func TestASelectionReadNeverResolvesTheExecutablePath(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root searches any directory, so the refused search cannot be reproduced")
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	image, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	base := t.TempDir()
	home := filepath.Join(base, "home")
	if err := os.MkdirAll(filepath.Join(home, "bin"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(home, 0700) })
	executable := filepath.Join(home, "bin", "bootwright.test")
	if err := os.WriteFile(executable, image, 0755); err != nil {
		t.Fatal(err)
	}
	selectionHome := filepath.Join(base, "selection")
	if err := os.Mkdir(selectionHome, 0700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	command := exec.CommandContext(ctx, executable, "-test.run=^TestSelectionUnderAnUnsearchableExecutableHelper$")
	command.Env = append(os.Environ(), "BOOTWRIGHT_SQUASHED_HOME="+home, "BOOTWRIGHT_SELECTION_HOME="+selectionHome)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("the selection read refused: %v\n%s", err, output)
	}
}

func TestSelectionUnderAnUnsearchableExecutableHelper(t *testing.T) {
	home := os.Getenv("BOOTWRIGHT_SQUASHED_HOME")
	if home == "" {
		return
	}
	if err := os.Chmod(home, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := privilege.Executable(); err == nil {
		t.Fatal("the executable path still resolves, so this proves nothing")
	}
	account := invokingAccount{resolver: fixedAccount{UID: os.Getuid(), GID: os.Getgid(), Home: os.Getenv("BOOTWRIGHT_SELECTION_HOME")}}
	selection, err := account.Read(context.Background())
	if err != nil {
		t.Fatalf("reading the selection = %#v", diagnostics.Of(err))
	}
	if selection != (contexts.Selection{}) {
		t.Fatalf("an empty home selected %+v", selection)
	}
}
