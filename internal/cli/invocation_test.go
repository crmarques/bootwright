package cli

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestPrivilegeClassificationHasNoInformationOrInvalidEffects(t *testing.T) {
	for _, test := range []struct {
		args []string
		root bool
	}{
		{[]string{"help"}, false},
		{[]string{"version"}, false},
		{[]string{"completion", "bash"}, false},
		{[]string{"__bootwright_complete", "context", ""}, false},
		{[]string{"context", "init", "--help"}, false},
		{[]string{"context", "init"}, false},
		{[]string{"context", "use", "--name", "bad/name"}, false},
		{[]string{"context", "current"}, true},
		{[]string{"context", "list"}, true},
		{[]string{"context", "use", "--name", "test"}, true},
		{[]string{"validate", "--file", "fixtures"}, false},
		{[]string{"validate"}, true},
		{[]string{"render", "effective"}, true},
		{[]string{"apply"}, true},
		{[]string{"apply", "--help"}, false},
		{[]string{"plan"}, true},
		{[]string{"controller", "prerequisites"}, false},
		{[]string{"secret", "encryption", "types"}, false},
	} {
		if got := ClassifyInvocation(test.args); got.RequiresRoot != test.root {
			t.Errorf("%v: got %v", test.args, got)
		}
	}
}

func TestPrivilegeFailurePreservesJSONEnvelope(t *testing.T) {
	classification := ClassifyInvocation([]string{"render", "effective", "--output", "json"})
	if !classification.RequiresRoot || !classification.JSON {
		t.Fatal(classification)
	}
	var out, errOut bytes.Buffer
	if code := classification.Failure(&out, &errOut, "runtime.privilege", "sudo invocation failed", 1); code != 1 {
		t.Fatal(code)
	}
	var result commandEnvelope
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Command != "render effective" || result.ExitCode != 1 || result.OK || len(result.Diagnostics) != 1 || result.Diagnostics[0].Code != "runtime.privilege" || errOut.Len() != 0 {
		t.Fatalf("envelope %+v stderr %q", result, errOut.String())
	}
}

// Only the commands that acquire before a context exists may read the invoking
// environment for a route. Everything context-backed takes its Machine's
// proxy choice, so admitting one here would give one host two routes.
func TestOnlyContextFreeAcquisitionConsumesTheInvokingEnvironment(t *testing.T) {
	for _, test := range []struct {
		args    []string
		ambient bool
	}{
		{[]string{"setup"}, true},
		{[]string{"setup", "--dry-run"}, true},
		{[]string{"setup", "--yes"}, true},
		{[]string{"preflight", "controller"}, true},
		{[]string{"preflight", "controller", "--context", "lab"}, true},
		{[]string{"media", "add", "--name", "image.iso", "--from-url", "https://images.example/image.iso", "--sha256", "a1b2c3d4a1b2c3d4a1b2c3d4a1b2c3d4a1b2c3d4a1b2c3d4a1b2c3d4a1b2c3d4"}, true},
		{[]string{"media", "add", "--name", "image.iso", "--from-file", "/images/image.iso"}, true},
		{[]string{"media", "add", "--name", "image.iso", "--from-url", "https://images.example/image.iso"}, false},
		{[]string{"media", "list"}, false},
		{[]string{"media", "delete", "--name", "image.iso"}, false},
		{[]string{"apply"}, false},
		{[]string{"plan"}, false},
		{[]string{"destroy"}, false},
		{[]string{"validate"}, false},
		{[]string{"context", "list"}, false},
		{[]string{"add-ons", "add", "--name", "example"}, false},
		{[]string{"setup", "--help"}, false},
		{[]string{"help"}, false},
	} {
		if classified := ClassifyInvocation(test.args); classified.AmbientRoute != test.ambient {
			t.Fatalf("%v ambient route = %v", test.args, classified.AmbientRoute)
		}
	}
}
