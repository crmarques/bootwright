package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/availability"
	containeraccess "github.com/crmarques/bootwright/internal/containercluster/access"
	"github.com/crmarques/bootwright/internal/diagnostics"
	environmentaccess "github.com/crmarques/bootwright/internal/environment/access"
	"github.com/crmarques/bootwright/internal/machine"
	machineaccess "github.com/crmarques/bootwright/internal/machine/access"
)

func runRecorded(args []string) (int, string, string, *dispatchRecord) {
	var out, errOut bytes.Buffer
	record := &dispatchRecord{err: availability.ErrNotImplemented}
	code := New(Config{Out: &out, ErrOut: &errOut, Services: dispatchSpies(record)}).Run(context.Background(), args)
	return code, out.String(), errOut.String(), record
}

func TestRunnerDispatchesEveryApplicationCommand(t *testing.T) {
	cases := []string{
		"context init --name demo -f input", "context update --name demo -f input", "context use --name demo", "context list", "context current", "context delete --name demo --purge",
		"add-ons list", "add-ons add --name demo", "add-ons delete --name demo",
		"secret set --name demo --value-file secret.bin", "secret generate", "secret check", "secret list", "secret show --name demo --part value", "secret delete --name demo", "secret encryption init", "secret encryption status", "secret encryption rotate",
		"media add --name demo.iso --from-file image.iso", "media list", "media delete --name demo.iso", "validate", "preflight controller", "preflight infra", "preflight clusters", "preflight container-cluster", "preflight storage-cluster", "preflight add-ons", "preflight all", "plan", "status", "render --output-dir artifacts --sensitive", "render effective", "render installer", "render storage", "apply", "destroy",
		"machine list", "machine rsh --name demo", "machine exec --name demo echo", "machine trust", "setup", "cluster list", "cluster info", "cluster rsh --name demo", "cluster exec --name demo echo", "cluster oc --name demo get", "cluster kubectl --name demo get", "cluster kubeconfig --name demo",
	}
	for _, invocation := range cases {
		t.Run(invocation, func(t *testing.T) {
			code, out, errOut, record := runRecorded(strings.Fields(invocation))
			// A refusal before an SSH session opens exits 255, the client's own
			// failure status, so it never reads as the remote command's.
			want := 1
			if record.path == "machine rsh" || record.path == "machine exec" {
				want = 255
			}
			if code != want || out != "" || record.calls != 1 || errOut != "[FAIL] cli.not-implemented: bootwright "+record.path+" is not implemented\n" {
				t.Fatalf("code=%d out=%q err=%q calls=%d", code, out, errOut, record.calls)
			}
		})
	}
}

func TestHelpPrecedenceAndClosedSyntax(t *testing.T) {
	cases := []struct {
		args string
		code int
	}{
		{"", 0}, {"help", 0}, {"completion", 0}, {"context", 2}, {"secret encryption", 2}, {"context --help", 0}, {"--help context init", 0}, {"help context init", 0},
		{"context init --name= --help", 0}, {"context init -f a -f b --help", 0}, {"secret show --part invalid --help", 0}, {"status --output invalid --help", 0}, {"machine list --silent --output json --help", 0}, {"machine list --help --output invalid", 0},
		{"render --output json", 0}, {"render --clusters one --output json", 0}, {"render --output invalid", 2}, {"render --input-dir= --help", 0},
		{"version --help=false", 0}, {"version --help=invalid", 2}, {"machine --output json list", 2}, {"render --output json effective", 2}, {"render effective --input-dir input", 2},
		{"example --help", 2}, {"help example", 2}, {"container-cluster --help", 2}, {"help container-cluster", 2}, {"clu list", 2}, {"VERSION", 2}, {"--version", 2}, {"version --format json", 2}, {"__complete", 2}, {"__completeNoDesc", 2}, {"help __bootwright_complete", 2},
		{"version --", 2}, {"version --help --bad", 2}, {"machine list --silent invalid --help", 0}, {"machine list --silent=invalid --help", 2}, {"version -vh", 2},
	}
	for _, tc := range cases {
		t.Run(tc.args, func(t *testing.T) {
			code, out, errOut, record := runRecorded(strings.Fields(tc.args))
			if code != tc.code || record.calls != 0 {
				t.Fatalf("code=%d out=%q err=%q calls=%d", code, out, errOut, record.calls)
			}
			if tc.code == 0 && (out == "" || errOut != "") {
				t.Fatalf("help/version streams: out=%q err=%q", out, errOut)
			}
		})
	}
}

