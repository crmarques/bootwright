package contextstore

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/managedroot"
)

func TestNewContextDefaultsUnderHomeBootwright(t *testing.T) {
	root := filepath.Join(t.TempDir(), "bootwright-root")
	t.Cleanup(SetRootDirForTest(root))

	ctx, err := NewContext("lab")
	if err != nil {
		t.Fatal(err)
	}
	wantBase := filepath.Join(root, "contexts", "lab")
	if ctx.BaseDir != wantBase {
		t.Fatalf("BaseDir = %q, want %q", ctx.BaseDir, wantBase)
	}
	if ctx.InputDir != filepath.Join(wantBase, InputDirName) {
		t.Fatalf("InputDir = %q", ctx.InputDir)
	}
	if ctx.RenderedDir != filepath.Join(wantBase, RenderedDirName) {
		t.Fatalf("RenderedDir = %q", ctx.RenderedDir)
	}
	if ctx.RunsDir != filepath.Join(wantBase, RunsDirName) {
		t.Fatalf("RunsDir = %q", ctx.RunsDir)
	}
	if ctx.ManagedDir != filepath.Join(wantBase, ManagedDirName) {
		t.Fatalf("ManagedDir = %q", ctx.ManagedDir)
	}
}

func TestDefaultRegistryPathIgnoresSudoUser(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SUDO_USER", "operator")
	t.Setenv("SUDO_UID", "1000")
	t.Setenv("SUDO_GID", "1000")

	got, err := DefaultRegistryPath()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(home, ".bootwright", RegistryFileName)
	if got != want {
		t.Fatalf("DefaultRegistryPath = %q, want %q", got, want)
	}
}

func TestStoreRoundTripCurrentContext(t *testing.T) {
	path := filepath.Join(t.TempDir(), "contexts.yaml")
	root := filepath.Join(t.TempDir(), "bootwright-root")
	t.Cleanup(SetRootDirForTest(root))
	ctx, err := NewContext("lab")
	if err != nil {
		t.Fatal(err)
	}
	store := Store{Current: "lab", Contexts: []string{"lab"}}
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
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	body := string(data)
	if !strings.Contains(body, "contexts:\n    - lab\n") {
		t.Fatalf("store was not saved in strict list format:\n%s", body)
	}
	if strings.Contains(body, "baseDir") || strings.Contains(body, "inputDir") {
		t.Fatalf("store unexpectedly persisted derived paths:\n%s", body)
	}
}

func TestCurrentDerivesContextWithoutTraversingProtectedRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "bootwright-root")
	t.Cleanup(SetRootDirForTest(root))
	contextsDir := filepath.Join(root, "contexts")
	if err := os.MkdirAll(contextsDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(contextsDir, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(contextsDir, 0o700) })

	current, err := Current(Store{Current: "lab", Contexts: []string{"lab"}})
	if err != nil {
		t.Fatal(err)
	}
	if current.BaseDir != filepath.Join(contextsDir, "lab") {
		t.Fatalf("BaseDir = %q", current.BaseDir)
	}
}

func TestLoadRejectsLegacyContextMapRegistry(t *testing.T) {
	path := filepath.Join(t.TempDir(), "contexts.yaml")
	body := []byte("current: lab\ncontexts:\n  lab:\n    baseDir: /tmp/lab\n")
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("Load accepted legacy context map registry")
	} else if !strings.Contains(err.Error(), "legacy context registry map is not supported") {
		t.Fatalf("Load returned unclear legacy registry error: %v", err)
	}
}

