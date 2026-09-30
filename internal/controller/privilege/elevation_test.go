package privilege

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"slices"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/crmarques/bootwright/internal/controller"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

// scriptedChild is what the fake sudo does once the -ll probe is answered:
// it writes the child's streams in order, runs during, then ends.
type scriptedChild struct {
	stdout string
	stderr []string
	code   int
	err    error
	during func()
}

func scriptedElevator(child scriptedChild, commands *[]Command) Elevator {
	executor := executorFunc(func(_ context.Context, c Command) (int, error) {
		if slices.Equal(c.Arguments, []string{"-n", "-u", "#0", "-ll"}) {
			return 1, nil
		}
		if commands != nil {
			*commands = append(*commands, c)
		}
		if child.stdout != "" {
			io.WriteString(c.Output, child.stdout)
		}
		for _, chunk := range child.stderr {
			io.WriteString(c.Error, chunk)
		}
		if child.during != nil {
			child.during()
		}
		return child.code, child.err
	})
	delay := delayFunc(func(ctx context.Context, _ time.Duration) error {
		<-ctx.Done()
		return ctx.Err()
	})
	return Elevator{
		Executable: func() (string, error) { return "/proc/4242/exe", nil },
		Sudo:       func() (string, error) { return "/usr/bin/sudo", nil },
		Executor:   executor,
		Delay:      delay,
	}
}

// elevationCase runs one scripted child. With errorTerminal, standard error
// is a terminal the child writes to directly, as sudo hands it to an
// interactive child, and stderr is everything that reached either writer.
type elevationCase struct {
	name                          string
	json, terminal, errorTerminal bool
	child                         scriptedChild
	exit                          int
	code, message                 string
	stderr                        string
}

func (c elevationCase) check(t *testing.T, ctx context.Context) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	invocation := Invocation{JSON: c.json, InputTerminal: c.terminal, Arguments: []string{"status"}, Output: &stdout, Error: &stderr}
	var terminal *os.File
	if c.errorTerminal {
		read, write, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		defer read.Close()
		defer write.Close()
		terminal, invocation.ErrorTerminal = read, write
	}
	outcome := scriptedElevator(c.child, nil).Run(ctx, invocation)
	if terminal != nil {
		invocation.ErrorTerminal.Close()
		if _, err := stderr.ReadFrom(terminal); err != nil {
			t.Fatal(err)
		}
	}
	if outcome.ExitCode != c.exit {
		t.Errorf("exit = %d, want %d", outcome.ExitCode, c.exit)
	}
	var code, message string
	if outcome.Diagnostic != nil {
		code, message = outcome.Diagnostic.Code, outcome.Diagnostic.Message
		if outcome.Diagnostic.Severity != "error" {
			t.Errorf("severity = %q", outcome.Diagnostic.Severity)
		}
	}
	if code != c.code || message != c.message {
		t.Errorf("diagnostic = %q %q, want %q %q", code, message, c.code, c.message)
	}
	if stderr.String() != c.stderr {
		t.Errorf("stderr = %q, want %q", stderr.String(), c.stderr)
	}
	if stdout.String() != c.child.stdout {
		t.Errorf("stdout = %q, want %q", stdout.String(), c.child.stdout)
	}
}

const (
	authorization  = "sudo authorization could not be obtained; authenticate to sudo or run Bootwright as root"
	passwordNeeded = "sudo: a password is required\n"
	hostWarning    = "sudo: unable to resolve host lab-01: Name or service not known\n"
	stateFailure   = "[FAIL] lifecycle.state: the context holds no applied revision\n"
	denial         = "Sorry, user operator is not allowed to execute '/proc/4242/exe status' as root on lab-01.\n"
)

