package cli

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"regexp"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/machine/power"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/reconciliation/logview"
)

func runLogs(t *testing.T, record *dispatchRecord, args ...string) (int, string, string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := New(Config{Out: &out, ErrOut: &errOut, Services: dispatchSpies(record)}).Run(context.Background(), args)
	return code, out.String(), errOut.String()
}

// Every selection logview would refuse, and the two only the CLI can, are
// usage errors before privilege: the invocation is classified as needing no
// root and no service is called.
func TestLogsRefusesConflictingSelections(t *testing.T) {
	for _, test := range []struct {
		args []string
		want string
	}{
		{[]string{"--block", "b", "--run", "run-000001"}, "--run and --install select one whole output"},
		{[]string{"--raw", "--block", "b", "--install"}, "--run and --install select one whole output"},
		{[]string{"--attempt", "1", "--raw", "--block", "b", "--run", "run-000001"}, "--run and --install select one whole output"},
		{[]string{"--run", "run-000001", "--install"}, "--run and --install select different outputs"},
		{[]string{"--operation", "op-0123456789abcdef0123456789abcdef", "--run", "run-000001"}, "--run and --install select one whole output"},
		{[]string{"--install", "--context", "lab"}, "--install reads the host's controller install output and takes no --context"},
		{[]string{"--raw"}, "--raw needs the --block"},
		{[]string{"--attempt", "2"}, "--attempt selects the output --raw reads"},
		{[]string{"--attempt", "0", "--raw", "--block", "b"}, "--attempt is a number from 1 to 999999"},
		{[]string{"--attempt", "two", "--raw", "--block", "b"}, "--attempt is a number from 1 to 999999"},
		{[]string{"--attempt", "1000000", "--raw", "--block", "b"}, "--attempt is a number from 1 to 999999"},
		{[]string{"--follow", "--raw", "--block", "b"}, "--follow streams events and output as they are written; --raw reads one whole output"},
		{[]string{"--operation", "op-1"}, "--operation names an operation as op- and 32 lowercase hexadecimal digits"},
		{[]string{"--block", "Bad/Block"}, "--block names a block of the operation's plan"},
		{[]string{"--run", "run-1"}, "--run names a bounded run as run-NNNNNN"},
		{[]string{"--install=latest"}, "--install names a controller install as newest or install-NNNNNN"},
	} {
		args := append([]string{"logs"}, test.args...)
		if ClassifyInvocation(args).RequiresRoot {
			t.Errorf("%v needs root, want a refusal before privilege", args)
		}
		record := &dispatchRecord{}
		code, out, errOut := runLogs(t, record, args...)
		if code != 2 || record.calls != 0 || out != "" || !strings.Contains(errOut, "cli.usage") || !strings.Contains(errOut, test.want) {
			t.Errorf("%v: exit %d after %d calls, stdout %q, stderr %q; want usage %q", args, code, record.calls, out, errOut, test.want)
		}
	}
	for _, valid := range [][]string{{"logs"}, {"logs", "--follow"}, {"logs", "--install", "--follow"}, {"logs", "--install=install-000003"}, {"logs", "--run", "run-000003", "--follow"},
		{"logs", "--operation", "op-0123456789abcdef0123456789abcdef", "--block", "b", "--raw", "--attempt", "2"}} {
		if !ClassifyInvocation(valid).RequiresRoot {
			t.Errorf("%v was refused or needs no root, want it admitted as privileged as status is", valid)
		}
	}
}

func TestFollowRefusesStructuredOutputBeforePrivilege(t *testing.T) {
	for _, format := range []string{"json", "yaml"} {
		args := []string{"logs", "--follow", "--output", format}
		if ClassifyInvocation(args).RequiresRoot {
			t.Fatalf("%v needs root, want a refusal before privilege", args)
		}
		record := &dispatchRecord{}
		code, out, _ := runLogs(t, record, args...)
		if code != 2 || record.calls != 0 || !strings.Contains(out, `cli.usage`) || !strings.Contains(out, "--follow streams lines and has no") {
			t.Fatalf("%s: exit %d after %d calls with %q", format, code, record.calls, out)
		}
	}
}

