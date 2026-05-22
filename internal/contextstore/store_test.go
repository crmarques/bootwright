package contextstore

import (
	"os"
	"path/filepath"
	"testing"
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

func TestSafePurgeBaseDirRejectsHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	ctx, err := NewContext("lab", home)
	if err != nil {
		t.Fatal(err)
	}
	if err := SafePurgeBaseDir(ctx); err == nil {
		t.Fatal("SafePurgeBaseDir unexpectedly removed home")
	}
}