func TestResolutionFailuresExplainOnlyTrustedReasons(t *testing.T) {
	const privateArgument = "do-not-display-private-argument"
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"separator", []string{"version", "--", privateArgument}, "separator is not accepted"},
		{"flag-position", []string{"version", "--" + privateArgument}, "flag is not accepted at this position"},
		{"missing-value", []string{"context", "init", "--name"}, "flag value is missing"},
		{"command", []string{privateArgument}, "unknown command"},
		{"flag-owner", []string{"render", "--output", "text", "effective"}, "flag does not apply to the selected subcommand"},
		{"help-command", []string{"help", privateArgument}, "unknown help command"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			code, out, errOut, record := runRecorded(test.args)
			if code != 2 || out != "" || record.calls != 0 || !strings.HasPrefix(errOut, "[FAIL] cli.usage: "+test.want+"\n") {
				t.Fatalf("code=%d out=%q stderr=%q calls=%d", code, out, errOut, record.calls)
			}
			if strings.Contains(errOut, privateArgument) {
				t.Fatalf("resolution diagnostic exposed argv: %q", errOut)
			}
		})
	}
	for _, reason := range []resolutionError{
		separatorNotAccepted,
		flagNotAccepted,
		flagValueMissing,
		unknownCommand,
		localFlagNotInherited,
		commandResolutionFailed,
		unknownHelpCommand,
	} {
		if got := trustedResolutionMessage(reason); got != reason.Error() {
			t.Fatalf("trusted resolution reason changed: got %q, want %q", got, reason)
		}
	}

	for _, err := range []error{errors.New(privateArgument), resolutionError(privateArgument)} {
		if got := trustedResolutionMessage(err); got != "invalid command or flag syntax" {
			t.Fatalf("untrusted resolution error became diagnostic: %q", got)
		}
	}
}

// TestWithdrawnFlagsAreUsageErrors keeps the withdrawn status --watch and
// --watch-interval, and -v, --verbose, out of every command that accepted them:
// each is now an unlisted flag, so it fails resolution as cli.usage with exit 2
// before explicit help and reaches no use case.
func TestWithdrawnFlagsAreUsageErrors(t *testing.T) {
	for _, args := range [][]string{
		{"status", "--watch"}, {"status", "--watch=false"}, {"status", "--watch-interval", "5s"}, {"status", "--watch", "--help"},
		{"apply", "--verbose"}, {"apply", "-v"}, {"apply", "--yes", "--verbose=false"}, {"destroy", "--verbose"}, {"destroy", "-v", "--help"},
		{"preflight", "infra", "--verbose"}, {"preflight", "clusters", "-v"}, {"preflight", "container-cluster", "--verbose"}, {"preflight", "storage-cluster", "--verbose"}, {"preflight", "all", "-v"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			code, out, errOut, record := runRecorded(args)
			if code != 2 || out != "" || record.calls != 0 || !strings.HasPrefix(errOut, "[FAIL] cli.usage: flag is not accepted at this position\n") {
				t.Fatalf("code=%d out=%q stderr=%q calls=%d", code, out, errOut, record.calls)
			}
		})
	}
	code, out, errOut, record := runRecorded([]string{"status", "--output", "json", "--watch"})
	var envelope struct {
		Command     string
		OK          bool
		ExitCode    int
		Diagnostics []struct{ Code string }
	}
	if err := json.Unmarshal([]byte(out), &envelope); err != nil || code != 2 || errOut != "" || record.calls != 0 || envelope.Command != "status" || envelope.OK || envelope.ExitCode != 2 || len(envelope.Diagnostics) != 1 || envelope.Diagnostics[0].Code != "cli.usage" {
		t.Fatalf("JSON usage: code=%d out=%q err=%q calls=%d decode=%v", code, out, errOut, record.calls, err)
	}
}