// Sudo exits 1 for its own refusals and writes them on the child's standard
// error, so each row separates a refusal from a child that ran by the start
// announcement alone. Every report the supervisor writes exits 1.
func TestElevationOutcomes(t *testing.T) {
	failed := errors.New("sudo could not be waited for")
	for _, c := range []elevationCase{
		{name: "a run error after a result keeps the child's failure", child: scriptedChild{stdout: "partial\n", code: 3, err: failed}, exit: 3},
		{name: "a run error after a result never exits 0", child: scriptedChild{stdout: "partial\n", err: failed}, exit: 1},
		{name: "a run error before any result", child: scriptedChild{code: 1, err: failed}, exit: 1, code: "runtime.privilege", message: "sudo invocation failed"},
		{name: "success stands", json: true, child: scriptedChild{stdout: "{}\n", stderr: []string{startAnnouncement}}, stderr: ""},
		{name: "a child's result stands whatever its status", json: true, child: scriptedChild{stdout: "{}\n", stderr: []string{startAnnouncement}, code: 1}, exit: 1},
		{name: "success forwards a held warning of a child that never announced", child: scriptedChild{stderr: []string{hostWarning}}, stderr: hostWarning},
		{name: "an interactive failure stands", terminal: true, child: scriptedChild{stderr: []string{startAnnouncement, stateFailure}, code: 1}, exit: 1, stderr: stateFailure},
		{name: "a JSON child that started and was killed", json: true, child: scriptedChild{stderr: []string{startAnnouncement}, code: 137}, exit: 1, code: "runtime.internal", message: "the elevated command exited with status 137 without a result"},
		{name: "a JSON child that started and panicked", json: true, child: scriptedChild{stderr: []string{startAnnouncement, "panic: boom\n"}, code: 2}, exit: 1, code: "runtime.internal", message: "the elevated command exited with status 2 without a result", stderr: "panic: boom\n"},
		{name: "a JSON refusal", json: true, child: scriptedChild{stderr: []string{passwordNeeded}, code: 1}, exit: 1, code: "runtime.privilege", message: authorization},
		{name: "a JSON policy denial leaves standard error empty", json: true, child: scriptedChild{stderr: []string{hostWarning, denial}, code: 1}, exit: 1, code: "runtime.privilege", message: authorization},
		{name: "a warning before a child's own failure", child: scriptedChild{stderr: []string{hostWarning, startAnnouncement, stateFailure}, code: 1}, exit: 1, stderr: hostWarning + stateFailure},
		{name: "a warning before an unannounced child's own failure", child: scriptedChild{stderr: []string{hostWarning, stateFailure}, code: 1}, exit: 1, stderr: hostWarning + stateFailure},
		{name: "a relayed remote refusal", child: scriptedChild{stderr: []string{startAnnouncement, passwordNeeded}, code: 1}, exit: 1, stderr: passwordNeeded},
		{name: "a human refusal", child: scriptedChild{stderr: []string{hostWarning, passwordNeeded}, code: 1}, exit: 1, code: "runtime.privilege", message: authorization},
		{name: "a policy denial speaks for itself", child: scriptedChild{stderr: []string{denial}, code: 1}, exit: 1, stderr: denial},
		{name: "held lines reach an unexpected status", child: scriptedChild{stderr: []string{passwordNeeded}, code: 2}, exit: 2, stderr: passwordNeeded},
		{name: "a silent failure keeps its status", child: scriptedChild{code: 1}, exit: 1},
	} {
		t.Run(c.name, func(t *testing.T) { c.check(t, context.Background()) })
	}
}

