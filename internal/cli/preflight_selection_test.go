package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

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
		{"context", false, false},
		{"catalog media", false, false},
		{"catalog add-ons", false, false},
		{"completion", false, false},
		{"machine", true, true},
		{"cluster", true, true},
	} {
		var out, errOut bytes.Buffer
		if code := New(Config{Out: &out, ErrOut: &errOut}).Run(context.Background(), append(strings.Fields(test.group), "--help")); code != 0 || errOut.Len() != 0 {
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
