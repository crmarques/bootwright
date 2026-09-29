package bundlelocal

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"

	"github.com/crmarques/bootwright/ansible"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
)

// Every statement of the qualified ansible-core minor, read from this
// package's directory, so qualifying another minor moves all of them in one
// commit or fails here.
const (
	checkLock         = "../../../scripts/tools/ansible-requirements.txt"
	lintConfiguration = "../../../ansible/.ansible-lint"
	sanityIgnores     = "../../../ansible/collections/ansible_collections/bootwright/core/tests/sanity"
	developmentGuide  = "../../../docs/development.md"
)

// pinnedAnsibleMinor reads the minor of the one ansible-core pin in a lock.
// Only the ansible-core line counts: other packages in the same lock carry
// versions that begin with the same digits.
func pinnedAnsibleMinor(t *testing.T, name string, data []byte) string {
	t.Helper()
	var minors []string
	for _, line := range strings.Split(string(data), "\n") {
		pin, found := strings.CutPrefix(line, "ansible-core==")
		if !found {
			continue
		}
		version, _, _ := strings.Cut(pin, " ")
		parts := strings.Split(version, ".")
		if len(parts) != 3 {
			t.Fatalf("%s pins ansible-core %q, not a MAJOR.MINOR.PATCH release", name, version)
		}
		minors = append(minors, parts[0]+"."+parts[1])
	}
	if len(minors) != 1 {
		t.Fatalf("%s has %d ansible-core pins, want exactly one", name, len(minors))
	}
	return minors[0]
}

func TestQualifiedAnsibleMinorAgreesEverywhere(t *testing.T) {
	minor := prerequisites.QualifiedAnsibleMinor
	lock, err := os.ReadFile(checkLock)
	if err != nil {
		t.Fatal(err)
	}
	assets := ansible.Assets()
	for name, data := range map[string][]byte{checkLock: lock, "controller/requirements.txt": assets["controller/requirements.txt"]} {
		if pinned := pinnedAnsibleMinor(t, name, data); pinned != minor {
			t.Errorf("%s pins ansible-core %s, not the qualified minor %s", name, pinned, minor)
		}
	}

	major, number, _ := strings.Cut(minor, ".")
	next, err := strconv.Atoi(number)
	if err != nil {
		t.Fatalf("qualified minor %q is not MAJOR.MINOR", minor)
	}
	runtime := "requires_ansible: '>=" + minor + ".0,<" + major + "." + strconv.Itoa(next+1) + ".0'\n"
	if got := string(assets["collections/ansible_collections/bootwright/core/meta/runtime.yml"]); got != runtime {
		t.Errorf("the collection declares %q, want %q", got, runtime)
	}

	data, err := os.ReadFile(lintConfiguration)
	if err != nil {
		t.Fatal(err)
	}
	var lint struct {
		Supported []string `yaml:"supported_ansible_also"`
	}
	if err := yaml.Unmarshal(data, &lint); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(lint.Supported, []string{minor}) {
		t.Errorf("ansible-lint lists %q as supported, want exactly %s", lint.Supported, minor)
	}

	ignores, err := filepath.Glob(filepath.Join(sanityIgnores, "ignore-*.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if len(ignores) != 1 || filepath.Base(ignores[0]) != "ignore-"+minor+".txt" {
		t.Errorf("sanity ignore files %q, want exactly ignore-%s.txt", ignores, minor)
	}

	record, _, err := compiledCatalog()
	if err != nil {
		t.Fatal(err)
	}
	if err := prerequisites.ValidateQualifiedAnsibleVersion(record.AnsibleVersion); err != nil {
		t.Errorf("the compiled catalog's ansible-core %s is outside the qualified minor", record.AnsibleVersion)
	}
	if !prerequisites.QualifiedControllerPython(record.PythonVersion) {
		t.Errorf("the compiled catalog's Python %s is not a qualified controller Python", record.PythonVersion)
	}

	guide, err := os.ReadFile(developmentGuide)
	if err != nil {
		t.Fatal(err)
	}
	stated := guideStatements(guide, "`?ansible-core`?\\s+([0-9]+\\.[0-9]+)(?:[^0-9]|$)", `ansible/blob/stable-([0-9]+\.[0-9]+)/`)
	if len(stated) == 0 || slices.ContainsFunc(stated, func(value string) bool { return value != minor }) {
		t.Errorf("the development guide states ansible-core %q, want only %s", stated, minor)
	}
	pythons := guideStatements(guide, `controller\s+CPython\s+([0-9]+\.[0-9]+(?:(?:,|\s+or|\s+and)\s+[0-9]+\.[0-9]+)*)`)
	if want := prerequisites.QualifiedControllerPythons(); !slices.Equal(pythons, want) {
		t.Errorf("the development guide states controller CPython %q, want %q", pythons, want)
	}
}

// guideStatements collects every version each pattern's first group states,
// in document order, so one statement moved alone is caught.
func guideStatements(guide []byte, patterns ...string) []string {
	version := regexp.MustCompile(`[0-9]+\.[0-9]+`)
	var stated []string
	for _, pattern := range patterns {
		for _, match := range regexp.MustCompile(pattern).FindAllSubmatch(guide, -1) {
			for _, found := range version.FindAll(match[1], -1) {
				stated = append(stated, string(found))
			}
		}
	}
	return stated
}
