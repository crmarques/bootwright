package repocheck

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// allowedImports is the per-package import allowlist for production code
// (test files are exempt). Keys and values are package paths relative to the
// module root. A package may import only the internal packages in its row;
// "*" allows everything (the presentation composition root). The matrix is
// total: every package with non-test Go files must have a row, and every row
// must correspond to an existing package, so it cannot silently go stale.
var allowedImports = map[string][]string{
	"api/v1alpha1":   {},
	"cmd/bootwright": {"internal/cli"},

	// Presentation. cli is the composition root; only cli may import output.
	"internal/cli":        {"*"},
	"internal/cli/output": {},

	// Desired state: load/validate, point lookups, service graph, examples.
	"internal/state/desired":  {"api/v1alpha1", "internal/addons/inputs", "internal/entitlements", "internal/infra/artifacts", "internal/infra/media", "internal/nmstate", "internal/roles", "internal/state/graph", "internal/state/view", "internal/storage/topology"},
	"internal/state/graph":    {"api/v1alpha1", "internal/addons/inputs", "internal/infra/artifacts", "internal/roles", "internal/state/view"},
	"internal/state/scaffold": {"api/v1alpha1", "internal/roles"},
	"internal/state/view":     {"api/v1alpha1"},

	// Read-only advisory analysis over a validated State (operator-facing,
	// returns plain data; separate from the load/validate contract).
	"internal/state/advice": {"api/v1alpha1", "internal/storage/topology"},

	// Shared components.
	"internal/roles":     {"api/v1alpha1"},
	"internal/workspace": {"internal/host/localroot", "internal/host/managedroot", "internal/host/safefs"},
	"internal/secrets":   {"api/v1alpha1", "internal/host/callerio", "internal/host/localroot", "internal/host/safefs", "internal/state/view", "internal/storage/topology", "internal/workspace"},
	"internal/sshtrust":  {"api/v1alpha1", "internal/host/execution", "internal/host/safefs", "internal/infra/locality", "internal/secrets"},
	"internal/ownership": {"internal/host/safefs"},

	// Infra resolvers over state.
	"internal/infra/artifacts": {"api/v1alpha1", "internal/state/view"},
	"internal/infra/locality":  {"api/v1alpha1", "internal/state/view"},
	"internal/infra/media":     {"internal/host/managedroot", "internal/workspace"},
	"internal/infra/proxy":     {"api/v1alpha1", "internal/secrets", "internal/state/view"},

	"internal/entitlements": {"api/v1alpha1", "internal/secrets"},
	"internal/nmstate":      {},

	// Host primitives: generic, importable by everyone, import only host/*.
	"internal/host/become":      {"internal/host/safefs"},
	"internal/host/callerio":    {"internal/host/localroot"},
	"internal/host/execution":   {},
	"internal/host/localroot":   {"internal/host/execution"},
	"internal/host/managedroot": {"internal/host/safefs"},
	"internal/host/ptyexec":     {},
	"internal/host/safefs":      {},
	"internal/host/shellquote":  {},

	// Render: root is the only published surface; families are fs-free and
	// one-way (inventory -> installer, inventory -> ceph).
	"internal/render":           {"api/v1alpha1", "internal/host/managedroot", "internal/host/safefs", "internal/infra/media", "internal/ownership", "internal/render/ceph", "internal/render/installer", "internal/render/inventory", "internal/roles", "internal/secrets", "internal/state/view", "internal/storage/datafoundation", "internal/storage/topology"},
	"internal/render/ceph":      {"api/v1alpha1", "internal/addons/inputs", "internal/host/shellquote", "internal/storage/datafoundation", "internal/storage/topology"},
	"internal/render/installer": {"api/v1alpha1", "internal/infra/artifacts", "internal/infra/proxy", "internal/nmstate", "internal/secrets", "internal/state/view"},
	"internal/render/inventory": {"api/v1alpha1", "internal/addons/inputs", "internal/entitlements", "internal/host/shellquote", "internal/infra/artifacts", "internal/infra/locality", "internal/infra/media", "internal/nmstate", "internal/infra/proxy", "internal/ownership", "internal/render/ceph", "internal/render/installer", "internal/roles", "internal/secrets", "internal/sshtrust", "internal/state/graph", "internal/state/view", "internal/storage/cephprovider", "internal/storage/datafoundation", "internal/storage/topology"},

	// Convergence: root orchestrates; subpackages never import the root.
	"internal/converge":                   {"api/v1alpha1", "internal/addons/plan", "internal/addons/records", "internal/converge/ansible", "internal/converge/bundle", "internal/converge/workflow", "internal/infra/locality", "internal/ownership", "internal/render", "internal/roles", "internal/state/desired", "internal/state/view", "internal/storage/cephstate", "internal/workspace"},
	"internal/converge/ansible":           {"internal/host/callerio", "internal/host/localroot", "internal/host/ptyexec"},
	"internal/converge/ansible/runconfig": {"internal/converge/ansible", "internal/converge/bundle"},
	"internal/converge/bastion":           {"api/v1alpha1", "internal/host/execution", "internal/render", "internal/roles"},
	"internal/converge/bundle":            {},
	"internal/converge/workflow":          {"api/v1alpha1", "internal/addons/inputs", "internal/addons/oc", "internal/addons/plan", "internal/converge/ansible", "internal/converge/ansible/runconfig", "internal/converge/bundle", "internal/host/execution", "internal/host/safefs", "internal/host/shellquote", "internal/ownership", "internal/render", "internal/roles", "internal/secrets", "internal/sshtrust", "internal/state/graph", "internal/state/view", "internal/storage", "internal/storage/datafoundation", "internal/storage/topology"},

	// Storage and addons.
	"internal/storage":                {"api/v1alpha1", "internal/addons/inputs", "internal/host/safefs", "internal/state/view", "internal/storage/datafoundation"},
	"internal/storage/cephadopt":      {"api/v1alpha1", "internal/storage/cephdiff", "internal/storage/topology", "internal/workspace"},
	"internal/storage/cephprovider":   {"api/v1alpha1", "internal/entitlements", "internal/secrets"},
	"internal/storage/cephstate":      {},
	"internal/storage/cephdiff":       {"api/v1alpha1", "internal/storage/cephstate", "internal/storage/topology"},
	"internal/storage/datafoundation": {"api/v1alpha1", "internal/secrets", "internal/state/view", "internal/storage/topology"},
	"internal/storage/topology":       {"api/v1alpha1", "internal/state/view"},
	"internal/addons":                 {},
	"internal/addons/inputs":          {"api/v1alpha1"},
	"internal/addons/oc":              {"api/v1alpha1", "internal/addons", "internal/addons/plan", "internal/addons/records", "internal/addons/render", "internal/host/execution", "internal/host/shellquote"},
	"internal/addons/plan":            {"api/v1alpha1", "internal/addons", "internal/addons/render"},
	"internal/addons/records":         {"internal/host/safefs"},
	"internal/addons/render":          {"api/v1alpha1", "internal/addons"},

	// Operator-facing application services. They return plain data; cli
	// prints. None of them may import cli/output (see TestOnlyCLIImportsOutput).
	"internal/preflight":     {"api/v1alpha1", "internal/addons/plan", "internal/converge/bastion", "internal/host/callerio", "internal/host/execution", "internal/host/safefs", "internal/infra/locality", "internal/infra/media", "internal/secrets", "internal/sshtrust", "internal/state/view", "internal/storage/datafoundation", "internal/storage/topology", "internal/workspace"},
	"internal/status":        {"api/v1alpha1", "internal/addons/plan", "internal/addons/records", "internal/clusteraccess", "internal/converge/workflow", "internal/ownership", "internal/render", "internal/state/graph", "internal/state/view"},
	"internal/clusteraccess": {"api/v1alpha1", "internal/converge/workflow", "internal/host/safefs", "internal/host/shellquote", "internal/render", "internal/state/graph", "internal/state/view", "internal/storage/topology"},
}