// A bare --install reads the newest controller install, and its value is taken only after
// =: one written after a space is an operand, refused with the spelling.
func TestInstallOptionalValueNeedsEquals(t *testing.T) {
	for _, test := range []struct {
		args []string
		want string
	}{{[]string{"logs", "--install"}, logview.NewestInstall}, {[]string{"logs", "--install=install-000003"}, "install-000003"}} {
		record := &dispatchRecord{result: commandResult{logs: logsStream(logview.SourceInstall, "install-000003", "controller/install-000003.output")}}
		if code, _, errOut := runLogs(t, record, test.args...); code != 0 || record.request.(logview.Selection).Install != test.want {
			t.Fatalf("%v: exit %d (%q), selection %+v, want install %q", test.args, code, errOut, record.request, test.want)
		}
	}
	record := &dispatchRecord{}
	code, _, errOut := runLogs(t, record, "logs", "--install", "install-000003")
	if code != 2 || record.calls != 0 || !strings.Contains(errOut, "--install takes its value after =; write --install=install-000003") {
		t.Fatalf("exit %d after %d calls with %q", code, record.calls, errOut)
	}
	if code, _, errOut := runLogs(t, &dispatchRecord{}, "logs", "--install", "other"); code != 2 || !strings.Contains(errOut, "this command accepts no positional operands") {
		t.Fatalf("an unrelated operand exited %d with %q", code, errOut)
	}
}

// Every line, followed or read whole, reaches the terminal through the one
// display escaper: an escape sequence prints as its escaped text, a CR before
// the LF is dropped, and a CR elsewhere is shown.
func TestLogLinesEscapeTerminalSequences(t *testing.T) {
	record := &dispatchRecord{
		followed: []followedLine{
			{Line: logview.Line{Source: "machine-rhel-01", Text: "\x1b[31mred"}},
			{Line: logview.Line{Text: "12:00:00  -  opened  \x1b]0;title\x07"}, event: true},
		},
		result: commandResult{logsFollow: &logview.Summary{Context: "lab"}},
	}
	code, out, _ := runLogs(t, record, "logs", "--follow")
	if want := "[machine-rhel-01] \\u001b[31mred\n12:00:00  -  opened  \\u001b]0;title\\u0007\n"; code != 0 || out != want {
		t.Fatalf("followed exit %d %q, want %q", code, out, want)
	}
	raw := logsStream(logview.SourceRun, "run-000003", "contexts/lab/runs/run-000003.output")
	raw.Raw.Content = []byte("\x1b[31mred\r\nkeep\rcr\nlast")
	code, out, _ = runLogs(t, &dispatchRecord{result: commandResult{logs: raw}}, "logs", "--run", "run-000003")
	if want := "[run-000003] \\u001b[31mred\n[run-000003] keep\\rcr\n[run-000003] last\n"; code != 0 || out != want {
		t.Fatalf("raw exit %d %q, want %q", code, out, want)
	}
}

// Structured output carries the exact bytes: as UTF-8 text when they hold no
// control character, otherwise as base64 of the very bytes.
func TestRawStructuredOutputIsBase64WhenItCarriesControls(t *testing.T) {
	for _, test := range []struct {
		content, encoding string
	}{{"PLAY [power]\nok\n", "utf-8"}, {"\x1b[31mred\r\n", "base64"}, {"\xff\xfe", "base64"}} {
		result := logsStream(logview.SourceRun, "run-000003", "contexts/lab/runs/run-000003.output")
		result.Raw.Content = []byte(test.content)
		code, out, _ := runLogs(t, &dispatchRecord{result: commandResult{logs: result}}, "logs", "--run", "run-000003", "--output", "json")
		var envelope struct {
			Result struct {
				Truncated *bool `json:"truncated"`
				Output    struct{ Encoding, Content string }
			}
		}
		if err := json.Unmarshal([]byte(out), &envelope); err != nil || code != 0 {
			t.Fatalf("exit %d %q (%v)", code, out, err)
		}
		content := envelope.Result.Output.Content
		if envelope.Result.Output.Encoding == "base64" {
			decoded, err := base64.StdEncoding.DecodeString(content)
			if err != nil {
				t.Fatal(err)
			}
			content = string(decoded)
		}
		if envelope.Result.Output.Encoding != test.encoding || content != test.content || envelope.Result.Truncated != nil {
			t.Fatalf("%q encoded as %+v, want %s of the same bytes and no truncated member for a run", test.content, envelope.Result, test.encoding)
		}
	}
}

