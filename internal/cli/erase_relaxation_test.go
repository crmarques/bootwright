package cli

import (
	"strings"
	"testing"
)

func TestEraseRelaxationUsageErrors(t *testing.T) {
	for _, test := range []struct {
		args []string
		want string
	}{
		{[]string{"destroy", "--authorize", "erase=srv-01:retain,erase=srv-01:metadataClear"}, "names Machine srv-01 more than once"},
		{[]string{"destroy", "--authorize", "erase=srv-01:retain", "--authorize", "erase=srv-01:retain"}, "names Machine srv-01 more than once"},
		{[]string{"destroy", "--authorize", "erase=srv-01:wipe"}, `"wipe" is not a relaxation; use metadataClear or retain`},
		{[]string{"destroy", "--authorize", "erase=srv-01:logicalOverwrite"}, "a relaxation never strengthens or restates the declared method"},
		{[]string{"destroy", "--dry-run", "--authorize", "erase=srv-01:logicalOverwrite"}, "a relaxation never strengthens or restates the declared method"},
		{[]string{"destroy", "--authorize", "erase="}, "must be erase=<machine>:metadataClear or erase=<machine>:retain"},
		{[]string{"destroy", "--authorize", "erase"}, "must be erase=<machine>:metadataClear or erase=<machine>:retain"},
		{[]string{"destroy", "--authorize", "erase=srv-01"}, "must be erase=<machine>:metadataClear or erase=<machine>:retain"},
		{[]string{"destroy", "--authorize", "erase=srv-01:"}, "must be erase=<machine>:metadataClear or erase=<machine>:retain"},
		{[]string{"destroy", "--authorize", "erase=:retain"}, "must be erase=<machine>:metadataClear or erase=<machine>:retain"},
		{[]string{"destroy", "--authorize", "erase=Srv_01:retain"}, `"Srv_01" is not a Machine name`},
		{[]string{"destroy", "--dry-run", "--authorize", "erase=srv-01:retain,"}, "--authorize must not contain an empty member"},
		{[]string{"destroy", "--authorize", "erase=srv-01:retain,,data-loss"}, "--authorize must not contain an empty member"},
		{[]string{"apply", "--authorize", "erase=srv-01:retain"}, "applies only to destroy and destroy --dry-run, not apply"},
		{[]string{"destroy", "--dry-run", "--authorize", "data-loss"}, "destroy --dry-run accepts only --authorize erase="},
		{[]string{"destroy", "--dry-run", "--authorize", "rebind=bmc"}, "destroy --dry-run accepts only --authorize erase="},
	} {
		t.Run(strings.Join(test.args, " "), func(t *testing.T) {
			code, _, errOut, record := runRecorded(test.args)
			if code != 2 || record.calls != 0 || !strings.HasPrefix(errOut, "  error  cli.usage\n") || !strings.Contains(strings.Join(strings.Fields(errOut), " "), test.want) {
				t.Fatalf("code=%d calls=%d stderr=%q, want a usage error containing %q", code, record.calls, errOut, test.want)
			}
		})
	}
}

func TestEraseRelaxationsAreCarriedBesideTheOtherAuthorizations(t *testing.T) {
	for _, args := range [][]string{
		{"destroy", "--authorize", "data-loss,erase=srv-01:retain", "--authorize", "erase=srv-02:metadataClear", "--yes"},
		{"destroy", "--dry-run", "--authorize", "erase=srv-01:retain,erase=srv-02:metadataClear"},
		{"apply", "--dry-run"},
	} {
		code, _, errOut, record := runRecorded(args)
		if code == 2 || record.calls != 1 {
			t.Fatalf("%v: code=%d calls=%d stderr=%q, want the request dispatched", args, code, record.calls, errOut)
		}
	}
}