type packageImports struct {
	files   []string
	imports map[string][]string // internal import -> files using it
}

// collectProductionImports walks non-test Go files under api/, cmd/, and
// internal/ and groups their module-internal imports by package directory.
func collectProductionImports(t *testing.T) map[string]*packageImports {
	t.Helper()
	root := repoRoot(t)
	packages := map[string]*packageImports{}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if skipGoImportDir(entry.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if !strings.HasPrefix(rel, "api/") && !strings.HasPrefix(rel, "cmd/") && !strings.HasPrefix(rel, "internal/") {
			return nil
		}
		pkg := filepath.ToSlash(filepath.Dir(rel))
		entryInfo := packages[pkg]
		if entryInfo == nil {
			entryInfo = &packageImports{imports: map[string][]string{}}
			packages[pkg] = entryInfo
		}
		entryInfo.files = append(entryInfo.files, rel)
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, spec := range file.Imports {
			importPath, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				return err
			}
			if importPath != modulePath && !strings.HasPrefix(importPath, modulePath+"/") {
				continue
			}
			dep := strings.TrimPrefix(strings.TrimPrefix(importPath, modulePath), "/")
			entryInfo.imports[dep] = append(entryInfo.imports[dep], rel)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return packages
}

func TestImportMatrixIsTotalAndRespected(t *testing.T) {
	packages := collectProductionImports(t)

	for pkg, info := range packages {
		row, ok := allowedImports[pkg]
		if !ok {
			t.Errorf("package %s has no row in allowedImports; add one deliberately instead of importing freely", pkg)
			continue
		}
		if len(row) == 1 && row[0] == "*" {
			continue
		}
		allowed := map[string]bool{}
		for _, dep := range row {
			allowed[dep] = true
		}
		deps := make([]string, 0, len(info.imports))
		for dep := range info.imports {
			deps = append(deps, dep)
		}
		sort.Strings(deps)
		for _, dep := range deps {
			if !allowed[dep] {
				t.Errorf("%s imports %s (from %s) but the import matrix does not allow it", pkg, dep, strings.Join(info.imports[dep], ", "))
			}
		}
	}

	for pkg := range allowedImports {
		if _, ok := packages[pkg]; !ok {
			t.Errorf("allowedImports has a row for %s but no non-test Go files exist there; remove or update the stale row", pkg)
		}
	}
}

// TestHostPrimitivesAreGeneric keeps internal/host free of domain knowledge:
// host packages may import only other host packages (and stdlib), never the
// schema or any domain package.
func TestHostPrimitivesAreGeneric(t *testing.T) {
	for pkg, info := range collectProductionImports(t) {
		if !strings.HasPrefix(pkg, "internal/host/") {
			continue
		}
		for dep, files := range info.imports {
			if !strings.HasPrefix(dep, "internal/host/") {
				t.Errorf("%s imports %s (from %s); host primitives must stay generic", pkg, dep, strings.Join(files, ", "))
			}
		}
	}
}

// TestOnlyCLIImportsOutput enforces the output invariant structurally: all
// human-facing text flows through internal/cli/output, and only the
// presentation layer may build it. Services return plain data.
func TestOnlyCLIImportsOutput(t *testing.T) {
	outputPath := modulePath + "/internal/cli/output"
	for _, ref := range collectGoImports(t) {
		if ref.path != outputPath {
			continue
		}
		switch {
		case strings.HasPrefix(ref.file, "cmd/"):
			continue
		case strings.HasPrefix(ref.file, "internal/cli/"):
			continue
		case strings.HasSuffix(ref.file, "_test.go"):
			continue
		default:
			t.Errorf("%s imports %s; services return plain data and cli renders it", ref.file, ref.path)
		}
	}
}

// TestRunOptionsConstructedOnlyInConvergeRoot keeps the workflow executor's
// RunOptions entrypoint behind the converge facade: presentation and other
// services must call converge.RunScopePreflight / ExecuteApply / ExecuteDestroy
// rather than hand-building workflow.RunOptions and calling workflow.Run. The
// converge root (and the workflow engine that defines RunOptions) are exempt.
func TestRunOptionsConstructedOnlyInConvergeRoot(t *testing.T) {
	root := repoRoot(t)
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if skipGoImportDir(entry.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if !strings.HasPrefix(rel, "internal/") && !strings.HasPrefix(rel, "cmd/") {
			return nil
		}
		if rel == "internal/converge" || strings.HasPrefix(rel, "internal/converge/") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(string(data), "workflow.RunOptions{") {
			t.Errorf("%s constructs workflow.RunOptions; route through the converge facade (RunScopePreflight/ExecuteApply/ExecuteDestroy) instead", rel)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// TestRoleNamesOnlyInRolesRegistry keeps internal/roles the single source of
// bootwright.core role and playbook names in Go production code.
func TestRoleNamesOnlyInRolesRegistry(t *testing.T) {
	for pkg, info := range collectProductionImports(t) {
		if pkg == "internal/roles" {
			continue
		}
		root := repoRoot(t)
		for _, rel := range info.files {
			data, err := os.ReadFile(filepath.Join(root, rel))
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(data), `"bootwright.core.`) {
				t.Errorf("%s spells a bootwright.core role or playbook name; use a constant from internal/roles", rel)
			}
		}
	}
}