// The supervisor's own refresh warning reaches a human invocation's standard
// error and never a JSON one's, which stays empty around the child's document.
// The refresh fails once the child announced, and the child ends only after
// the refresh has warned.
func TestARefreshWarningNeverReachesJSONStandardError(t *testing.T) {
	const warning = "[WARN] sudo credential refresh stopped; the active operation continues.\n"
	for _, test := range []struct {
		name           string
		json           bool
		stdout, stderr string
	}{
		{name: "JSON", json: true, stdout: `{"exitCode":0}` + "\n"},
		{name: "human", stdout: "[OK] done\n", stderr: warning},
	} {
		t.Run(test.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				announced := make(chan struct{})
				elevator := scriptedElevator(scriptedChild{}, nil)
				elevator.Delay = delayFunc(func(ctx context.Context, _ time.Duration) error {
					select {
					case <-announced:
						return nil
					case <-ctx.Done():
						return ctx.Err()
					}
				})
				elevator.Executor = executorFunc(func(_ context.Context, c Command) (int, error) {
					if slices.Equal(c.Arguments, []string{"-n", "-u", "#0", "-ll"}) || slices.Equal(c.Arguments, []string{"-n", "-u", "#0", "-v"}) {
						return 1, nil
					}
					io.WriteString(c.Error, startAnnouncement)
					close(announced)
					synctest.Wait()
					io.WriteString(c.Output, test.stdout)
					return 0, nil
				})
				var stdout, stderr bytes.Buffer
				outcome := elevator.Run(context.Background(), Invocation{JSON: test.json, Arguments: []string{"status"}, Output: &stdout, Error: &stderr})
				if outcome != (Outcome{}) || stdout.String() != test.stdout || stderr.String() != test.stderr {
					t.Fatalf("outcome %+v, stdout %q, stderr %q; want stdout %q, stderr %q", outcome, stdout.String(), stderr.String(), test.stdout, test.stderr)
				}
			})
		})
	}
}

func TestElevationHandsTerminalsOnlyToAnInteractiveInvocation(t *testing.T) {
	outputRead, outputTerminal, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer outputRead.Close()
	defer outputTerminal.Close()
	errorRead, errorTerminal, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer errorRead.Close()
	defer errorTerminal.Close()
	for _, test := range []struct {
		name                             string
		json, inputTerminal, interactive bool
	}{
		{name: "interactive", inputTerminal: true, interactive: true},
		{name: "piped input", inputTerminal: false},
		{name: "JSON on a terminal", json: true, inputTerminal: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			var commands []Command
			input := strings.NewReader("payload")
			var stdout, stderr bytes.Buffer
			invocation := Invocation{
				JSON: test.json, InputTerminal: test.inputTerminal, Arguments: []string{"apply"}, Terminal: "xterm-256color",
				Input: input, Output: &stdout, Error: &stderr, OutputTerminal: outputTerminal, ErrorTerminal: errorTerminal,
			}
			if outcome := scriptedElevator(scriptedChild{}, &commands).Run(context.Background(), invocation); outcome != (Outcome{}) {
				t.Fatalf("outcome = %+v", outcome)
			}
			if len(commands) != 1 {
				t.Fatalf("commands = %d", len(commands))
			}
			child := commands[0]
			handed := child.Output == io.Writer(outputTerminal) && child.Error == io.Writer(errorTerminal)
			if handed != test.interactive {
				t.Errorf("terminals handed = %v (output %T, error %T), want %v", handed, child.Output, child.Error, test.interactive)
			}
			if child.Input != io.Reader(input) {
				t.Errorf("stdin reached the child as %T", child.Input)
			}
			if noninteractive := slices.Contains(child.Arguments, "-n"); noninteractive == test.interactive {
				t.Errorf("arguments %q, want noninteractive %v", child.Arguments, !test.interactive)
			}
			if forwarded := slices.Contains(child.Environment, "TERM=xterm-256color"); forwarded != test.interactive {
				t.Errorf("environment %q, want TERM forwarded %v", child.Environment, test.interactive)
			}
		})
	}
}

func TestElevationForwardsTheRouteOnlyForAProxy(t *testing.T) {
	proxy, refusal := AmbientRoute(true, environment(map[string]string{"HTTPS_PROXY": "http://proxy.example:3128", "NO_PROXY": "images.internal"}))
	if refusal != nil {
		t.Fatal(*refusal)
	}
	direct, refusal := AmbientRoute(true, environment(nil))
	if refusal != nil {
		t.Fatal(*refusal)
	}
	for _, test := range []struct {
		name  string
		route controller.Route
		want  []string
	}{
		{"proxy", proxy, []string{"-u", "#0", "-n", "HTTPS_PROXY=http://proxy.example:3128", "NO_PROXY=images.internal", "--", "/proc/4242/exe", "media", "add"}},
		{"direct", direct, []string{"-u", "#0", "-n", "--", "/proc/4242/exe", "media", "add"}},
		{"unselected", controller.Route{}, []string{"-u", "#0", "-n", "--", "/proc/4242/exe", "media", "add"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			var commands []Command
			var stdout, stderr bytes.Buffer
			invocation := Invocation{Arguments: []string{"media", "add"}, Route: test.route, Output: &stdout, Error: &stderr}
			scriptedElevator(scriptedChild{}, &commands).Run(context.Background(), invocation)
			if len(commands) != 1 || !slices.Equal(commands[0].Arguments, test.want) {
				t.Fatalf("sudo arguments = %v, want %q", commands, test.want)
			}
		})
	}
}

