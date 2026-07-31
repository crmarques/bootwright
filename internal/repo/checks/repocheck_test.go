package repocheck

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/state/scaffold"
	"go.yaml.in/yaml/v3"
)

var spelledKindCounts = map[int]string{
	5: "five", 6: "six", 7: "seven", 8: "eight", 9: "nine", 10: "ten",
	11: "eleven", 12: "twelve", 13: "thirteen", 14: "fourteen",
	15: "fifteen", 16: "sixteen", 17: "seventeen", 18: "eighteen",
	19: "nineteen", 20: "twenty", 21: "twenty-one", 22: "twenty-two",
	23: "twenty-three", 24: "twenty-four", 25: "twenty-five",
}

func TestREADMEDescribesDesiredStateModel(t *testing.T) {
	readme := readRepoFile(t, "README.md")
	kinds := v1alpha1.AuthoredKindAccessors()
	spelled, ok := spelledKindCounts[len(kinds)]
	if !ok {
		t.Fatalf("no spelled-out form for %d authored kinds; extend spelledKindCounts", len(kinds))
	}
	required := []string{spelled + " kinds"}
	for _, kind := range kinds {
		required = append(required, "`"+kind.Kind+"`")
	}
	for _, phrase := range required {
		if !strings.Contains(readme, phrase) {
			t.Fatalf("README.md missing %q", phrase)
		}
	}
	rejected := []string{
		"`Host`",
		"`ClusterInfra`",
		"`StorageClusterBinding`",
		"`HostPool`",
		"`Playbook`",
		"providerRefs",
	}
	for count, word := range spelledKindCounts {
		if count != len(kinds) {
			rejected = append(rejected, word+" kinds")
		}
	}
	for _, phrase := range rejected {
		if strings.Contains(readme, phrase) {
			t.Fatalf("README.md still contains stale model text %q", phrase)
		}
	}
}

func TestCurrentDefinitionDocsUseNewSchemaTerms(t *testing.T) {
	files := []string{
		"README.md",
		"specs/architecture.md",
		"specs/state-model.md",
		"ansible/collections/ansible_collections/bootwright/core/docs/vars-contract.md",
	}
	docsDir := filepath.Join(repoRoot(t), "docs")
	if err := filepath.WalkDir(docsDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || filepath.Ext(path) != ".md" {
			return nil
		}
		rel, err := filepath.Rel(repoRoot(t), path)
		if err != nil {
			return err
		}
		files = append(files, filepath.ToSlash(rel))
		return nil
	}); err != nil {
		t.Fatalf("walk docs: %v", err)
	}
	rejected := []string{
		"spec.bootArtifactsHttp",
		"spec.bootArtifactsExternal",
		"providerRefs",
		"HostPool",
		"`Network`",
		"infrastructureRef",
		"clusterInstallMode",
		"bootInterface",
		"serviceAddresses",
		"serviceAddressNames",
		"`spec.installMode`",
		"`spec.ssh.address`",
		"isoFrom",
		"`Host`",
		"`ClusterInfra`",
		"kind: Host",
		"kind: ClusterInfra",
		"kind: Playbook",
		"clusterInfra",
		"hostRef",
	}
	for _, path := range files {
		data := readRepoFile(t, path)
		for _, phrase := range rejected {
			if strings.Contains(data, phrase) {
				t.Fatalf("%s still contains stale schema text %q", path, phrase)
			}
		}
	}
}

func TestCephFSMetadataPoolDestructiveChangeDocumented(t *testing.T) {
	note := regexp.MustCompile(`(?is)metadata pool[^.]{0,220}(data-destroying|ceph fs rm|recreate)`)
	surfaces := []string{
		"specs/state-model.md",
		"docs/advanced/operations.md",
		"docs/advanced/ceph-topologies.md",
		"docs/concepts/storage.md",
	}
	for _, path := range surfaces {
		data := readRepoFile(t, path)
		if !note.MatchString(data) {
			t.Fatalf("%s must document that changing a CephFS metadata pool is a data-destroying --mode rebuild recreate (ceph fs rm)", path)
		}
	}
}

