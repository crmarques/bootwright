package contextstore

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/managedroot"
)

func TestNewContextDefaultsUnderHomeBootwright(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	ctx, err := NewContext("lab", "")
	if err != nil {
		t.Fatal(err)
	}
	wantBase := filepath.Join(home, "bootwright", "lab")
	if ctx.BaseDir != wantBase {
		t.Fatalf("BaseDir = %q, want %q", ctx.BaseDir, wantBase)
	}
	if ctx.InputDir != filepath.Join(wantBase, InputDirName) {
		t.Fatalf("InputDir = %q", ctx.InputDir)
	}
}

func TestStoreRoundTripCurrentContext(t *testing.T) {
	path := filepath.Join(t.TempDir(), "contexts.yaml")
	ctx, err := NewContext("lab", filepath.Join(t.TempDir(), "lab"))
	if err != nil {
		t.Fatal(err)
	}
	store := Store{Current: "lab", Contexts: map[string]Context{"lab": ctx}}
	if err := Save(path, store); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	current, err := Current(loaded)
	if err != nil {
		t.Fatal(err)
	}
	if current.Name != "lab" || current.BaseDir != ctx.BaseDir {
		t.Fatalf("current = %+v, want %+v", current, ctx)
	}
}

func TestNewContextRejectsUnsafeBaseDir(t *testing.T) {
	if _, err := NewContext("lab", string(filepath.Separator)); err == nil {
		t.Fatal("NewContext accepted filesystem root")
	}
}

func TestNewContextRejectsExistingNonEmptyUnmarkedBaseDir(t *testing.T) {
	baseDir := filepath.Join(t.TempDir(), "lab")
	if err := os.MkdirAll(baseDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(baseDir, "owned-by-someone-else"), []byte("data\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewContext("lab", baseDir); err == nil {
		t.Fatal("NewContext accepted an existing non-empty unmarked base directory")
	}
}

func TestEnsureDirsMarksContextBaseDir(t *testing.T) {
	ctx, err := NewContext("lab", filepath.Join(t.TempDir(), "lab"))
	if err != nil {
		t.Fatal(err)
	}
	if err := EnsureDirs(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(ctx.BaseDir, managedroot.MarkerName)); err != nil {
		t.Fatalf("context marker missing: %v", err)
	}
}

func TestCurrentRejectsUnsafeContextLayout(t *testing.T) {
	baseDir := filepath.Join(t.TempDir(), "lab")
	ctx, err := NewContext("lab", baseDir)
	if err != nil {
		t.Fatal(err)
	}
	ctx.StateDir = string(filepath.Separator)
	store := Store{Current: "lab", Contexts: map[string]Context{"lab": ctx}}
	if _, err := Current(store); err == nil {
		t.Fatal("Current accepted context with unsafe stateDir")
	}
}

func TestValidateNameRejectsInvalidNames(t *testing.T) {
	for _, name := range []string{"", "Lab", "-lab", "lab_", "lab."} {
		t.Run(name, func(t *testing.T) {
			if err := ValidateName(name); err == nil {
				t.Fatalf("ValidateName(%q) unexpectedly succeeded", name)
			}
		})
	}
}

func TestImportInputsCollisionAndReplace(t *testing.T) {
	source := t.TempDir()
	inputDir := filepath.Join(t.TempDir(), "input-files")
	if err := os.WriteFile(filepath.Join(source, "environment.yaml"), []byte("one\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ImportInputs([]string{source}, inputDir, false); err != nil {
		t.Fatal(err)
	}
	if _, err := ImportInputs([]string{source}, inputDir, false); err == nil {
		t.Fatal("second import without replacement unexpectedly succeeded")
	}
	if err := os.WriteFile(filepath.Join(source, "environment.yaml"), []byte("two\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ImportInputs([]string{source}, inputDir, true); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(inputDir, "environment.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "two\n" {
		t.Fatalf("imported body = %q", string(body))
	}
}

func TestImportInputsRejectsSourceInsideTarget(t *testing.T) {
	inputDir := filepath.Join(t.TempDir(), "input-files")
	if err := os.MkdirAll(inputDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(inputDir, "environment.yaml"), []byte("one\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := ImportInputs([]string{inputDir}, inputDir, true)
	if err == nil {
		t.Fatal("self-import unexpectedly succeeded")
	}
	if !strings.Contains(err.Error(), "must not be inside target input directory") {
		t.Fatalf("error %q does not reject self-import", err)
	}
}

func TestImportInputsRejectsYAMLSymlink(t *testing.T) {
	source := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.yaml")
	if err := os.WriteFile(outside, []byte("apiVersion: bootwright.io/v1alpha1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(source, "environment.yaml")); err != nil {
		t.Fatal(err)
	}
	inputDir := filepath.Join(t.TempDir(), "input-files")
	if _, err := ImportInputs([]string{source}, inputDir, false); err == nil {
		t.Fatal("ImportInputs accepted a YAML symlink")
	}
}

func TestSafePurgeBaseDirRejectsHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	ctx := Context{
		Name:       "lab",
		BaseDir:    home,
		InputDir:   filepath.Join(home, InputDirName),
		StateDir:   filepath.Join(home, StateDirName),
		SecretsDir: filepath.Join(home, SecretsDirName),
	}
	if err := SafePurgeBaseDir(ctx); err == nil {
		t.Fatal("SafePurgeBaseDir unexpectedly removed home")
	}
}

func TestSafePurgeBaseDirRequiresMarker(t *testing.T) {
	baseDir := filepath.Join(t.TempDir(), "lab")
	ctx := Context{
		Name:       "lab",
		BaseDir:    baseDir,
		InputDir:   filepath.Join(baseDir, InputDirName),
		StateDir:   filepath.Join(baseDir, StateDirName),
		SecretsDir: filepath.Join(baseDir, SecretsDirName),
	}
	if err := os.MkdirAll(baseDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := SafePurgeBaseDir(ctx); err == nil {
		t.Fatal("SafePurgeBaseDir removed an unmarked base directory")
	}
}
