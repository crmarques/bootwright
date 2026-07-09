package cli

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/addons/nativecatalog"
)

func TestAddonsCLIListAddDelete(t *testing.T) {
	setTestHomeAndRoot(t)

	stdout, stderr, code := runCLI(t, "add-ons", "list", "--output", "json")
	if code != 0 {
		t.Fatalf("add-ons list exited %d, stderr=%q", code, stderr)
	}
	var report struct {
		AddOns []struct {
			Name           string   `json:"name"`
			DefaultVersion string   `json:"defaultVersion"`
			Versions       []string `json:"versions"`
			Registered     string   `json:"registered"`
		} `json:"addOns"`
	}
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("decode list JSON: %v (%q)", err, stdout)
	}
	names := map[string]string{}
	for _, row := range report.AddOns {
		names[row.Name] = row.Registered
	}
	if _, ok := names["openshift-data-foundation"]; !ok {
		t.Fatalf("catalog list missing openshift-data-foundation: %q", stdout)
	}
	if _, ok := names["fusion-data-foundation"]; !ok {
		t.Fatalf("catalog list missing fusion-data-foundation: %q", stdout)
	}
	if names["openshift-data-foundation"] != "" {
		t.Fatalf("nothing should be registered yet: %q", stdout)
	}

	stdout, stderr, code = runCLI(t, "add-ons", "add", "--name", "openshift-data-foundation:4.21")
	if code != 0 {
		t.Fatalf("add-ons add exited %d, stdout=%q stderr=%q", code, stdout, stderr)
	}
	if !strings.Contains(stdout, "registered") {
		t.Fatalf("add-ons add output = %q", stdout)
	}
	if marker, found, err := nativecatalog.ReadMarker(nativecatalog.InstalledDir("openshift-data-foundation")); err != nil || !found || marker.Version != "4.21" {
		t.Fatalf("registered marker = %+v found=%v err=%v", marker, found, err)
	}

	// The colon shorthand and --version are mutually exclusive.
	_, stderr, code = runCLI(t, "add-ons", "add", "--name", "openshift-data-foundation:4.21", "--version", "4.21")
	if code != 2 || !strings.Contains(stderr, "conflicts") {
		t.Fatalf("conflicting version forms: code=%d stderr=%q", code, stderr)
	}

	// Re-registering requires confirmation; an interactive y proceeds.
	stdout, stderr, code = runCLIWithInput(t, "y\n", "add-ons", "add", "--name", "openshift-data-foundation")
	if code != 0 {
		t.Fatalf("re-add exited %d, stdout=%q stderr=%q", code, stdout, stderr)
	}

	stdout, _, code = runCLI(t, "add-ons", "list", "--output", "json")
	if code != 0 || !strings.Contains(stdout, `"registered": "4.21"`) {
		t.Fatalf("list after add: code=%d %q", code, stdout)
	}

	// delete accepts the same colon shorthand add teaches; a version that does
	// not match the registered one is an error, a matching one deletes.
	_, stderr, code = runCLI(t, "add-ons", "delete", "--name", "openshift-data-foundation:9.99", "--yes")
	if code != 1 || !strings.Contains(stderr, "registered at version 4.21, not 9.99") {
		t.Fatalf("delete version mismatch: code=%d stderr=%q", code, stderr)
	}
	_, stderr, code = runCLI(t, "add-ons", "delete", "--name", "openshift-data-foundation:4.21", "--yes")
	if code != 0 {
		t.Fatalf("add-ons delete exited %d, stderr=%q", code, stderr)
	}
	_, stderr, code = runCLI(t, "add-ons", "delete", "--name", "openshift-data-foundation", "--yes")
	if code != 1 || !strings.Contains(stderr, "not registered") {
		t.Fatalf("delete absent: code=%d stderr=%q", code, stderr)
	}
}

func TestAddonsCLIAddUnknownNameFails(t *testing.T) {
	setTestHomeAndRoot(t)
	_, stderr, code := runCLI(t, "add-ons", "add", "--name", "no-such-add-on", "--yes")
	if code != 1 || !strings.Contains(stderr, "not found in the catalog") {
		t.Fatalf("unknown add-on: code=%d stderr=%q", code, stderr)
	}
}