func TestRuntimeBundleUseNewSchemaTerms(t *testing.T) {
	roots := []string{
		"ansible/collections/ansible_collections/bootwright/core/roles",
	}
	rejected := []string{
		"machines[*].provisioner",
		"components.artifacts",
		"networkRef",
		"clusterInstallMode",
		"serviceAddresses",
		"serviceAddressNames",
		"`spec.installMode`",
		"`spec.ssh.address`",
		"isoFrom",
	}
	for _, root := range roots {
		err := filepath.WalkDir(filepath.Join(repoRoot(t), root), func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				if strings.HasPrefix(d.Name(), ".") {
					return filepath.SkipDir
				}
				return nil
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			rel, err := filepath.Rel(repoRoot(t), path)
			if err != nil {
				return err
			}
			body := string(data)
			for _, phrase := range rejected {
				if strings.Contains(body, phrase) {
					t.Fatalf("%s still contains stale schema text %q", rel, phrase)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("scan %s: %v", root, err)
		}
	}
}

func TestDesiredStateExamplesSpaceSpecBlocks(t *testing.T) {
	for _, rel := range desiredStateYAMLPaths(t, "add-ons", "examples", "test/e2e", "internal/state/desired/testdata/good") {
		t.Run(rel, func(t *testing.T) {
			assertSpecBlocksSpaced(t, rel, readRepoFile(t, rel))
		})
	}

	for _, provider := range scaffold.KnownProviders() {
		t.Run("scaffold/"+provider, func(t *testing.T) {
			files, err := scaffold.Workspace("spacing-check", scaffold.Provider(provider))
			if err != nil {
				t.Fatalf("workspace: %v", err)
			}
			for _, file := range files {
				assertSpecBlocksSpaced(t, "scaffold/"+provider+"/"+file.Name, file.Body)
			}
		})
	}
}

func TestDesiredStateExamplesUseCurrentAddressAndInstallFields(t *testing.T) {
	for _, rel := range desiredStateYAMLPaths(t, "add-ons", "examples", "test/e2e", "internal/state/desired/testdata/good") {
		t.Run(rel, func(t *testing.T) {
			assertNoRetiredDesiredStateFields(t, rel, readRepoFile(t, rel))
		})
	}

	for _, provider := range scaffold.KnownProviders() {
		t.Run("scaffold/"+provider, func(t *testing.T) {
			files, err := scaffold.Workspace("schema-check", scaffold.Provider(provider))
			if err != nil {
				t.Fatalf("workspace: %v", err)
			}
			for _, file := range files {
				if ext := filepath.Ext(file.Name); ext != ".yaml" && ext != ".yml" {
					continue
				}
				assertNoRetiredDesiredStateFields(t, "scaffold/"+provider+"/"+file.Name, file.Body)
			}
		})
	}
}

func TestDesiredStateYAMLUsesBlockStyleCollections(t *testing.T) {
	for _, rel := range desiredStateYAMLPaths(t, "add-ons", "examples", "test/e2e", "internal/state/desired/testdata/good") {
		t.Run(rel, func(t *testing.T) {
			assertNoFlowStyleCollections(t, rel, readRepoFile(t, rel))
		})
	}

	for _, provider := range scaffold.KnownProviders() {
		t.Run("scaffold/"+provider, func(t *testing.T) {
			files, err := scaffold.Workspace("block-style-check", scaffold.Provider(provider))
			if err != nil {
				t.Fatalf("workspace: %v", err)
			}
			for _, file := range files {
				if ext := filepath.Ext(file.Name); ext != ".yaml" && ext != ".yml" {
					continue
				}
				assertNoFlowStyleCollections(t, "scaffold/"+provider+"/"+file.Name, file.Body)
			}
		})
	}
}

func TestMakefileGuardsDestructiveCleanTargets(t *testing.T) {
	body := readRepoFile(t, "Makefile")
	for _, want := range []string{
		"CLEAN_PATHS = $(BIN_DIR) $(STATE_DIR) $(EMBED_BUNDLE_ARCHIVE) dist build out rendered tmp",
		"--exclude='./$(EMBED_BUNDLE_ARCHIVE)'",
		"refusing to clean unsafe path",
		"refusing unsafe CASE",
		"refusing to clean E2E_CONTEXT_DIR outside expected case context",
		"$(E2E_CLEAN) \"$(E2E_CONTEXT_DIR)\"",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("Makefile missing cleanup guard fragment %q", want)
		}
	}
}

func TestMakefileRejectsUnsafeE2ECleanupInputs(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{
			name: "traversal case",
			args: []string{"-s", "check-e2e-case", "CASE=../.."},
			want: "refusing unsafe CASE",
		},
		{
			name: "absolute case",
			args: []string{"-s", "check-e2e-case", "CASE=/tmp/example"},
			want: "refusing unsafe CASE",
		},
		{
			name: "slash case",
			args: []string{"-s", "check-e2e-case", "CASE=001-sno-libvirt/extra"},
			want: "refusing unsafe CASE",
		},
		{
			name: "unexpected context dir",
			args: []string{"-s", "clean-e2e-state", "CASE=001-sno-libvirt", "E2E_CONTEXT_DIR=/var/lib/bootwright/contexts/../..", "E2E_CLEAN=:"},
			want: "refusing to clean E2E_CONTEXT_DIR outside expected case context",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cmd := exec.Command("make", tc.args...)
			cmd.Dir = repoRoot(t)
			out, err := cmd.CombinedOutput()
			if err == nil {
				t.Fatalf("make unexpectedly succeeded with output:\n%s", out)
			}
			if !strings.Contains(string(out), tc.want) {
				t.Fatalf("make output missing %q:\n%s", tc.want, out)
			}
		})
	}

	cmd := exec.Command("make", "-s", "clean-e2e-state", "CASE=001-sno-libvirt", "E2E_CLEAN=:")
	cmd.Dir = repoRoot(t)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("safe clean-e2e-state failed: %v\n%s", err, out)
	}
}

