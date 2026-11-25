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
		{[]string{"apply", "--authorize", "erase=srv-01:retain"}, "apply only to destroy and destroy --dry-run, not apply"},
		{[]string{"apply", "--authorize", "abandon=srv-01"}, "apply only to destroy and destroy --dry-run, not apply"},
		{[]string{"destroy", "--dry-run", "--authorize", "data-loss"}, "destroy --dry-run accepts only --authorize erase="},
		{[]string{"destroy", "--dry-run", "--authorize", "rebind=bmc"}, "destroy --dry-run accepts only --authorize erase="},
		{[]string{"apply", "--dry-run", "--authorize", "data-loss"}, "--authorize is used when executing apply"},
		{[]string{"destroy", "--dry-run", "--unregistered", "srv-01,"}, "--unregistered must not contain an empty member"},
		{[]string{"destroy", "--authorize", "abandon"}, "must be abandon=<machine>"},
		{[]string{"destroy", "--authorize", "abandon=Srv_01"}, `"Srv_01" is not a Machine name`},
		{[]string{"destroy", "--authorize", "abandon=srv-01,erase=srv-01:retain"}, "an abandoned Machine is neither erased nor relaxed"},
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
		{"destroy", "--keep-going", "--authorize", "abandon=srv-03", "--authorize", "erase=srv-01:retain"},
		{"destroy", "--dry-run", "--authorize", "abandon=srv-03"},
		{"destroy", "--dry-run", "--unregistered", "srv-01"},
		{"apply", "--dry-run"},
	} {
		code, _, errOut, record := runRecorded(args)
		if code == 2 || record.calls != 1 {
			t.Fatalf("%v: code=%d calls=%d stderr=%q, want the request dispatched", args, code, record.calls, errOut)
		}
	}
}