func TestElevationRefusesBeforeSudoWhenTheExecutableOrSudoIsUnverified(t *testing.T) {
	unverified := func() (string, error) { return "", errors.New("unverified") }
	for _, test := range []struct {
		name     string
		elevator func(Elevator) Elevator
		message  string
	}{
		{"executable", func(e Elevator) Elevator { e.Executable = unverified; return e }, "invocation executable cannot be verified"},
		{"sudo", func(e Elevator) Elevator { e.Sudo = unverified; return e }, "sudo is unavailable; run Bootwright as root"},
	} {
		t.Run(test.name, func(t *testing.T) {
			elevator := scriptedElevator(scriptedChild{}, nil)
			elevator.Executor = executorFunc(func(context.Context, Command) (int, error) {
				t.Fatal("sudo ran")
				return 0, nil
			})
			var stdout, stderr bytes.Buffer
			outcome := test.elevator(elevator).Run(context.Background(), Invocation{Arguments: []string{"status"}, Output: &stdout, Error: &stderr})
			want := diagnostics.Diagnostic{Severity: "error", Code: "runtime.privilege", Message: test.message}
			if outcome.ExitCode != 1 || outcome.Diagnostic == nil || *outcome.Diagnostic != want {
				t.Fatalf("outcome = %+v", outcome)
			}
			if stdout.Len() != 0 || stderr.Len() != 0 {
				t.Fatalf("stdout %q, stderr %q", stdout.String(), stderr.String())
			}
		})
	}
}

type accountFunc func(context.Context) (Account, error)

func (f accountFunc) Resolve(ctx context.Context) (Account, error) { return f(ctx) }

func TestAdmissionRefusesAnUnverifiableAccountOrSudoParent(t *testing.T) {
	for _, test := range []struct {
		name     string
		account  Account
		resolve  error
		guard    error
		guarded  bool
		released bool
		message  string
	}{
		{name: "unverifiable account", resolve: errAccount, message: "invoking account cannot be verified"},
		{name: "direct invocation", account: Account{UID: 1000}},
		{name: "unguardable sudo parent", account: Account{SudoParentPID: 4242}, guard: errors.New("parent changed"), guarded: true, message: "sudo parent lifetime cannot be guarded"},
		{name: "guarded sudo parent", account: Account{SudoParentPID: 4242}, guarded: true, released: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			var guarded, released bool
			guard := func(pid int) (func(), error) {
				if pid != 4242 {
					t.Fatalf("guarded %d", pid)
				}
				guarded = true
				if test.guard != nil {
					return nil, test.guard
				}
				return func() { released = true }, nil
			}
			accounts := accountFunc(func(context.Context) (Account, error) { return test.account, test.resolve })
			release, refusal := Admit(context.Background(), accounts, guard)
			if guarded != test.guarded {
				t.Fatalf("guarded = %v, want %v", guarded, test.guarded)
			}
			var message string
			if refusal != nil {
				if refusal.Code != "runtime.privilege" || refusal.Severity != "error" {
					t.Fatalf("refusal = %+v", *refusal)
				}
				message = refusal.Message
			}
			if message != test.message {
				t.Fatalf("refusal %q, want %q", message, test.message)
			}
			release()
			if released != test.released {
				t.Fatalf("released = %v, want %v", released, test.released)
			}
		})
	}
}