// Every Logs field a person reads is followed by the command that reads it:
// while it runs, the form that streams it; once settled, the form pinned to
// what wrote it, so old scrollback never names a newer operation.
func TestEveryLogsFieldHasItsHint(t *testing.T) {
	const operation = "op-0123456789abcdef0123456789abcdef"
	render := func(write func(*display)) string {
		var text display
		write(&text)
		var out bytes.Buffer
		if err := text.writeTo(&out); err != nil {
			t.Fatal(err)
		}
		return out.String()
	}
	progress := func(report func(io.Writer)) string {
		var out bytes.Buffer
		report(&out)
		return out.String()
	}
	operationResult := func(state string) string {
		var out bytes.Buffer
		if err := writeLifecycleOperation(&out, &lifecycle.OperationResult{Context: lifecycle.ContextIdentity{Name: "lab"}, Verb: "apply", LogLocation: "/logs",
			Receipt: lifecycle.Receipt{Operation: operation, Verb: "apply", State: state, Next: "none"}}); err != nil {
			t.Fatal(err)
		}
		return out.String()
	}
	statusResult := func(state string) string {
		return render(func(text *display) {
			writeStatusLifecycle(text, &lifecycle.StatusResult{Context: lifecycle.ContextIdentity{Name: "lab"}, LogLocation: "/logs",
				Lifecycle: &lifecycle.LifecycleSummary{Operation: operation, Verb: "apply", State: state}})
		})
	}
	for name, test := range map[string]struct{ got, want string }{
		"apply progress": {progress(func(out io.Writer) {
			(&LifecycleProgressPresenter{progress: progressPresenter{out: out, clock: newManualClock().clock()}}).ReportLogLocation(context.Background(), lifecycle.LogLocation{Path: "/logs", Context: "lab", Operation: operation})
		}), "bootwright logs --context lab --follow\n"},
		"run progress": {progress(func(out io.Writer) {
			(&LifecycleProgressPresenter{progress: progressPresenter{out: out, clock: newManualClock().clock()}}).ReportLogLocation(context.Background(), lifecycle.LogLocation{Path: "/logs", Context: "lab", Run: "run-000003"})
		}), "bootwright logs --context lab --run run-000003 --follow\n"},
		"setup progress": {progress(func(out io.Writer) {
			(&ControllerPresenter{progress: progressPresenter{out: out, clock: newManualClock().clock()}}).ReportLogLocation(context.Background(), prerequisites.LogLocation{Path: "/logs", Setup: "install-000003"})
		}), "bootwright logs --install=install-000003 --follow\n"},
		"running apply":  {operationResult("running"), "bootwright logs --context lab --follow\n"},
		"settled apply":  {operationResult("failed"), "bootwright logs --context lab --operation " + operation + "\n"},
		"running status": {statusResult("running"), "bootwright logs --context lab --follow\n"},
		"settled status": {statusResult("done"), "bootwright logs --context lab --operation " + operation + "\n"},
		"setup result": {render(func(text *display) {
			text.fields(controllerOutcomeFields("controller install", &prerequisites.Report{Outcome: "changed", LogLocation: "/logs", SetupRun: "install-000003"})...)
		}), "bootwright logs --install=install-000003\n"},
		"power result": {func() string {
			var out bytes.Buffer
			_ = writeSingleMachinePower(&out, power.Result{Context: "lab", Machine: "m", Power: "off", LogLocation: "/logs", Run: "run-000003"})
			return out.String()
		}(), "bootwright logs --context lab --run run-000003\n"},
	} {
		follow := regexp.MustCompile(`(?m)^ +Follow +` + regexp.QuoteMeta(strings.TrimSuffix(test.want, "\n")) + `$`)
		if !follow.MatchString(test.got) || strings.Count(test.got, "Follow") != 1 || !strings.Contains(test.got, "Logs") {
			t.Errorf("%s:\n%s\nwant one Logs field and %q", name, test.got, test.want)
		}
	}
	if logsCommand(logsHint{Context: "lab"}) != "" || logsCommand(logsHint{Context: "Bad Name", Running: true}) != "" || logsCommand(logsHint{Context: "lab", Operation: "op-1"}) != "" {
		t.Fatal("a hint was offered for facts that name nothing logs could read")
	}
}

func TestRootHelpListsLogsUnderContext(t *testing.T) {
	_, out, _ := runLogs(t, &dispatchRecord{}, "help")
	context := strings.Index(out, "\n  Context\n")
	status, logs, diff, resources := strings.Index(out, "\n  status "), strings.Index(out, "\n  logs "), strings.Index(out, "\n  diff "), strings.Index(out, "\n  Resources\n")
	if context < 0 || !(context < status && status < logs && logs < diff && diff < resources) {
		t.Fatalf("logs is not listed after status in the Context group:\n%s", out)
	}
}
