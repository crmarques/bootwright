package workspace

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/host/managedroot"
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
	wantInput := filepath.Join(wantBase, InputDirName)
	if ctx.InputDir != wantInput {
		t.Fatalf("InputDir = %q, want %q", ctx.InputDir, wantInput)
	}
	if len(ctx.InputPaths) != 1 || ctx.InputPaths[0] != wantInput {
		t.Fatalf("InputPaths = %v, want [%q]", ctx.InputPaths, wantInput)
	}
	if ctx.RenderedDir != filepath.Join(wantBase, RenderedDirName) {
		t.Fatalf("RenderedDir = %q", ctx.RenderedDir)
	}
	if ctx.RunsDir != filepath.Join(wantBase, RunsDirName) {
		t.Fatalf("RunsDir = %q", ctx.RunsDir)
	}
	if ctx.ClustersDir != filepath.Join(wantBase, ClustersDirName) {
		t.Fatalf("ClustersDir = %q", ctx.ClustersDir)
	}
	if ctx.ManagedServicesDir != filepath.Join(wantBase, ManagedServicesName) {
		t.Fatalf("ManagedServicesDir = %q", ctx.ManagedServicesDir)
	}
	if ctx.ProviderStateDir != filepath.Join(wantBase, ProviderStateName) {
		t.Fatalf("ProviderStateDir = %q", ctx.ProviderStateDir)
	}
	if ctx.OwnershipDir != filepath.Join(wantBase, OwnershipName) {
		t.Fatalf("OwnershipDir = %q", ctx.OwnershipDir)
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
	if err := EnsureDirs(ctx); err != nil {
		t.Fatal(err)
	}
	store := Store{Current: "lab"}
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
	if current.InputDir != ctx.InputDir || len(current.InputPaths) != 1 || current.InputPaths[0] != ctx.InputDir {
		t.Fatalf("current input = %q %v, want %q", current.InputDir, current.InputPaths, ctx.InputDir)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	body := string(data)
	if !strings.Contains(body, "current: lab\n") {
		t.Fatalf("store did not save current context:\n%s", body)
	}
	if strings.Contains(body, "contexts:") || strings.Contains(body, "baseDir") || strings.Contains(body, "inputDir") {
		t.Fatalf("store unexpectedly persisted derived context data:\n%s", body)
	}
}

func TestLoadToleratesAndSaveStripsRetiredContextList(t *testing.T) {
	path := filepath.Join(t.TempDir(), "contexts.yaml")
	root := filepath.Join(t.TempDir(), "bootwright-root")
	t.Cleanup(SetRootDirForTest(root))
	ctx, err := NewContext("lab")
	if err != nil {
		t.Fatal(err)
	}
	if err := EnsureDirs(ctx); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("current: lab\ncontexts:\n  - lab\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if store.Current != "lab" {
		t.Fatalf("Current = %q, want lab", store.Current)
	}
	if err := Save(path, store); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "contexts:") {
		t.Fatalf("retired contexts list was not stripped:\n%s", body)
	}
}