func environment(values map[string]string) func(string) (string, bool) {
	return func(name string) (string, bool) {
		value, found := values[name]
		return value, found
	}
}

func TestAmbientRouteRefusesOnlyASelectedUnqualifiedRoute(t *testing.T) {
	httpOnly := environment(map[string]string{"HTTP_PROXY": "http://proxy.example:3128"})
	if route, refusal := AmbientRoute(false, httpOnly); refusal != nil || route.Configured() {
		t.Fatalf("an unselected route = %+v, %+v", route, refusal)
	}
	_, refusal := AmbientRoute(true, httpOnly)
	if refusal == nil || refusal.Code != "controller.unsupported" || refusal.Message != "every dependency source is HTTPS, so HTTP_PROXY alone selects no acquisition route" || refusal.Remediation == "" {
		t.Fatalf("refusal = %+v", refusal)
	}
	route, refusal := AmbientRoute(true, environment(map[string]string{"HTTPS_PROXY": "http://proxy.example:3128"}))
	if refusal != nil || route.HTTPSProxy() != "http://proxy.example:3128" {
		t.Fatalf("proxy route = %+v, %+v", route, refusal)
	}
	route, refusal = AmbientRoute(true, environment(nil))
	if refusal != nil || !route.Direct() {
		t.Fatalf("direct route = %+v, %+v", route, refusal)
	}
}

type filterCase struct {
	name, mode string
	writes     []string
	want       string
	started    bool
	held       string
}

func filterFor(mode string, out io.Writer) *startFilter {
	switch mode {
	case "json":
		return newStartFilter(out, true, true)
	case "human":
		return newStartFilter(out, false, true)
	}
	return newStartFilter(out, false, false)
}

func (c filterCase) check(t *testing.T) {
	t.Helper()
	var out bytes.Buffer
	filter := filterFor(c.mode, &out)
	for _, write := range c.writes {
		if n, err := filter.Write([]byte(write)); err != nil || n != len(write) {
			t.Fatalf("write %q = %d, %v", write, n, err)
		}
	}
	if err := filter.Close(); err != nil {
		t.Fatalf("close = %v", err)
	}
	if out.String() != c.want {
		t.Errorf("forwarded %q, want %q", out.String(), c.want)
	}
	if filter.started != c.started {
		t.Errorf("started = %v, want %v", filter.started, c.started)
	}
	if string(filter.held) != c.held {
		t.Errorf("held %q, want %q", filter.held, c.held)
	}
	if len(filter.line) != 0 {
		t.Errorf("an undecided line of %d bytes survived Close", len(filter.line))
	}
}

// An explicit sensitive result, such as an exported kubeconfig, is the child's
// exact bytes: behind a pipe they cross the counting relay and the writer that
// serializes the child's streams, which must neither add a final LF nor
// translate a NUL or a CR, however the child splits its writes.
func TestElevationRelaysRawOutputUnchanged(t *testing.T) {
	chunks := []string{"apiVersion: v1\r\n", "data: \x00\x01", "\r", "\nkind: Config"}
	var relayed []string
	executor := executorFunc(func(_ context.Context, c Command) (int, error) {
		if slices.Equal(c.Arguments, []string{"-n", "-u", "#0", "-ll"}) {
			return 1, nil
		}
		for _, chunk := range chunks {
			n, err := io.WriteString(c.Output, chunk)
			if err != nil || n != len(chunk) {
				return 1, err
			}
			relayed = append(relayed, chunk)
		}
		return 0, nil
	})
	elevator := Elevator{
		Executable: func() (string, error) { return "/proc/4242/exe", nil },
		Sudo:       func() (string, error) { return "/usr/bin/sudo", nil },
		Executor:   executor,
		Delay:      delayFunc(func(ctx context.Context, _ time.Duration) error { <-ctx.Done(); return ctx.Err() }),
	}
	var stdout, stderr bytes.Buffer
	outcome := elevator.Run(context.Background(), Invocation{Arguments: []string{"cluster", "kubeconfig", "--name", "sno"}, Output: &stdout, Error: &stderr})
	if outcome.ExitCode != 0 || outcome.Diagnostic != nil {
		t.Fatalf("outcome = %+v", outcome)
	}
	if want := strings.Join(chunks, ""); stdout.String() != want || stderr.Len() != 0 || len(relayed) != len(chunks) {
		t.Fatalf("relayed %q with standard error %q, want exactly %q", stdout.String(), stderr.String(), want)
	}
}

