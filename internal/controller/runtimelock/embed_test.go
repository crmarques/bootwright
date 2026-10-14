package runtimelock

import (
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

const (
	checkRequirements = "../../../scripts/tools/ansible-requirements.txt"
	collectionRuntime = "../../../ansible/collections/ansible_collections/bootwright/core/meta/runtime.yml"
)

func TestTheEmbeddedLockIsTheOneTheBuildParses(t *testing.T) {
	lock, err := Embedded()
	if err != nil {
		t.Fatal(err)
	}
	onDisk, err := os.ReadFile("lock.json")
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := Parse(onDisk)
	if err != nil {
		t.Fatal(err)
	}
	if lock.Digest() != parsed.Digest() {
		t.Errorf("the embedded lock digest %s differs from lock.json's %s", lock.Digest(), parsed.Digest())
	}
}

// The lock, the check suite's pin and the collection's requires_ansible name
// one ansible-core, so the version the controller runs is the version the
// collection is tested and linted on.
func TestTheLockCarriesOneAnsibleCoreEverywhere(t *testing.T) {
	lock, err := Embedded()
	if err != nil {
		t.Fatal(err)
	}
	locked := lock.AnsibleCore()

	requirements, err := os.ReadFile(checkRequirements)
	if err != nil {
		t.Fatal(err)
	}
	var pins []string
	for _, line := range strings.Split(string(requirements), "\n") {
		if pin, found := strings.CutPrefix(line, "ansible-core=="); found {
			version, _, _ := strings.Cut(pin, " ")
			pins = append(pins, version)
		}
	}
	if len(pins) != 1 || pins[0] != locked {
		t.Errorf("%s pins ansible-core %v, want exactly the locked %s", checkRequirements, pins, locked)
	}

	runtime, err := os.ReadFile(collectionRuntime)
	if err != nil {
		t.Fatal(err)
	}
	match := regexp.MustCompile(`^requires_ansible: '>=([0-9]+)\.([0-9]+)\.0,<([0-9]+)\.([0-9]+)\.0'\n$`).FindStringSubmatch(string(runtime))
	if match == nil {
		t.Fatalf("%s is not a one-minor requires_ansible range: %q", collectionRuntime, runtime)
	}
	major, minor, _ := strings.Cut(locked, ".")
	minor, _, _ = strings.Cut(minor, ".")
	next, err := strconv.Atoi(minor)
	if err != nil {
		t.Fatalf("the locked ansible-core %q is not a release", locked)
	}
	if match[1] != major || match[2] != minor || match[3] != major || match[4] != strconv.Itoa(next+1) {
		t.Errorf("the collection requires %s, which does not admit the locked ansible-core %s alone", runtime, locked)
	}
}