func TestCurrentResolvesManagedContext(t *testing.T) {
	root := filepath.Join(t.TempDir(), "bootwright-root")
	t.Cleanup(SetRootDirForTest(root))
	if _, err := Current(Store{Current: "lab"}); err == nil {
		t.Fatal("Current accepted missing shared context")
	} else if !strings.Contains(err.Error(), "not available in shared storage") {
		t.Fatalf("Current returned unclear stale-current error: %v", err)
	}
	ctx, err := NewContext("lab")
	if err != nil {
		t.Fatal(err)
	}
	if err := EnsureDirs(ctx); err != nil {
		t.Fatal(err)
	}
	current, err := Current(Store{Current: "lab"})
	if err != nil {
		t.Fatalf("Current failed for a managed context: %v", err)
	}
	if current.BaseDir != ctx.BaseDir {
		t.Fatalf("BaseDir = %q, want %q", current.BaseDir, ctx.BaseDir)
	}
	if current.InputDir != ctx.InputDir {
		t.Fatalf("InputDir = %q, want %q", current.InputDir, ctx.InputDir)
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
		ctx.ClustersDir,
		ctx.RunsDir,
		ctx.ManagedServicesDir,
		ctx.ProviderStateDir,
		ctx.OwnershipDir,
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

func TestReplaceInputDirCopiesTreeAndSurvivesSourceDeletion(t *testing.T) {
	root := filepath.Join(t.TempDir(), "bootwright-root")
	t.Cleanup(SetRootDirForTest(root))
	ctx, err := NewContext("lab")
	if err != nil {
		t.Fatal(err)
	}
	if err := EnsureDirs(ctx); err != nil {
		t.Fatal(err)
	}

	// A source tree with YAML, a referenced non-YAML file (e.g. a file:-sourced
	// secret), a nested directory, and a VCS directory that must be skipped.
	source := t.TempDir()
	writeFile(t, filepath.Join(source, "environment.yaml"), "kind: Environment\n")
	writeFile(t, filepath.Join(source, "pull-secret.txt"), "secret-bytes\n")
	writeFile(t, filepath.Join(source, "nested", "machine.yaml"), "kind: Machine\n")
	writeFile(t, filepath.Join(source, ".git", "config"), "[core]\n")

	if err := ReplaceInputDir(ctx, source); err != nil {
		t.Fatal(err)
	}
	// The whole tree (not just YAML) is copied so referenced material survives.
	mustExist(t, filepath.Join(ctx.InputDir, "environment.yaml"))
	mustExist(t, filepath.Join(ctx.InputDir, "pull-secret.txt"))
	mustExist(t, filepath.Join(ctx.InputDir, "nested", "machine.yaml"))
	if _, err := os.Stat(filepath.Join(ctx.InputDir, ".git")); !os.IsNotExist(err) {
		t.Fatalf("ReplaceInputDir copied the .git directory: %v", err)
	}
	if mode := statMode(t, filepath.Join(ctx.InputDir, "pull-secret.txt")); mode != 0o600 {
		t.Fatalf("copied file mode = %#o, want 0600", mode)
	}

	// Deleting the source must not affect the self-contained context.
	if err := os.RemoveAll(source); err != nil {
		t.Fatal(err)
	}
	mustExist(t, filepath.Join(ctx.InputDir, "environment.yaml"))
	mustExist(t, filepath.Join(ctx.InputDir, "pull-secret.txt"))

	// A second update fully replaces the input; stale files do not linger.
	second := t.TempDir()
	writeFile(t, filepath.Join(second, "only.yaml"), "kind: Environment\n")
	if err := ReplaceInputDir(ctx, second); err != nil {
		t.Fatal(err)
	}
	mustExist(t, filepath.Join(ctx.InputDir, "only.yaml"))
	if _, err := os.Stat(filepath.Join(ctx.InputDir, "environment.yaml")); !os.IsNotExist(err) {
		t.Fatalf("ReplaceInputDir kept stale input from the previous source: %v", err)
	}
	// No staging directory is left behind under the context base directory.
	entries, err := os.ReadDir(ctx.BaseDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".input.tmp-") {
			t.Fatalf("ReplaceInputDir left a staging directory: %s", entry.Name())
		}
	}
}

func TestReplaceInputDirRejectsSymlinkInSource(t *testing.T) {
	root := filepath.Join(t.TempDir(), "bootwright-root")
	t.Cleanup(SetRootDirForTest(root))
	ctx, err := NewContext("lab")
	if err != nil {
		t.Fatal(err)
	}
	if err := EnsureDirs(ctx); err != nil {
		t.Fatal(err)
	}
	source := t.TempDir()
	writeFile(t, filepath.Join(source, "environment.yaml"), "kind: Environment\n")
	if err := os.Symlink(filepath.Join(source, "environment.yaml"), filepath.Join(source, "link.yaml")); err != nil {
		t.Fatal(err)
	}
	if err := ReplaceInputDir(ctx, source); err == nil || !strings.Contains(err.Error(), "must not contain symlinks") {
		t.Fatalf("ReplaceInputDir on a tree with a symlink = %v, want symlink rejection", err)
	}
}

func TestValidateInputDirNamesContextAndRemediation(t *testing.T) {
	root := filepath.Join(t.TempDir(), "bootwright-root")
	t.Cleanup(SetRootDirForTest(root))
	ctx, err := NewContext("lab")
	if err != nil {
		t.Fatal(err)
	}
	if err := EnsureDirs(ctx); err != nil {
		t.Fatal(err)
	}
	if err := ValidateInputDir(ctx); err != nil {
		t.Fatalf("ValidateInputDir on a populated context failed: %v", err)
	}
	if err := os.RemoveAll(ctx.InputDir); err != nil {
		t.Fatal(err)
	}
	err = ValidateInputDir(ctx)
	if err == nil {
		t.Fatal("ValidateInputDir accepted a missing input directory")
	}
	for _, want := range []string{`context "lab"`, ctx.InputDir, "is missing", "context update -f", "context init lab -f <dir> --yes"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q missing %q", err, want)
		}
	}
}