func TestJSONUsageUsesFinalOutputAndScalarValues(t *testing.T) {
	cases := []struct {
		args  []string
		code  int
		json  bool
		calls int
	}{
		{[]string{"machine", "list", "--silent=true", "--output", "json"}, 2, true, 0},
		{[]string{"machine", "list", "--output", "json", "--silent=true"}, 2, true, 0},
		{[]string{"machine", "list", "--silent=true", "--silent=false", "--output", "json"}, 1, true, 1},
		{[]string{"machine", "list", "--silent=false", "--silent=true", "--output", "json"}, 2, true, 0},
		{[]string{"machine", "list", "--bad", "--output", "json"}, 2, true, 0},
		{[]string{"machine", "list", "--output", "json", "--bad"}, 2, true, 0},
		{[]string{"machine", "list", "--silent=invalid", "--output", "json"}, 2, true, 0},
		{[]string{"machine", "list", "--output", "json", "--silent=invalid"}, 2, true, 0},
		{[]string{"machine", "list", "--output", "json", "--bad", "--output", "invalid"}, 2, false, 0},
		{[]string{"machine", "list", "--output", "invalid", "--bad", "--output", "json"}, 2, true, 0},
		{[]string{"machine", "list", "--output=", "--output=json"}, 2, true, 0},
		{[]string{"machine", "list", "--bad", "--clusters", "--output", "json"}, 2, false, 0},
	}
	for _, tc := range cases {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			code, out, errOut, record := runRecorded(tc.args)
			if code != tc.code || record.calls != tc.calls {
				t.Fatalf("code=%d out=%q err=%q calls=%d", code, out, errOut, record.calls)
			}
			if tc.json {
				var envelope struct {
					Command     string
					OK          bool
					ExitCode    int
					Result      any
					Diagnostics []struct{ Code string }
					Logs        []string
				}
				if err := json.Unmarshal([]byte(out), &envelope); err != nil || errOut != "" || !strings.HasSuffix(out, "\n") || envelope.Command != "machine list" || envelope.OK || envelope.ExitCode != code || envelope.Result != nil || len(envelope.Diagnostics) != 1 || len(envelope.Logs) != 0 {
					t.Fatalf("invalid envelope: out=%q err=%q decode=%v", out, errOut, err)
				}
			} else if out != "" || !strings.Contains(errOut, "cli.usage") {
				t.Fatalf("human usage: out=%q err=%q", out, errOut)
			}
		})
	}
}

func TestRawFlagParseFailureRemainsPrivate(t *testing.T) {
	const privateValue = "do-not-display-private-value"
	code, out, errOut, record := runRecorded([]string{"machine", "list", "--silent=" + privateValue, "--output", "json"})
	var envelope commandEnvelope
	if err := json.Unmarshal([]byte(out), &envelope); err != nil {
		t.Fatal(err)
	}
	if code != 2 || errOut != "" || record.calls != 0 || len(envelope.Diagnostics) != 1 || envelope.Diagnostics[0].Message != "invalid flag syntax or value" {
		t.Fatalf("code=%d out=%q stderr=%q calls=%d", code, out, errOut, record.calls)
	}
	if strings.Contains(out, privateValue) {
		t.Fatalf("flag parser detail escaped into diagnostic: %q", out)
	}
}

