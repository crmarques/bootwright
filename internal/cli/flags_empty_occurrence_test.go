package cli

import (
	"strings"
	"testing"
)

func TestASoleEmptyOccurrenceOfARepeatableFlagIsAUsageError(t *testing.T) {
	for _, test := range []struct {
		args []string
		flag string
	}{
		{[]string{"validate", "--file="}, "file"},
		{[]string{"validate", "-f", ""}, "file"},
		{[]string{"context", "init", "--name", "demo", "--file="}, "file"},
		{[]string{"context", "update", "--name", "demo", "--file=", "--input-dir", "in"}, "file"},
		{[]string{"apply", "--authorize="}, "authorize"},
		{[]string{"destroy", "--authorize", ""}, "authorize"},
	} {
		t.Run(strings.Join(test.args, " "), func(t *testing.T) {
			code, out, errOut, record := runRecorded(test.args)
			want := "[FAIL] cli.usage: --" + test.flag + " must not contain an empty occurrence\n"
			if code != 2 || out != "" || record.calls != 0 || !strings.HasPrefix(errOut, want) {
				t.Fatalf("code=%d out=%q calls=%d stderr=%q, want code 2, no call and stderr starting %q", code, out, record.calls, errOut, want)
			}
			if class := ClassifyInvocation(test.args); class.RequiresRoot {
				t.Fatalf("classification elevates a usage error: %+v", class)
			}
		})
	}
}
