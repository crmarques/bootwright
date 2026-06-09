package contextstore

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/runtime/root/managedroot"
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
	if ctx.InputDir != "" || len(ctx.InputPaths) != 0 {
		t.Fatalf("InputDir/InputPaths should be unset before workspace resolution, got %q %v", ctx.InputDir, ctx.InputPaths)
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
	workspace := t.TempDir()
	if err := WriteInputSource(ctx, workspace); err != nil {
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
	if current.InputDir != workspace || len(current.InputPaths) != 1 || current.InputPaths[0] != workspace {
		t.Fatalf("current workspace = %q %v, want %q", current.InputDir, current.InputPaths, workspace)
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

func TestCurrentRequiresSharedContextStorage(t *testing.T) {
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
	if _, err := Current(Store{Current: "lab"}); err == nil {
		t.Fatal("Current accepted a context without a recorded workspace path")
	} else if !strings.Contains(err.Error(), "no recorded workspace path") || !strings.Contains(err.Error(), "context init lab") {
		t.Fatalf("Current returned unclear missing-workspace error: %v", err)
	}
	workspace := t.TempDir()
	if err := WriteInputSource(ctx, workspace); err != nil {
		t.Fatal(err)
	}
	current, err := Current(Store{Current: "lab"})
	if err != nil {
		t.Fatal(err)
	}
	if current.BaseDir != ctx.BaseDir {
		t.Fatalf("BaseDir = %q, want %q", current.BaseDir, ctx.BaseDir)
	}
	if current.InputDir != workspace {
		t.Fatalf("InputDir = %q, want %q", current.InputDir, workspace)
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

func TestInputSourceRoundTripAndRepoint(t *testing.T) {
	root := filepath.Join(t.TempDir(), "bootwright-root")
	t.Cleanup(SetRootDirForTest(root))
	ctx, err := NewContext("lab")
	if err != nil {
		t.Fatal(err)
	}
	if err := EnsureDirs(ctx); err != nil {
		t.Fatal(err)
	}
	first := t.TempDir()
	if err := WriteInputSource(ctx, first); err != nil {
		t.Fatal(err)
	}
	resolved, err := ResolveExistingContext("lab")
	if err != nil {
		t.Fatal(err)
	}
	if resolved.InputDir != first || len(resolved.InputPaths) != 1 || resolved.InputPaths[0] != first {
		t.Fatalf("resolved workspace = %q %v, want %q", resolved.InputDir, resolved.InputPaths, first)
	}
	// Re-writing the source is how `context init --yes` re-points a context.
	second := t.TempDir()
	if err := WriteInputSource(ctx, second); err != nil {
		t.Fatal(err)
	}
	resolved, err = ResolveExistingContext("lab")
	if err != nil {
		t.Fatal(err)
	}
	if resolved.InputDir != second {
		t.Fatalf("re-pointed workspace = %q, want %q", resolved.InputDir, second)
	}
}

func TestResolveExistingContextIgnoresLegacyInputCopy(t *testing.T) {
	root := filepath.Join(t.TempDir(), "bootwright-root")
	t.Cleanup(SetRootDirForTest(root))
	ctx, err := NewContext("lab")
	if err != nil {
		t.Fatal(err)
	}
	if err := EnsureDirs(ctx); err != nil {
		t.Fatal(err)
	}
	// A pre-redesign context has a copied input/ directory and no recorded
	// workspace path. It must fail with the named error; the copy is never read.
	legacyInput := filepath.Join(ctx.BaseDir, "input")
	if err := os.MkdirAll(legacyInput, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacyInput, "environment.yaml"), []byte("legacy\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = ResolveExistingContext("lab")
	if err == nil {
		t.Fatal("ResolveExistingContext accepted a context without a recorded workspace path")
	}
	for _, want := range []string{"lab", "no recorded workspace path", "bootwright context init lab -f <workspace>"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q missing %q", err, want)
		}
	}
}

func TestValidateInputSourceNamesContextPathAndRemediation(t *testing.T) {
	ctx := Context{Name: "lab"}
	missing := filepath.Join(t.TempDir(), "moved-away")
	ctx = ctx.WithInputSource(missing)
	err := ValidateInputSource(ctx)
	if err == nil {
		t.Fatal("ValidateInputSource accepted a missing workspace")
	}
	for _, want := range []string{`context "lab"`, missing, "does not exist", "must exist and be readable", "bootwright context init lab -f <dir>"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q missing %q", err, want)
		}
	}

	file := filepath.Join(t.TempDir(), "workspace.yaml")
	if err := os.WriteFile(file, []byte("data\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	err = ValidateInputSource(ctx.WithInputSource(file))
	if err == nil || !strings.Contains(err.Error(), "is not a directory") {
		t.Fatalf("ValidateInputSource on a file = %v, want not-a-directory error", err)
	}

	ok := t.TempDir()
	if err := ValidateInputSource(ctx.WithInputSource(ok)); err != nil {
		t.Fatalf("ValidateInputSource on a readable directory failed: %v", err)
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

func TestWriteInputSourceRejectsWorkspaceInsideBootwrightRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "bootwright-root")
	t.Cleanup(SetRootDirForTest(root))
	ctx, err := NewContext("lab")
	if err != nil {
		t.Fatal(err)
	}
	if err := EnsureDirs(ctx); err != nil {
		t.Fatal(err)
	}
	inside := filepath.Join(root, "workspace")
	if err := os.MkdirAll(inside, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := WriteInputSource(ctx, inside); err == nil {
		t.Fatal("WriteInputSource accepted a workspace inside the Bootwright state directory")
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