func TestStartFilterStripsOneAnnouncementAndPassesTheRestUnchanged(t *testing.T) {
	long := strings.Repeat("x", 5000) + "\n"
	for _, c := range []filterCase{
		{name: "split writes", mode: "human", writes: []string{"sudo: war", "ning\nbootwr", "ight: elevated command st", "arted\n[FAIL] cli", ".usage: bad\n"}, want: "sudo: warning\n[FAIL] cli.usage: bad\n", started: true},
		{name: "later sudo and announcement lines pass", mode: "json", writes: []string{startAnnouncement + passwordNeeded + startAnnouncement}, want: passwordNeeded + startAnnouncement, started: true},
		{name: "an interactive announcement", mode: "interactive", writes: []string{startAnnouncement, stateFailure}, want: stateFailure, started: true},
		{name: "a long line before the start", mode: "human", writes: []string{long}, want: long},
		{name: "a long sudo line before the start", mode: "human", writes: []string{"sudo: " + long}, want: "sudo: " + long},
		{name: "a long JSON sudo line", mode: "json", writes: []string{"sudo: " + long}},
		{name: "a long line after the start", mode: "json", writes: []string{startAnnouncement, long}, want: long, started: true},
		{name: "JSON drops a refusal before a line", mode: "json", writes: []string{passwordNeeded + stateFailure}},
		{name: "JSON drops a refusal after a line", mode: "json", writes: []string{stateFailure + passwordNeeded}},
		{name: "JSON drops every line before the start", mode: "json", writes: []string{denial[:20], denial[20:] + hostWarning, long, startAnnouncement, stateFailure}, want: stateFailure, started: true},
		{name: "JSON drops a line that only began like the announcement", mode: "json", writes: []string{"bootwright: elevated", " command failed\n", startAnnouncement}, started: true},
		{name: "an unterminated JSON refusal", mode: "json", writes: []string{"sudo: a password is required"}},
		{name: "an unterminated human refusal", mode: "human", writes: []string{"sudo: a password is required"}, held: "sudo: a password is required"},
		{name: "an unterminated fragment", mode: "json", writes: []string{"sudo"}},
		{name: "an unterminated human fragment", mode: "human", writes: []string{"sudo"}, want: "sudo"},
		{name: "an unterminated announcement", mode: "human", writes: []string{hostWarning, strings.TrimSuffix(startAnnouncement, "\n")}, want: hostWarning, started: true},
		{name: "an unterminated diagnostic", mode: "human", writes: []string{"[FAIL] cli.usage: bad"}, want: "[FAIL] cli.usage: bad"},
	} {
		t.Run(c.name, c.check)
	}
	const prompt = "[sudo] password for operator: "
	for mode, want := range map[string]string{"interactive": prompt, "human": prompt, "json": ""} {
		t.Run("a prompt is decided at once in "+mode, func(t *testing.T) {
			var out bytes.Buffer
			filter := filterFor(mode, &out)
			if _, err := filter.Write([]byte(prompt)); err != nil || out.String() != want || len(filter.line) != 0 {
				t.Fatalf("forwarded %q, %v with %d bytes undecided before any further write, want %q", out.String(), err, len(filter.line), want)
			}
		})
	}
}

