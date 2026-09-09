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
		{[]string{"apply"}, false},
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
