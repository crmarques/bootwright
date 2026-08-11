package repocheck

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestPythonTestDiscoversEveryPluginUnitSuite(t *testing.T) {
	makefile := readRepoFile(t, "Makefile")
	match := regexp.MustCompile(`(?m)^BOOTWRIGHT_PLUGIN_TEST_SUITES\s*=\s*(.+)$`).FindStringSubmatch(makefile)
	if len(match) != 2 {
		t.Fatal("Makefile must declare BOOTWRIGHT_PLUGIN_TEST_SUITES")
	}
	declared := map[string]bool{}
	for _, suite := range strings.Fields(match[1]) {
		declared[suite] = true
	}
	root := filepath.Join(repoRoot(t), "ansible/collections/ansible_collections/bootwright/core/tests/unit/plugins")
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("read plugin test root: %v", err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		tests, err := filepath.Glob(filepath.Join(root, entry.Name(), "test_*.py"))
		if err != nil {
			t.Fatalf("discover %s plugin tests: %v", entry.Name(), err)
		}
		if len(tests) > 0 && !declared[entry.Name()] {
			t.Errorf("Python plugin test suite %q has %d test files but make python-test does not discover it", entry.Name(), len(tests))
		}
	}
	for _, want := range []string{"for suite in $(BOOTWRIGHT_PLUGIN_TEST_SUITES)", "$(PYTHON) -m unittest discover -v"} {
		if !strings.Contains(makefile, want) {
			t.Errorf("python-test does not execute the declared plugin suites through %q", want)
		}
	}
}