func TestPayloadParsing(t *testing.T) {
	cases := []struct {
		path string
		tail []string
		want []string
		code int
	}{
		{"machine exec", []string{"echo", "one", "--context", "other", "two"}, []string{"echo", "one", "two"}, 255},
		{"cluster exec", []string{"echo", "one", "--context", "other", "two"}, []string{"echo", "one", "two"}, 1},
		{"cluster oc", []string{"get", "pods", "--context", "other", "--help"}, []string{"get", "pods", "--context", "other", "--help"}, 1},
		{"cluster kubectl", []string{"get", "pods", "--context", "other", "--help"}, []string{"get", "pods", "--context", "other", "--help"}, 1},
		{"machine exec", []string{"--", "--help", "", "a b", "$(payload)"}, []string{"--help", "", "a b", "$(payload)"}, 255},
		{"cluster exec", []string{"--", "--help"}, []string{"--help"}, 1},
		{"cluster oc", []string{"--", "--help"}, []string{"--help"}, 1},
		{"cluster kubectl", []string{"--", "--help"}, []string{"--help"}, 1},
		{"cluster oc", []string{"--help"}, nil, 0},
		{"machine exec", []string{"--", ""}, nil, 2},
		{"cluster exec", nil, nil, 2},
		{"machine exec", []string{"--unknown"}, nil, 2},
	}
	for _, tc := range cases {
		t.Run(tc.path+strings.Join(tc.tail, " "), func(t *testing.T) {
			args := append(strings.Fields(tc.path), "--name", "demo")
			args = append(args, tc.tail...)
			code, out, errOut, record := runRecorded(args)
			if code != tc.code {
				t.Fatalf("code=%d out=%q err=%q", code, out, errOut)
			}
			if tc.want == nil {
				if record.calls != 0 {
					t.Fatal("unexpected dispatch")
				}
				return
			}
			var got []string
			switch request := record.request.(type) {
			case machineaccess.ExecRequest:
				got = request.Command
			case environmentaccess.ClusterExecRequest:
				got = request.Command
			case containeraccess.OCRequest:
				got = request.Command
			case containeraccess.KubectlRequest:
				got = request.Command
			default:
				t.Fatalf("unexpected request %T", request)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("payload=%q want=%q", got, tc.want)
			}
		})
	}
}

type rejectingWriter struct{ short bool }

func (w rejectingWriter) Write(p []byte) (int, error) {
	if w.short {
		return len(p) / 2, nil
	}
	return 0, errors.New("unavailable writer")
}

func TestOutputFailureHasNoFallback(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"version"}, {"machine", "list", "--output", "json"}, {"completion", "bash"}, {"__bootwright_complete", ""}} {
		for _, short := range []bool{false, true} {
			var errOut bytes.Buffer
			record := &dispatchRecord{err: availability.ErrNotImplemented}
			code := New(Config{Out: rejectingWriter{short: short}, ErrOut: &errOut, Services: dispatchSpies(record)}).Run(context.Background(), args)
			if code != 1 || errOut.Len() != 0 {
				t.Errorf("%v short=%t code=%d stderr=%q", args, short, code, errOut.String())
			}
		}
	}
	if code := New(Config{Out: io.Discard, ErrOut: rejectingWriter{short: true}}).Run(context.Background(), []string{"missing"}); code != 1 {
		t.Errorf("stderr failure code %d", code)
	}
}

