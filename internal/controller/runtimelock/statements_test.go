package runtimelock

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

// Every other statement of the locked ansible-core minor and ABIs, read from
// this package's directory, so qualifying another minor moves all of them in
// one commit or fails here.
const (
	lintConfiguration = "../../../ansible/.ansible-lint"
	sanityIgnores     = "../../../ansible/collections/ansible_collections/bootwright/core/tests/sanity"
	developmentGuide  = "../../../docs/development.md"
	floorLock         = "../../../scripts/tools/ansible-check-floor-interpreter.json"
	floorTest         = "../../../ansible/collections/ansible_collections/bootwright/core/tests/unit/test_remote_python_floor.py"
)

func lockedMinor(t *testing.T) string {
	t.Helper()
	lock, err := Embedded()
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(lock.AnsibleCore(), ".")
	if len(parts) != 3 {
		t.Fatalf("the locked ansible-core %q is not a MAJOR.MINOR.PATCH release", lock.AnsibleCore())
	}
	return parts[0] + "." + parts[1]
}

func TestTheLintAndSanityConfigurationsNameTheLockedMinor(t *testing.T) {
	minor := lockedMinor(t)
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

func TestTheDevelopmentGuideStatesTheLockedMinorAndABIs(t *testing.T) {
	minor := lockedMinor(t)
	lock, err := Embedded()
	if err != nil {
		t.Fatal(err)
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
	slices.Sort(pythons)
	if want := slices.Sorted(slices.Values(lock.PythonABIs())); !slices.Equal(pythons, want) {
		t.Errorf("the development guide states controller CPython %q, want the locked %q", pythons, want)
	}
}

// The collection's remote Python floor is held, by a test the units suite runs
// under the check lock's ansible-core, to that ansible-core's oldest target
// Python; the floor interpreter the sanity and units suites run is held here
// to the same floor, so it moves with the locked ansible-core or fails.
func TestTheFloorLockPinsTheCollectionsRemotePythonFloor(t *testing.T) {
	data, err := os.ReadFile(floorLock)
	if err != nil {
		t.Fatal(err)
	}
	var lock struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(data, &lock); err != nil {
		t.Fatal(err)
	}
	source, err := os.ReadFile(floorTest)
	if err != nil {
		t.Fatal(err)
	}
	floors := regexp.MustCompile(`(?m)^REMOTE_PYTHON_FLOOR = \(([0-9]+), ([0-9]+)\)$`).FindAllSubmatch(source, -1)
	if len(floors) != 1 {
		t.Fatalf("%s states REMOTE_PYTHON_FLOOR %d times, want exactly once", floorTest, len(floors))
	}
	floor := string(floors[0][1]) + "." + string(floors[0][2])
	if !regexp.MustCompile(`^` + regexp.QuoteMeta(floor) + `\.[0-9]+$`).MatchString(lock.Version) {
		t.Errorf("the floor interpreter lock pins Python %q, not a %s release", lock.Version, floor)
	}
}