func TestResolveWorkspaceDirRejectsMissingSymlinkAndFile(t *testing.T) {
	if _, err := ResolveWorkspaceDir(filepath.Join(t.TempDir(), "absent")); err == nil {
		t.Fatal("ResolveWorkspaceDir accepted a missing directory")
	}
	target := t.TempDir()
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if _, err := ResolveWorkspaceDir(link); err == nil {
		t.Fatal("ResolveWorkspaceDir accepted a symlink")
	}
	file := filepath.Join(t.TempDir(), "workspace.yaml")
	if err := os.WriteFile(file, []byte("data\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ResolveWorkspaceDir(file); err == nil {
		t.Fatal("ResolveWorkspaceDir accepted a file")
	}
	got, err := ResolveWorkspaceDir(target)
	if err != nil {
		t.Fatal(err)
	}
	if got != target {
		t.Fatalf("ResolveWorkspaceDir = %q, want %q", got, target)
	}
}

func TestResolveWorkspaceDirRejectsSourceInsideBootwrightRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "bootwright-root")
	t.Cleanup(SetRootDirForTest(root))
	inside := filepath.Join(root, "workspace")
	if err := os.MkdirAll(inside, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := ResolveWorkspaceDir(inside); err == nil {
		t.Fatal("ResolveWorkspaceDir accepted a source inside the Bootwright state directory")
	} else if !strings.Contains(err.Error(), "must live outside the Bootwright state directory") {
		t.Fatalf("ResolveWorkspaceDir returned unclear inside-root error: %v", err)
	}
}

func TestCurrentRejectsMissingCurrentContext(t *testing.T) {
	root := filepath.Join(t.TempDir(), "bootwright-root")
	t.Cleanup(SetRootDirForTest(root))
	other, err := NewContext("other")
	if err != nil {
		t.Fatal(err)
	}
	if err := EnsureDirs(other); err != nil {
		t.Fatal(err)
	}
	store := Store{Current: "lab"}
	if _, err := Current(store); err == nil {
		t.Fatal("Current accepted a current context absent from shared storage")
	}
}

func TestListContextsReturnsOnlyUsableContextDirs(t *testing.T) {
	root := filepath.Join(t.TempDir(), "bootwright-root")
	t.Cleanup(SetRootDirForTest(root))
	lab, err := NewContext("lab")
	if err != nil {
		t.Fatal(err)
	}
	if err := EnsureDirs(lab); err != nil {
		t.Fatal(err)
	}
	contextsDir, err := ContextsDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(contextsDir, "unmarked"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(contextsDir, "Invalid"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(contextsDir, "file"), []byte("data\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(lab.BaseDir, filepath.Join(contextsDir, "linked")); err != nil {
		t.Fatal(err)
	}
	contexts, err := ListContexts()
	if err != nil {
		t.Fatal(err)
	}
	if len(contexts) != 1 || contexts[0].Name != "lab" {
		t.Fatalf("contexts = %+v, want only lab", contexts)
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

func TestSafePurgeBaseDirRejectsHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	ctx := Context{
		Name:               "lab",
		BaseDir:            home,
		InputDir:           filepath.Join(home, InputDirName),
		RenderedDir:        filepath.Join(home, RenderedDirName),
		SecretsDir:         filepath.Join(home, SecretsDirName),
		ClustersDir:        filepath.Join(home, ClustersDirName),
		RunsDir:            filepath.Join(home, RunsDirName),
		ManagedServicesDir: filepath.Join(home, ManagedServicesName),
		ProviderStateDir:   filepath.Join(home, ProviderStateName),
		OwnershipDir:       filepath.Join(home, OwnershipName),
	}
	if err := SafePurgeBaseDir(ctx); err == nil {
		t.Fatal("SafePurgeBaseDir unexpectedly removed home")
	}
}

func TestSafePurgeBaseDirRequiresMarker(t *testing.T) {
	baseDir := filepath.Join(t.TempDir(), "lab")
	ctx := Context{
		Name:               "lab",
		BaseDir:            baseDir,
		InputDir:           filepath.Join(baseDir, InputDirName),
		RenderedDir:        filepath.Join(baseDir, RenderedDirName),
		SecretsDir:         filepath.Join(baseDir, SecretsDirName),
		ClustersDir:        filepath.Join(baseDir, ClustersDirName),
		RunsDir:            filepath.Join(baseDir, RunsDirName),
		ManagedServicesDir: filepath.Join(baseDir, ManagedServicesName),
		ProviderStateDir:   filepath.Join(baseDir, ProviderStateName),
		OwnershipDir:       filepath.Join(baseDir, OwnershipName),
	}
	if err := os.MkdirAll(baseDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := SafePurgeBaseDir(ctx); err == nil {
		t.Fatal("SafePurgeBaseDir removed an unmarked base directory")
	}
}

func writeFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}

func mustExist(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected %s to exist: %v", path, err)
	}
}

func statMode(t *testing.T, path string) os.FileMode {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info.Mode().Perm()
}
