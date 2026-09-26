package cli

import (
	"bytes"
	"encoding/json"
	"strings"
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

// routeOutcome is what classification decides about the invoking
// environment's route for one invocation shape.
type routeOutcome int

const (
	// readsRoute: admitted, and it acquires or previews acquisition before a
	// context exists.
	readsRoute routeOutcome = iota
	// ignoresRoute: admitted when its command is available, and it acquires
	// nothing before a context exists.
	ignoresRoute
	// notAdmitted: refused or informational, so it classifies as nothing.
	notAdmitted
)

// Only an invocation whose parsed flags acquire, or preview acquisition, before
// a context exists may read the invoking environment for a route. A
// context-backed shape takes its Machine's proxy choice, so reading one there
// would give one host two routes, and a local shape would refuse over, and ask
// sudo to forward, a proxy it never uses. The table holds an admitted shape of
// every catalog command and every flag that moves a command between those
// shapes.
func TestOnlyContextFreeAcquisitionConsumesTheInvokingEnvironment(t *testing.T) {
	const (
		source = "https://images.example/image.iso"
		digest = "a1b2c3d4a1b2c3d4a1b2c3d4a1b2c3d4a1b2c3d4a1b2c3d4a1b2c3d4a1b2c3d4"
	)
	tests := []struct {
		path  string
		flags []string
		want  routeOutcome
	}{
		// Setup selects no context: it acquires, and its dry run previews the
		// route setup would take.
		{"setup", nil, readsRoute},
		{"setup", []string{"--dry-run"}, readsRoute},
		{"setup", []string{"--yes"}, readsRoute},
		{"setup", []string{"--purge-old-bundles"}, readsRoute},
		{"setup", []string{"--context", "lab"}, readsRoute},
		// Preflight reports the host's route only when no context is named; an
		// explicitly empty final --context is omission.
		{"preflight controller", nil, readsRoute},
		{"preflight controller", []string{"--context", ""}, readsRoute},
		{"preflight controller", []string{"--context", "lab", "--context", ""}, readsRoute},
		{"preflight controller", []string{"--context", "lab"}, ignoresRoute},
		{"preflight controller", []string{"--context", "", "--context", "lab"}, ignoresRoute},
		// The media store is host-wide, so only the source decides: a URL is
		// downloaded, a local file is copied.
		{"media add", []string{"--name", "image.iso", "--from-url", source, "--sha256", digest}, readsRoute},
		{"media add", []string{"--name", "image.iso", "--from-url", source, "--sha256", digest, "--context", "lab"}, readsRoute},
		{"media add", []string{"--name", "image.iso", "--from-file", "/images/image.iso"}, ignoresRoute},
		{"media add", []string{"--name", "image.iso", "--from-file", "/images/image.iso", "--sha256", digest}, ignoresRoute},
		{"media list", nil, ignoresRoute},
		{"media list", []string{"--checksums"}, ignoresRoute},
		{"media delete", []string{"--name", "image.iso"}, ignoresRoute},
		// Every other command acquires nothing before a context exists.
		{"context init", []string{"--name", "lab"}, ignoresRoute},
		{"context update", []string{"--name", "lab", "--input-dir", "inputs"}, ignoresRoute},
		{"context use", []string{"--name", "lab"}, ignoresRoute},
		{"context list", nil, ignoresRoute},
		{"context current", nil, ignoresRoute},
		{"context delete", []string{"--name", "lab", "--purge"}, ignoresRoute},
		{"add-ons list", nil, ignoresRoute},
		{"add-ons add", []string{"--name", "example"}, ignoresRoute},
		{"add-ons delete", []string{"--name", "example"}, ignoresRoute},
		{"secret set", []string{"--name", "demo", "--value-file", "secret.bin"}, ignoresRoute},
		{"secret generate", nil, ignoresRoute},
		{"secret check", nil, ignoresRoute},
		{"secret list", nil, ignoresRoute},
		{"secret show", []string{"--name", "demo", "--part", "value"}, ignoresRoute},
		{"secret delete", []string{"--name", "demo"}, ignoresRoute},
		{"secret encryption init", nil, ignoresRoute},
		{"secret encryption status", nil, ignoresRoute},
		{"secret encryption rotate", nil, ignoresRoute},
		{"validate", nil, ignoresRoute},
		{"preflight infra", nil, ignoresRoute},
		{"preflight clusters", nil, ignoresRoute},
		{"preflight container-cluster", nil, ignoresRoute},
		{"preflight storage-cluster", nil, ignoresRoute},
		{"preflight add-ons", nil, ignoresRoute},
		{"preflight all", nil, ignoresRoute},
		{"plan", nil, ignoresRoute},
		{"plan", []string{"--stage", "controller"}, ignoresRoute},
		{"status", nil, ignoresRoute},
		{"render", []string{"--input-dir", "inputs", "--output-dir", "artifacts"}, ignoresRoute},
		{"render effective", nil, ignoresRoute},
		{"render installer", nil, ignoresRoute},
		{"render storage", nil, ignoresRoute},
		{"apply", nil, ignoresRoute},
		{"apply", []string{"--stage", "controller"}, ignoresRoute},
		{"destroy", nil, ignoresRoute},
		{"machine list", nil, ignoresRoute},
		{"machine rsh", []string{"--name", "demo"}, ignoresRoute},
		{"machine exec", []string{"--name", "demo", "--", "uname"}, ignoresRoute},
		{"machine start", []string{"--name", "demo"}, ignoresRoute},
		{"machine stop", []string{"--name", "demo"}, ignoresRoute},
		{"machine restart", []string{"--name", "demo"}, ignoresRoute},
		{"machine trust", nil, ignoresRoute},
		{"cluster list", nil, ignoresRoute},
		{"cluster info", nil, ignoresRoute},
		{"cluster rsh", []string{"--name", "demo"}, ignoresRoute},
		{"cluster exec", []string{"--name", "demo", "--", "uname"}, ignoresRoute},
		{"cluster oc", []string{"--name", "demo", "--", "get", "nodes"}, ignoresRoute},
		{"cluster kubectl", []string{"--name", "demo", "--", "get", "nodes"}, ignoresRoute},
		{"cluster kubeconfig", []string{"--name", "demo"}, ignoresRoute},
		{"version", nil, notAdmitted},
		{"help", nil, notAdmitted},
		{"completion bash", nil, notAdmitted},
		{"completion zsh", nil, notAdmitted},
		{"completion fish", nil, notAdmitted},
		{"completion powershell", nil, notAdmitted},
		// An invocation that is not admitted reads no route.
		{"setup", []string{"--help"}, notAdmitted},
		{"preflight controller", []string{"--context", "Lab"}, notAdmitted},
		{"media add", []string{"--name", "image.iso", "--from-url", source}, notAdmitted},
		{"media add", []string{"--name", "image.iso", "--from-url", source, "--from-file", "/images/image.iso", "--sha256", digest}, notAdmitted},
	}
	covered := map[string]bool{}
	for _, test := range tests {
		covered[test.path] = true
		args := append(strings.Fields(test.path), test.flags...)
		classified := ClassifyInvocation(args)
		switch test.want {
		case readsRoute:
			if !classified.AmbientRoute || classified.Command != test.path {
				t.Errorf("%v: %+v; want the admitted command to read the route", args, classified)
			}
		case ignoresRoute:
			// An available command must still be admitted, so the result is
			// the flags' decision rather than a refused invocation.
			if classified.AmbientRoute || (privilegedOperation(test.path) && classified.Command != test.path) {
				t.Errorf("%v: %+v; want an admitted invocation that reads no route", args, classified)
			}
		case notAdmitted:
			if classified != (InvocationClass{}) {
				t.Errorf("%v: %+v; want no classification", args, classified)
			}
		}
	}
	for _, spec := range commandCatalog() {
		if !covered[spec.path] {
			t.Errorf("%s: no route row; add its shapes and each flag that changes what it acquires", spec.path)
		}
	}
}