func TestContainerfileStampsBuildMetadata(t *testing.T) {
	body := readRepoFile(t, "Containerfile")
	for _, reject := range []string{
		"ARG VERSION=dev",
		"ARG GIT_COMMIT=unknown",
	} {
		if strings.Contains(body, reject) {
			t.Fatalf("Containerfile must not silently default metadata with %q", reject)
		}
	}
	for _, want := range []string{
		"ARG VERSION",
		"ARG GIT_COMMIT",
		`version="${VERSION}"`,
		`git_commit="${GIT_COMMIT}"`,
		"git describe --tags --always --dirty",
		"git rev-parse --short HEAD",
		`make go-build VERSION="${version}" GIT_COMMIT="${git_commit}"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("Containerfile missing build metadata fragment %q", want)
		}
	}

	dockerignore := readRepoFile(t, ".dockerignore")
	if strings.Contains(dockerignore, ".git/") {
		t.Fatal(".dockerignore excludes .git, so direct Containerfile builds cannot self-stamp metadata")
	}
}

func TestGitHubActionsUseCommitSHARefs(t *testing.T) {
	root := filepath.Join(repoRoot(t), ".github", "workflows")
	usesRE := regexp.MustCompile(`\buses:\s*([^#\s]+)`)
	shaRE := regexp.MustCompile(`^[0-9a-f]{40}$`)
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || filepath.Ext(path) != ".yml" {
			return nil
		}
		rel, err := filepath.Rel(repoRoot(t), path)
		if err != nil {
			return err
		}
		for i, line := range strings.Split(readRepoFile(t, rel), "\n") {
			match := usesRE.FindStringSubmatch(line)
			if match == nil {
				continue
			}
			action := match[1]
			if strings.HasPrefix(action, "./") {
				continue
			}
			at := strings.LastIndex(action, "@")
			if at < 0 || !shaRE.MatchString(action[at+1:]) {
				t.Fatalf("%s:%d uses mutable action ref %q; pin to a full commit SHA", rel, i+1, action)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scan workflows: %v", err)
	}
}

func desiredStateYAMLPaths(t *testing.T, roots ...string) []string {
	t.Helper()
	repo := repoRoot(t)
	paths := []string{}
	for _, root := range roots {
		err := filepath.WalkDir(filepath.Join(repo, root), func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				if strings.HasPrefix(d.Name(), ".") {
					return filepath.SkipDir
				}
				return nil
			}
			if ext := filepath.Ext(path); ext != ".yaml" && ext != ".yml" {
				return nil
			}
			rel, err := filepath.Rel(repo, path)
			if err != nil {
				return err
			}
			if isExtensionPayloadManifest(rel) {
				return nil
			}
			paths = append(paths, rel)
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", root, err)
		}
	}
	return paths
}

func isExtensionPayloadManifest(path string) bool {
	for _, part := range strings.Split(filepath.ToSlash(path), "/") {
		switch part {
		case "manifests", "playbooks", "roles", "collections":
			return true
		}
	}
	return false
}

func assertNoFlowStyleCollections(t *testing.T, name, body string) {
	t.Helper()
	for i, line := range strings.Split(body, "\n") {
		if allowedEmptyPresenceUnion(line) {
			continue
		}
		if strings.ContainsAny(line, "{}[]") {
			t.Fatalf("%s:%d uses flow-style collection syntax %q", name, i+1, strings.TrimSpace(line))
		}
	}
}

func allowedEmptyPresenceUnion(line string) bool {
	switch strings.TrimSpace(line) {
	case "secretRef: {}", "storageExportAttachment: {}", "- storageExportAttachment: {}", "boundCluster: {}",
		"tpm2: {}", "tpm: {}":
		return true
	default:
		return false
	}
}

func assertNoRetiredDesiredStateFields(t *testing.T, name, body string) {
	t.Helper()
	dec := yaml.NewDecoder(strings.NewReader(body))
	for {
		var doc map[string]any
		err := dec.Decode(&doc)
		if errors.Is(err, io.EOF) {
			return
		}
		if err != nil {
			t.Fatalf("%s: decode YAML: %v", name, err)
		}
		if len(doc) == 0 {
			continue
		}

		kind, _ := doc["kind"].(string)
		spec, _ := doc["spec"].(map[string]any)
		switch kind {
		case "Host", "ClusterInfra":
			t.Fatalf("%s uses retired kind %s", name, kind)
		case "ContainerCluster":
			rejectMapKey(t, name, kind, "spec.installMode", spec, "installMode")
			if install, ok := spec["install"].(map[string]any); ok {
				rejectMapKey(t, name, kind, "spec.install.isoFrom", install, "isoFrom")
			}
		}
	}
}

func rejectMapKey(t *testing.T, name, kind, field string, m map[string]any, key string) {
	t.Helper()
	if _, ok := m[key]; ok {
		t.Fatalf("%s %s uses retired field %s", name, kind, field)
	}
}

func assertSpecBlocksSpaced(t *testing.T, name, body string) {
	t.Helper()
	inSpec := false
	seenChild := false
	prevBlank := false
	for i, line := range strings.Split(body, "\n") {
		if line == "spec:" {
			inSpec = true
			seenChild = false
			prevBlank = false
			continue
		}
		if inSpec && strings.TrimSpace(line) == "---" {
			inSpec = false
			seenChild = false
			prevBlank = false
			continue
		}
		if inSpec && line != "" && !strings.HasPrefix(line, "  ") && !strings.HasPrefix(line, "#") {
			inSpec = false
			seenChild = false
		}
		if inSpec && isDirectSpecChild(line) {
			if seenChild && !prevBlank {
				t.Errorf("%s:%d missing blank line before %q", name, i+1, line)
			}
			seenChild = true
		}
		prevBlank = line == ""
	}
}

func isDirectSpecChild(line string) bool {
	if !strings.HasPrefix(line, "  ") || len(line) < 3 {
		return false
	}
	rest := line[2:]
	return rest[0] != ' ' && rest[0] != '#' && strings.Contains(rest, ":")
}

func repoFileExists(t *testing.T, rel string) bool {
	t.Helper()
	_, err := os.Stat(filepath.Join(repoRoot(t), rel))
	return err == nil
}

func readRepoFile(t *testing.T, rel string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repoRoot(t), rel))
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	return string(data)
}

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("get cwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not locate repository root")
		}
		dir = parent
	}
}
