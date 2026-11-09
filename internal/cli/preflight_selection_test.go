package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

// The add-ons stage has no checks yet, so selecting it is refused as an
// unavailable selection before privilege, a context or any service is reached,
// whatever else the stage list names and in either output mode.
func TestPreflightStageAddOnsIsRefusedAsNotImplementedBeforeAnyEffect(t *testing.T) {
	for _, stage := range []string{"add-ons", "clusters,add-ons", "add-ons,controller"} {
		args := []string{"preflight", "--stage", stage}
		if class := ClassifyInvocation(args); class.RequiresRoot || class.AmbientRoute {
			t.Errorf("%v requested privilege or the ambient route: %+v", args, class)
		}
		code, out, errOut, record := runRecorded(args)
		want := "  error  cli.not-implemented\n    bootwright preflight --stage add-ons is not implemented\n    Next: run bootwright preflight without that selection\n"
		if code != 1 || out != "" || errOut != want || record.calls != 0 {
			t.Errorf("%v: exit=%d stdout=%q stderr=%q calls=%d", args, code, out, errOut, record.calls)
		}
		code, out, errOut, record = runRecorded(append(args, "--output", "json"))
		if code != 1 || errOut != "" || record.calls != 0 || !strings.Contains(out, `"command":"preflight"`) || !strings.Contains(out, `"code":"cli.not-implemented"`) || !strings.Contains(out, `bootwright preflight --stage add-ons is not implemented`) {
			t.Errorf("%v json: exit=%d stdout=%q stderr=%q calls=%d", args, code, out, errOut, record.calls)
		}
	}
}

// Cluster names select only the clusters stage, so naming them while an
// explicit stage list leaves that stage out is a usage error that reaches no
// service, and the default stage list keeps accepting them.
func TestPreflightClustersRequireTheClustersStage(t *testing.T) {
	for _, args := range [][]string{
		{"preflight", "--stage", "controller", "--clusters", "one"},
		{"preflight", "--stage", "substrates,machines", "--clusters=one,two"},
	} {
		code, out, errOut, record := runRecorded(args)
		if code != 2 || out != "" || record.calls != 0 || !strings.HasPrefix(errOut, "  error  cli.usage\n    --clusters requires the clusters stage\n") {
			t.Errorf("%v: exit=%d stdout=%q stderr=%q calls=%d", args, code, out, errOut, record.calls)
		}
	}
	for _, args := range [][]string{{"preflight", "--clusters", "one"}, {"preflight", "--stage", "clusters", "--clusters", "one"}} {
		if code, out, errOut, record := runRecorded(args); code == 2 || record.calls != 1 {
			t.Errorf("%v: exit=%d stdout=%q stderr=%q calls=%d", args, code, out, errOut, record.calls)
		}
	}
}

// render effective writes only when it is given an output directory, and the
// flags that make sense only beside or without one are refused before any
// service runs.
func TestRenderEffectiveFlagRelationshipsAreUsageErrors(t *testing.T) {
	for _, test := range []struct {
		args    []string
		message string
	}{
		{[]string{"render", "effective", "--origins", "--output-dir", "out", "--sensitive"}, "--origins conflicts with --output-dir"},
		{[]string{"render", "effective", "--origins", "--input-dir", "in", "--output-dir", "out"}, "--origins conflicts with --output-dir"},
		{[]string{"render", "effective", "--sensitive"}, "--sensitive requires --output-dir"},
		{[]string{"render", "effective", "--input-dir", "in"}, "--input-dir requires --output-dir"},
		{[]string{"render", "effective", "--output-dir", "out"}, "context-backed --output-dir requires --sensitive"},
		{[]string{"render", "effective", "--input-dir", "in", "--output-dir", "out", "--sensitive"}, "--input-dir conflicts with --sensitive"},
		{[]string{"render", "effective", "--input-dir", "in", "--output-dir", "out", "--context", "lab"}, "--input-dir conflicts with an explicit context"},
	} {
		code, out, errOut, record := runRecorded(test.args)
		if code != 2 || out != "" || record.calls != 0 || !strings.HasPrefix(errOut, "  error  cli.usage\n    "+test.message+"\n") {
			t.Errorf("%v: exit=%d stdout=%q stderr=%q calls=%d", test.args, code, out, errOut, record.calls)
		}
	}
}

// A group's help offers only the inherited flags and hints its commands
// accept: --context and the SSH hint disappear from a group whose commands
// are all context-free, and stay on a group with a context-backed command.
func TestGroupHelpOffersOnlyTheFlagsItsCommandsAccept(t *testing.T) {
	for _, test := range []struct {
		group            string
		context, sshHint bool
	}{
		{"controller", false, false},
		{"media", false, false},
		{"add-ons", false, false},
		{"completion", false, false},
		{"machine", true, true},
		{"cluster", true, true},
	} {
		var out, errOut bytes.Buffer
		if code := New(Config{Out: &out, ErrOut: &errOut}).Run(context.Background(), []string{test.group, "--help"}); code != 0 || errOut.Len() != 0 {
			t.Fatalf("%s --help: exit=%d stderr=%q", test.group, code, errOut.String())
		}
		if has := strings.Contains(out.String(), "--context "); has != test.context {
			t.Errorf("%s help lists --context = %t, want %t:\n%s", test.group, has, test.context, out.String())
		}
		if has := strings.Contains(out.String(), "include SSH overrides"); has != test.sshHint {
			t.Errorf("%s help names the SSH overrides = %t, want %t:\n%s", test.group, has, test.sshHint, out.String())
		}
	}
}
