//go:build linux && amd64

package venvlocal

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
)

// A runtime names the wheels its build installed, read back from the
// requirements.lock that build left, installer included, so retention keeps
// them.
func TestARuntimeNamesTheWheelsItsBuildInstalled(t *testing.T) {
	_, adapter, runtime := built(t)
	files, _ := adapter.lock.Files(runtime.Interpreter.ABI)
	want := []string{}
	for _, file := range files {
		want = append(want, file.SHA256)
	}
	want = append(want, adapter.lock.Installer().SHA256)
	named, err := adapter.RuntimeWheels(context.Background(), runtime.Digest)
	if err != nil || !slices.Equal(named, want) {
		t.Fatalf("RuntimeWheels = %q, %v, want %q", named, err, want)
	}
	if _, err := adapter.RuntimeWheels(context.Background(), strings.Repeat("e", 64)); !errors.Is(err, prerequisites.ErrRuntimeUnavailable) {
		t.Fatalf("an absent runtime named wheels: %v", err)
	}
	if err := os.WriteFile(filepath.Join(adapter.runtimes, runtime.Digest, requirementsName), []byte("ansible-core==2.21.6\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := adapter.RuntimeWheels(context.Background(), runtime.Digest); err == nil || errors.Is(err, prerequisites.ErrRuntimeUnavailable) {
		t.Fatalf("a requirements.lock without hashes was admitted: %v", err)
	}
}