func TestNewContextDerivesFixedPathEvenWhenContextDirExists(t *testing.T) {
	root := filepath.Join(t.TempDir(), "bootwright-root")
	t.Cleanup(SetRootDirForTest(root))
	baseDir := filepath.Join(root, "contexts", "lab")
	if err := os.MkdirAll(baseDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(baseDir, "owned-by-someone-else"), []byte("data\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, err := NewContext("lab")
	if err != nil {
		t.Fatal(err)
	}
	if ctx.BaseDir != baseDir {
		t.Fatalf("BaseDir = %q, want %q", ctx.BaseDir, baseDir)
	}
}

func TestEnsureDirsMarksContextBaseDir(t *testing.T) {
	root := filepath.Join(t.TempDir(), "bootwright-root")
	t.Cleanup(SetRootDirForTest(root))
	ctx, err := NewContext("lab")
	if err != nil {
		t.Fatal(err)
	}
	if err := EnsureDirs(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(ctx.BaseDir, managedroot.MarkerName)); err != nil {
		t.Fatalf("context marker missing: %v", err)
	}
	for _, dir := range []string{
		filepath.Join(root, CacheDirName),
		filepath.Join(root, "contexts"),
		ctx.BaseDir,
		ctx.InputDir,
		ctx.RenderedDir,
		ctx.SecretsDir,
		ctx.RuntimeDir,
		ctx.RunsDir,
		ctx.ManagedDir,
		filepath.Join(ctx.ManagedDir, "services"),
		filepath.Join(ctx.ManagedDir, "substrate"),
	} {
		info, err := os.Stat(dir)
		if err != nil {
			t.Fatalf("expected directory %s: %v", dir, err)
		}
		if !info.IsDir() {
			t.Fatalf("%s is not a directory", dir)
		}
		if got := info.Mode().Perm(); got != 0o700 {
			t.Fatalf("%s mode = %#o, want 0700", dir, got)
		}
	}
}

func TestPreparedContextImportYesReplacesExistingUnmarkedContextDir(t *testing.T) {
	root := filepath.Join(t.TempDir(), "bootwright-root")
	t.Cleanup(SetRootDirForTest(root))
	ctx, err := NewContext("lab")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := managedroot.Ensure(root, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(ctx.BaseDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ctx.BaseDir, "stale"), []byte("stale\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	source := t.TempDir()
	if err := os.WriteFile(filepath.Join(source, "environment.yaml"), []byte("fresh\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	prepared, err := PrepareContextImport("lab", []string{source})
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Cancel()
	if _, err := prepared.Commit(true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(ctx.BaseDir, "stale")); !os.IsNotExist(err) {
		t.Fatalf("stale context file still exists or stat failed unexpectedly: %v", err)
	}
	if got, err := os.ReadFile(filepath.Join(ctx.InputDir, "environment.yaml")); err != nil || string(got) != "fresh\n" {
		t.Fatalf("imported environment.yaml = %q, err=%v", got, err)
	}
}

func TestCurrentRejectsMissingCurrentContext(t *testing.T) {
	store := Store{Current: "lab", Contexts: []string{"other"}}
	if _, err := Current(store); err == nil {
		t.Fatal("Current accepted a current context absent from the registry")
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
	inputDir := filepath.Join(t.TempDir(), InputDirName)
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

func TestImportInputsPreservesSubdirectoryLayout(t *testing.T) {
	source := t.TempDir()
	files := map[string]string{
		"environment.yaml":                  "env\n",
		"shared/provider.yaml":              "provider\n",
		"demo-ocp-a/container-cluster.yaml": "cluster-a\n",
		"demo-ocp-b/container-cluster.yaml": "cluster-b\n",
	}
	for name, content := range files {
		path := filepath.Join(source, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatalf("create %s: %v", filepath.Dir(path), err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	inputDir := filepath.Join(t.TempDir(), InputDirName)
	imported, err := ImportInputs([]string{source}, inputDir, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(imported) != len(files) {
		t.Fatalf("imported = %v, want %d files", imported, len(files))
	}
	for name, content := range files {
		body, err := os.ReadFile(filepath.Join(inputDir, name))
		if err != nil {
			t.Fatalf("read imported %s: %v", name, err)
		}
		if string(body) != content {
			t.Fatalf("imported %s = %q, want %q", name, string(body), content)
		}
	}
}

func TestImportInputsRejectsSourceInsideTarget(t *testing.T) {
	inputDir := filepath.Join(t.TempDir(), InputDirName)
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
	inputDir := filepath.Join(t.TempDir(), InputDirName)
	if _, err := ImportInputs([]string{source}, inputDir, false); err == nil {
		t.Fatal("ImportInputs accepted a YAML symlink")
	}
}

func TestImportInputsSkipsNonWorkspaceDirs(t *testing.T) {
	source := t.TempDir()
	if err := os.WriteFile(filepath.Join(source, "environment.yaml"), []byte("env\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{"vendor", "node_modules", ".cache"} {
		nested := filepath.Join(source, dir)
		if err := os.MkdirAll(nested, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(nested, "ignored.yaml"), []byte("ignored\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	inputDir := filepath.Join(t.TempDir(), InputDirName)
	imported, err := ImportInputs([]string{source}, inputDir, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(imported) != 1 || filepath.Base(imported[0]) != "environment.yaml" {
		t.Fatalf("imported = %v, want only environment.yaml", imported)
	}
}

func TestSafePurgeBaseDirRejectsHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	ctx := Context{
		Name:        "lab",
		BaseDir:     home,
		InputDir:    filepath.Join(home, InputDirName),
		RenderedDir: filepath.Join(home, RenderedDirName),
		SecretsDir:  filepath.Join(home, SecretsDirName),
		RuntimeDir:  filepath.Join(home, RuntimeDirName),
		RunsDir:     filepath.Join(home, RunsDirName),
		ManagedDir:  filepath.Join(home, ManagedDirName),
	}
	if err := SafePurgeBaseDir(ctx); err == nil {
		t.Fatal("SafePurgeBaseDir unexpectedly removed home")
	}
}

func TestSafePurgeBaseDirRequiresMarker(t *testing.T) {
	baseDir := filepath.Join(t.TempDir(), "lab")
	ctx := Context{
		Name:        "lab",
		BaseDir:     baseDir,
		InputDir:    filepath.Join(baseDir, InputDirName),
		RenderedDir: filepath.Join(baseDir, RenderedDirName),
		SecretsDir:  filepath.Join(baseDir, SecretsDirName),
		RuntimeDir:  filepath.Join(baseDir, RuntimeDirName),
		RunsDir:     filepath.Join(baseDir, RunsDirName),
		ManagedDir:  filepath.Join(baseDir, ManagedDirName),
	}
	if err := os.MkdirAll(baseDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := SafePurgeBaseDir(ctx); err == nil {
		t.Fatal("SafePurgeBaseDir removed an unmarked base directory")
	}
}
