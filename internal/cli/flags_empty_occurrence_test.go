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
			want := "  error  cli.usage\n    --" + test.flag + " must not contain an empty occurrence\n"
			if code != 2 || out != "" || record.calls != 0 || !strings.HasPrefix(errOut, want) {
				t.Fatalf("code=%d out=%q calls=%d stderr=%q, want code 2, no call and stderr starting %q", code, out, record.calls, errOut, want)
			}
			if class := ClassifyInvocation(test.args); class.RequiresRoot {
				t.Fatalf("classification elevates a usage error: %+v", class)
			}
		})
	}
}

// --authorize takes data-loss, bare or bound to the 16-digit digest of the
// steps a plan shows, and one rebind=<secret> per rotated access credential,
// each at most once; anything else is a usage error before any call.
func TestAuthorizeAcceptsDataLossAndNamedRebindsOnly(t *testing.T) {
	want := "  error  cli.usage\n    --authorize accepts data-loss or data-loss=<digest> and rebind=<secret>, each at most once\n"
	for _, args := range [][]string{
		{"apply", "--authorize", "data-loss="},
		{"apply", "--authorize", "data-loss=ABCDEF0123456789"},
		{"apply", "--authorize", "data-loss=0123456789abcdef0"},
		{"apply", "--authorize", "data-loss,data-loss=0123456789abcdef"},
		{"destroy", "--authorize", "rebind="},
		{"destroy", "--authorize", "rebind=Not_A_Name"},
		{"destroy", "--authorize", "rebind=bmc,rebind=bmc"},
		{"apply", "--authorize", "data-loss", "--authorize", "data-loss"},
		{"apply", "--authorize", "everything"},
	} {
		code, _, errOut, record := runRecorded(args)
		if code != 2 || record.calls != 0 || !strings.HasPrefix(errOut, want) {
			t.Fatalf("%v: code=%d calls=%d stderr=%q, want a usage error", args, code, record.calls, errOut)
		}
	}
	for _, args := range [][]string{
		{"destroy", "--authorize", "data-loss,rebind=bmc", "--yes"},
		{"destroy", "--authorize", "data-loss=0123456789abcdef", "--yes"},
		{"apply", "--authorize", "rebind=bmc", "--authorize", "rebind=registry-login", "--yes"},
	} {
		if code, _, errOut, record := runRecorded(args); code == 2 || record.calls != 1 {
			t.Fatalf("%v: code=%d calls=%d stderr=%q, want the request dispatched", args, code, record.calls, errOut)
		}
	}
}

// --expect and a --replace member name a fingerprint only as
// <machine>=SHA256:<43 base64 characters>, one per Machine, and the request
// carries each beside the plain replacement names.
func TestMachineTrustFingerprintsAreNamedPerMachine(t *testing.T) {
	fingerprint := "SHA256:" + strings.Repeat("A", 43)
	other := "SHA256:" + strings.Repeat("B", 43)
	for _, args := range [][]string{
		{"machine", "trust", "--expect", "node-a"},
		{"machine", "trust", "--expect", "node-a=MD5:aa"},
		{"machine", "trust", "--replace", "node-a=" + fingerprint, "--expect", "node-a=" + other},
		{"machine", "trust", "--machines", "node-b", "--replace", "node-a=" + fingerprint},
	} {
		if code, _, _, record := runRecorded(args); code != 2 || record.calls != 0 {
			t.Fatalf("%v: code=%d calls=%d, want a usage error", args, code, record.calls)
		}
	}
	replace, expect := trustExpectations([]string{"node-a=" + fingerprint, "node-b"}, []string{"node-c=" + other})
	if strings.Join(replace, ",") != "node-a,node-b" || len(expect) != 2 || expect["node-a"] != fingerprint || expect["node-c"] != other {
		t.Fatalf("replace %v, expect %v", replace, expect)
	}
	if replace, expect := trustExpectations(nil, nil); replace != nil || expect != nil {
		t.Fatalf("no members became replace %v, expect %v", replace, expect)
	}
}

// --object-user reveals one user's keys of one named cluster, and never beside
// --secrets, so one invocation reveals one credential.
func TestObjectUserNamesOneUserOfOneCluster(t *testing.T) {
	for _, args := range [][]string{
		{"cluster", "info", "--object-user", "documents"},
		{"cluster", "info", "--name", "a,b", "--object-user", "documents"},
		{"cluster", "info", "--name", "storage", "--object-user", "documents", "--secrets"},
		{"cluster", "info", "--name", "storage", "--object-user", "Not_A_Name"},
	} {
		if code, _, _, record := runRecorded(args); code != 2 || record.calls != 0 {
			t.Fatalf("%v: code=%d calls=%d, want a usage error", args, code, record.calls)
		}
	}
	if code, _, errOut, record := runRecorded([]string{"cluster", "info", "--name", "storage", "--object-user", "documents"}); code == 2 || record.calls != 1 {
		t.Fatalf("code=%d calls=%d stderr=%q, want the request dispatched", code, record.calls, errOut)
	}
}