// A session's streams and exit status belong to the remote process. Bootwright
// adds no status line of its own and returns exactly what the client reported.
func TestASessionReportsTheClientsExitStatusAndPrintsNothing(t *testing.T) {
	for _, invocation := range []string{"machine rsh --name demo", "machine exec --name demo pwd"} {
		t.Run(invocation, func(t *testing.T) {
			for _, want := range []int{0, 1, 7, 130, 255} {
				var out, errOut bytes.Buffer
				record := &dispatchRecord{result: commandResult{
					session: &machine.SessionResult{Context: "lab", Machine: "demo", Address: "192.0.2.10", ExitCode: want},
				}}
				code := New(Config{Out: &out, ErrOut: &errOut, Services: dispatchSpies(record)}).
					Run(context.Background(), strings.Fields(invocation))
				if code != want {
					t.Fatalf("exit = %d, want %d", code, want)
				}
				if out.Len() != 0 || errOut.Len() != 0 {
					t.Fatalf("out = %q err = %q", out.String(), errOut.String())
				}
			}
			// An interrupt that arrives while the session runs is the
			// session's to answer: its own status stands and nothing is added.
			for _, want := range []int{0, 143, 255} {
				var out, errOut bytes.Buffer
				var cancel context.CancelCauseFunc
				record := &dispatchRecord{result: commandResult{
					session: &machine.SessionResult{Context: "lab", Machine: "demo", Address: "192.0.2.10", ExitCode: want},
				}}
				record.afterCall = func() { cancel(ErrInterrupted) }
				code := New(Config{Out: &out, ErrOut: &errOut, Services: dispatchSpies(record), BeginOperation: func(ctx context.Context) (context.Context, func()) {
					ctx, cancel = context.WithCancelCause(ctx)
					return ctx, func() { cancel(nil) }
				}}).Run(context.Background(), strings.Fields(invocation))
				if code != want || out.Len() != 0 || errOut.Len() != 0 {
					t.Fatalf("interrupted session = %d, want %d; out = %q err = %q", code, want, out.String(), errOut.String())
				}
			}
		})
	}
}

// A session's status from 0 to 254 is the remote command's, so every refusal
// Bootwright reports before the session opens exits 255, the SSH client's own
// failure status. A refusal of how the command was invoked keeps 2 and an
// interrupt before the session keeps 130.
func TestASessionRefusalExitsTwoFiftyFive(t *testing.T) {
	refusal := func(code string) error {
		return diagnostics.NewFailureWithRemediation(code, "the session cannot open", "", "do what it says")
	}
	for _, invocation := range []string{"machine rsh --name demo", "machine exec --name demo pwd"} {
		for _, test := range []struct {
			name      string
			err       error
			interrupt bool
			code      int
			report    string
		}{
			{name: "trust.identity", err: refusal("trust.identity"), code: 255, report: "[FAIL] trust.identity: the session cannot open; next: do what it says\n"},
			{name: "access.target", err: refusal("access.target"), code: 255, report: "[FAIL] access.target: the session cannot open; next: do what it says\n"},
			{name: "access.unavailable", err: refusal("access.unavailable"), code: 255, report: "[FAIL] access.unavailable: the session cannot open; next: do what it says\n"},
			{name: "canceled", err: context.Canceled, code: 255, report: "[FAIL] runtime.canceled: operation canceled\n"},
			{name: "deadline", err: context.DeadlineExceeded, code: 255, report: "[FAIL] runtime.deadline: operation deadline exceeded\n"},
			{name: "unsupported", code: 255, report: "[FAIL] runtime.internal: application service returned an unsupported result\n"},
			{name: "usage", err: &diagnostics.Failure{Diagnostics: []diagnostics.Diagnostic{{Severity: "error", Code: "access.target", Message: "the session cannot open"}}, Usage: true}, code: 2},
			{name: "interrupted", err: context.Canceled, interrupt: true, code: 130, report: "[FAIL] runtime.interrupted: operation interrupted\n"},
		} {
			t.Run(invocation+"/"+test.name, func(t *testing.T) {
				var out, errOut bytes.Buffer
				var cancel context.CancelCauseFunc
				record := &dispatchRecord{err: test.err}
				if test.interrupt {
					record.afterCall = func() { cancel(ErrInterrupted) }
				}
				code := New(Config{Out: &out, ErrOut: &errOut, Services: dispatchSpies(record), BeginOperation: func(ctx context.Context) (context.Context, func()) {
					ctx, cancel = context.WithCancelCause(ctx)
					return ctx, func() { cancel(nil) }
				}}).Run(context.Background(), strings.Fields(invocation))
				if code != test.code || out.Len() != 0 || test.report != "" && errOut.String() != test.report {
					t.Fatalf("exit = %d, want %d; out = %q err = %q", code, test.code, out.String(), errOut.String())
				}
			})
		}
	}
}
