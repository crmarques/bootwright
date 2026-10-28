package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/desiredstate/encoding"
	"github.com/crmarques/bootwright/internal/desiredstate/inputfs"
)

func renderRun(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := runServices(context.Background(), args, &out, &errOut, isolatedServices(t))
	return code, out.String(), errOut.String()
}

func TestComposedRenderWritesTheEffectiveStateToThePathInsteadOfStandardOutput(t *testing.T) {
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		t.Skip("input admission is qualified for linux/amd64")
	}
	root := t.TempDir()
	input := filepath.Join(root, "environment.yaml")
	if err := os.WriteFile(input, []byte(serviceEnvironment+"\n---\n"+serviceHost), 0o600); err != nil {
		t.Fatal(err)
	}
	sources, err := (inputfs.Reader{}).Read(context.Background(), []string{input})
	if err != nil {
		t.Fatal(err)
	}
	state, _ := compileAcceptance(t, sources)
	want, err := encoding.YAML(context.Background(), state.Effective())
	if err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(root, "artifacts")
	code, out, errOut := renderRun(t, "render", "--input-dir", input, "--output-dir", output)
	written := filepath.Join(output, "effective-state.yaml")
	data, readErr := os.ReadFile(written)
	if code != 0 || errOut != "" || readErr != nil || !bytes.Equal(data, want) || strings.Contains(out, "kind:") || !strings.Contains(out, written) {
		t.Fatalf("exit %d, stdout %q, stderr %q, read %v", code, out, errOut, readErr)
	}
	if info, err := os.Stat(written); err != nil || info.Mode().Perm() != 0o644 {
		t.Fatalf("mode %v, error %v", info, err)
	}
	code, out, errOut = renderRun(t, "render", "--input-dir", input, "--output-dir", output, "--output", "json")
	var envelope struct {
		OK     bool
		Result struct {
			InputDir, OutputDir, EffectiveStatePath string
			Objects                                 int
		}
	}
	if err := json.Unmarshal([]byte(out), &envelope); err != nil || code != 0 || errOut != "" || !envelope.OK || envelope.Result.EffectiveStatePath != written || envelope.Result.InputDir != input || envelope.Result.Objects != 2 {
		t.Fatalf("exit %d, stdout %q, stderr %q, error %v", code, out, errOut, err)
	}
}

func TestComposedRenderRefusesALinkedTargetAndAnOutputInsideTheInput(t *testing.T) {
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		t.Skip("input admission is qualified for linux/amd64")
	}
	root := t.TempDir()
	directory := filepath.Join(root, "input")
	if err := os.Mkdir(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "environment.yaml"), []byte(serviceEnvironment+"\n---\n"+serviceHost), 0o600); err != nil {
		t.Fatal(err)
	}
	victim := filepath.Join(root, "victim")
	if err := os.WriteFile(victim, []byte("keep\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(root, "artifacts")
	if err := os.Mkdir(output, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(victim, filepath.Join(output, "effective-state.yaml")); err != nil {
		t.Fatal(err)
	}
	for _, row := range []struct{ name, output string }{{"linked target", output}, {"inside the input", filepath.Join(directory, "out")}} {
		code, out, errOut := renderRun(t, "render", "--input-dir", directory, "--output-dir", row.output)
		if code != 1 || out != "" || !strings.Contains(errOut, "render.publish") {
			t.Fatalf("%s: exit %d, stdout %q, stderr %q", row.name, code, out, errOut)
		}
	}
	if data, _ := os.ReadFile(victim); string(data) != "keep\n" {
		t.Fatalf("the link's target was written: %q", data)
	}
	if _, err := os.Stat(filepath.Join(directory, "out")); err == nil {
		t.Fatal("an output directory was created inside the input")
	}
}
