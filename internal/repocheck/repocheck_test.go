package repocheck

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/scaffold"
	"go.yaml.in/yaml/v3"
)

// TestREADMEDescribesSixKindModel verifies the README still names the
// canonical v1alpha1 six-kind model.
func TestREADMEDescribesSixKindModel(t *testing.T) {
	readme := readRepoFile(t, "README.md")
	required := []string{
		"six kinds",
		"`Host`",
		"`NetworkConfig`",
		"`InfraProvider`",
		"`ClusterInfra`",
		"`ContainerCluster`",
		"`Environment`",
	}
	for _, phrase := range required {
		if !strings.Contains(readme, phrase) {
			t.Fatalf("README.md missing %q", phrase)
		}
	}
	rejected := []string{
		"five kinds",
		"`HostPool`",
		"providerRefs",
	}
	for _, phrase := range rejected {
		if strings.Contains(readme, phrase) {
			t.Fatalf("README.md still contains stale model text %q", phrase)
		}
	}
}

// TestCurrentDefinitionDocsUseNewSchemaTerms scans the
// schema-bearing docs for terms from the abandoned shape (old
// bootArtifacts placement, single-substrate Closure, providerRefs, and
// pre-installer-aligned refs).
func TestCurrentDefinitionDocsUseNewSchemaTerms(t *testing.T) {
	files := []string{
		"README.md",
		"docs/index.md",
		"docs/concepts.md",
		"docs/advanced/architecture.md",
		"docs/advanced/networking.md",
		"docs/advanced/providers.md",
		"docs/advanced/proxy-and-disconnected.md",
		"specs/architecture.md",
		"specs/state-model.md",
		"ansible/VARS_CONTRACT.md",
	}
	if repoFileExists(t, "internal/embedded/bundle/VARS_CONTRACT.md") {
		files = append(files, "internal/embedded/bundle/VARS_CONTRACT.md")
	}
	rejected := []string{
		"spec.bootArtifactsHttp",
		"spec.bootArtifactsExternal",
		"providerRefs",
		"HostPool",
		"`Network`",
		"networkRef",
		"infrastructureRef",
		"clusterInstallMode",
		"Environment.spec.bastion",
		"bootInterface",
		"serviceAddresses",
		"serviceAddressNames",
		"`spec.installMode`",
		"`spec.ssh.address`",
		"isoFrom",
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

// TestRuntimeBundleUseNewSchemaTerms scans shipped Ansible role text for
// stale schema fragments that otherwise surface only as runtime diagnostics.
func TestRuntimeBundleUseNewSchemaTerms(t *testing.T) {
	roots := []string{
		"ansible/roles",
	}
	if repoFileExists(t, "internal/embedded/bundle/ansible.cfg") {
		roots = append(roots, "internal/embedded/bundle/roles")
	}
	rejected := []string{
		"machines[*].provisioner",
		"components.artifacts",
		"Environment.spec.bastion",
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
	for _, rel := range desiredStateYAMLPaths(t, "examples", "test/e2e", "internal/desiredstate/testdata/good") {
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
	for _, rel := range desiredStateYAMLPaths(t, "examples", "test/e2e", "internal/desiredstate/testdata/good") {
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
	for _, rel := range desiredStateYAMLPaths(t, "examples", "test/e2e", "internal/desiredstate/testdata/good") {
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
				return nil
			}
			if ext := filepath.Ext(path); ext != ".yaml" && ext != ".yml" {
				return nil
			}
			rel, err := filepath.Rel(repo, path)
			if err != nil {
				return err
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

func assertNoFlowStyleCollections(t *testing.T, name, body string) {
	t.Helper()
	for i, line := range strings.Split(body, "\n") {
		if strings.ContainsAny(line, "{}[]") {
			t.Fatalf("%s:%d uses flow-style collection syntax %q", name, i+1, strings.TrimSpace(line))
		}
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
		case "Host":
			rejectMapKey(t, name, kind, "spec.serviceAddresses", spec, "serviceAddresses")
			rejectMapKey(t, name, kind, "spec.serviceAddressNames", spec, "serviceAddressNames")
			if ssh, ok := spec["ssh"].(map[string]any); ok {
				rejectMapKey(t, name, kind, "spec.ssh.address", ssh, "address")
			}
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