func TestStartFilterBoundsWhatItHolds(t *testing.T) {
	refusals := strings.Repeat(passwordNeeded, heldLineCount)
	longest := "sudo: " + strings.Repeat("x", heldLineBytes-len("sudo: ")-1) + "\n"
	tooLong := "sudo: " + strings.Repeat("x", heldLineBytes-len("sudo: ")) + "\n"
	for _, c := range []filterCase{
		{name: "sixteen lines are held", mode: "human", writes: []string{refusals}, held: refusals},
		{name: "a seventeenth forwards them all", mode: "human", writes: []string{refusals, hostWarning, passwordNeeded}, want: refusals + hostWarning + passwordNeeded},
		{name: "a line of the bound is held", mode: "human", writes: []string{longest}, held: longest},
		{name: "a longer line passes whole after the held ones", mode: "human", writes: []string{passwordNeeded, tooLong, passwordNeeded}, want: passwordNeeded + tooLong + passwordNeeded},
		{name: "JSON drops without holding", mode: "json", writes: []string{refusals, passwordNeeded}},
	} {
		t.Run(c.name, c.check)
	}
}

func TestElevatedStderrWithholdsOnlySudoRefusals(t *testing.T) {
	for _, c := range []filterCase{
		{name: "interactive keeps the prompt", mode: "interactive", writes: []string{"[sudo] password for operator: "}, want: "[sudo] password for operator: "},
		{name: "withheld refusal", mode: "human", writes: []string{passwordNeeded}, held: passwordNeeded},
		{name: "product diagnostic survives", mode: "human", writes: []string{"[FAIL] context.state: context store is missing registry.json\n"}, want: "[FAIL] context.state: context store is missing registry.json\n"},
		{name: "refresh warning survives", mode: "human", writes: []string{"[WARN] sudo credential refresh stopped; the active operation continues.\n"}, want: "[WARN] sudo credential refresh stopped; the active operation continues.\n"},
		{name: "split writes rejoin one line", mode: "human", writes: []string{"sudo", ": a password", " is required\n"}, held: passwordNeeded},
		{name: "refusal before a diagnostic", mode: "human", writes: []string{passwordNeeded + "[FAIL] runtime.privilege: denied\n"}, want: passwordNeeded + "[FAIL] runtime.privilege: denied\n"},
		{name: "unterminated diagnostic flushes on close", mode: "human", writes: []string{"[FAIL] cli.usage: bad"}, want: "[FAIL] cli.usage: bad"},
	} {
		t.Run(c.name, c.check)
	}
}

func TestElevatedStderrBoundsOneWithheldLine(t *testing.T) {
	var out bytes.Buffer
	filter := filterFor("human", &out)
	overflow := "sudo: " + strings.Repeat("x", 2*heldLineBytes)
	if _, err := filter.Write([]byte(overflow)); err != nil {
		t.Fatalf("write = %v", err)
	}
	if len(filter.line) != 0 || len(filter.held) != 0 {
		t.Fatalf("retained %d undecided and %d held bytes", len(filter.line), len(filter.held))
	}
	if err := filter.Close(); err != nil {
		t.Fatalf("close = %v", err)
	}
	if out.String() != overflow {
		t.Fatalf("stderr = %d bytes of %d", out.Len(), len(overflow))
	}
}

func TestAnnouncementReachesOnlyAPipeOfASupervisedChild(t *testing.T) {
	if !strings.HasPrefix(startAnnouncement, "bootwright: ") || strings.HasPrefix(startAnnouncement, sudoLinePrefix) || strings.Count(startAnnouncement, "\n") != 1 || !strings.HasSuffix(startAnnouncement, "\n") {
		t.Fatalf("announcement %q", startAnnouncement)
	}
	for _, c := range strings.TrimSuffix(startAnnouncement, "\n") {
		if c < ' ' || c > '~' {
			t.Fatalf("announcement %q is not printable ASCII", startAnnouncement)
		}
	}
	for _, terminal := range []bool{false, true} {
		for _, root := range []bool{false, true} {
			for _, supervised := range []bool{false, true} {
				var out bytes.Buffer
				announceStart(&out, terminal, root, supervised)
				want := ""
				if !terminal && root && supervised {
					want = startAnnouncement
				}
				if out.String() != want {
					t.Errorf("terminal %v, root %v, supervised %v: wrote %q, want %q", terminal, root, supervised, out.String(), want)
				}
			}
		}
	}
}
