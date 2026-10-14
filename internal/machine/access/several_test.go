package access

import (
	"bytes"
	"context"
	"errors"
	"io"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/trust"
)

func execNames(t *testing.T, h *harness, names []string, options machine.SSHOptions, words ...string) (*machine.SessionResult, error) {
	t.Helper()
	return h.service.Exec(context.Background(), ExecRequest{Names: names, SSH: options, Command: words})
}

// sessionsByMachine indexes what the client was asked to run, so a test does
// not depend on the order concurrent sessions reached it.
func sessionsByMachine(c *client) map[string]int {
	index := map[string]int{}
	for i, session := range c.sessions {
		index[session.Machine] = i
	}
	return index
}

// Names that collapse to one Machine are the single session it always was:
// the operator's own streams reach the client and its status is the result.
func TestOneMemberIsTheSingleSessionUnchanged(t *testing.T) {
	h := newHarness(t, nil)
	in, out := strings.NewReader("input"), &bytes.Buffer{}
	h.service.options.Streams = Streams{In: in, Out: out, Err: h.errStream}
	h.client.code = 7
	result, err := execNames(t, h, []string{"", " guest ", "guest"}, machine.SSHOptions{}, "false")
	if err != nil {
		t.Fatal(err)
	}
	if result.ExitCode != 7 || result.Machine != "guest" || result.Address != "192.0.2.10" || result.Sessions != nil {
		t.Fatalf("result = %+v", result)
	}
	if len(h.client.sessions) != 1 || h.client.sessions[0].Batch {
		t.Fatalf("sessions = %+v", h.client.sessions)
	}
	if h.client.inputs[0] != io.Reader(in) || h.client.outputs[0][0] != io.Writer(out) ||
		h.client.outputs[0][1] != io.Writer(h.errStream) {
		t.Fatal("the single session was not handed the operator's own streams")
	}
	if h.errStream.Len() != 0 {
		t.Fatalf("the single session reported its own status: %q", h.errStream.String())
	}
}

func TestNamesThatResolveToNoMemberAreAUsageRefusal(t *testing.T) {
	h := newHarness(t, nil)
	if _, err := execNames(t, h, []string{"", "  "}, machine.SSHOptions{}, "true"); code(t, err) != "cli.usage" {
		t.Fatalf("code = %q", code(t, err))
	}
	if h.client.proofs != 0 || h.lender.lends != 0 || len(h.client.sessions) != 0 {
		t.Fatal("a nameless invocation reached the store or the host")
	}
}

// Every session opens before any ends, and none may read the terminal or
// prompt on it.
func TestSeveralSessionsRunConcurrentlyAndNonInteractively(t *testing.T) {
	h := newHarness(t, nil)
	h.service.options.Streams.In = strings.NewReader("input")
	var started sync.WaitGroup
	started.Add(3)
	all := make(chan struct{})
	go func() { started.Wait(); close(all) }()
	h.client.run = func(machine.Session, io.Writer, io.Writer) (int, error) {
		started.Done()
		select {
		case <-all:
			return 0, nil
		case <-time.After(10 * time.Second):
			return 0, errors.New("the sessions did not run together")
		}
	}
	result, err := execNames(t, h, []string{"host", "guest", "declared"}, machine.SSHOptions{}, "uptime")
	if err != nil {
		t.Fatal(err)
	}
	if result.ExitCode != 0 || len(result.Sessions) != 3 {
		t.Fatalf("result = %+v, stderr %q", result, h.errStream.String())
	}
	for i, session := range h.client.sessions {
		if !session.Batch || h.client.inputs[i] != nil || strings.Join(session.Command, " ") != "uptime" {
			t.Fatalf("session %s batch=%v input=%v command=%v", session.Machine, session.Batch, h.client.inputs[i], session.Command)
		}
	}
}

// One lend opens each declaration the sessions need once, each session
// carries its own credential, and every copy is cleared once all have ended.
func TestSeveralSessionsShareOneLendAndCarryTheirOwnCredentials(t *testing.T) {
	h := newHarness(t, nil)
	if _, err := execNames(t, h, []string{"keyed", "guest", "declared"}, machine.SSHOptions{}, "id"); err != nil {
		t.Fatal(err)
	}
	if h.lender.lends != 1 || strings.Join(h.lender.requested, ",") != "admin-key,declared-host-key,fleet" {
		t.Fatalf("lends = %d opened %v", h.lender.lends, h.lender.requested)
	}
	index := sessionsByMachine(h.client)
	for name, want := range map[string]string{"declared": "ADMIN KEY", "guest": "FLEET KEY", "keyed": "ADMIN KEY"} {
		if got := h.client.keys[index[name]]; got != want {
			t.Fatalf("%s carried %q, want %q", name, got, want)
		}
		if key := h.client.sessions[index[name]].PrivateKey; strings.Trim(string(key), "\x00") != "" {
			t.Fatalf("%s kept its credential after the sessions ended", name)
		}
	}
}

func TestEveryUnknownMemberIsNamedInOneRefusal(t *testing.T) {
	h := newHarness(t, nil)
	_, err := execNames(t, h, []string{"zeta", "guest", "alpha", "zeta"}, machine.SSHOptions{}, "true")
	reported := diagnostics.Of(err)
	if len(reported) != 1 || reported[0].Code != "access.target" ||
		reported[0].Message != "the selected context declares no Machines named alpha, zeta" ||
		reported[0].Remediation != "list the Machines this context selects with bootwright machine list --context lab" {
		t.Fatalf("refusal = %+v", reported)
	}
	if h.client.proofs != 0 || h.lender.lends != 0 || len(h.client.sessions) != 0 {
		t.Fatal("an unknown member let the others reach the store or the host")
	}
}

// A refusal of any one Machine, reported for the first refusing Machine in
// canonical order, opens no session on any of them.
func TestARefusalForAnyMemberOpensNoSession(t *testing.T) {
	for _, test := range []struct {
		name    string
		names   []string
		options machine.SSHOptions
		adjust  func(*harness)
		code    string
		want    string
	}{
		{name: "reached locally", names: []string{"secured", "host", "controller"},
			code: "access.unavailable", want: "Machine/controller"},
		{name: "password", names: []string{"secured", "peer"},
			code: "access.unavailable", want: "--name secured -- <command> on it alone"},
		{name: "password with an offered key", names: []string{"secured", "peer"},
			options: machine.SSHOptions{IdentityFile: "/keys/mine"},
			code:    "access.unavailable", want: "Machine/secured authenticates with a password"},
		{name: "unproved host key", names: []string{"host", "declared"},
			adjust: func(h *harness) { h.service.options.Terminal = func() (bool, error) { return false, nil } },
			code:   "trust.identity", want: "--machines host"},
		{name: "unowned installation", names: []string{"peer", "guest"},
			adjust: func(h *harness) { h.service.options.Ownership = owner{realized: false} },
			code:   "access.target", want: "Machine/guest"},
	} {
		t.Run(test.name, func(t *testing.T) {
			h := newHarness(t, nil)
			if test.adjust != nil {
				test.adjust(h)
			}
			_, err := execNames(t, h, test.names, test.options, "true")
			reported := diagnostics.Of(err)
			if len(reported) != 1 || reported[0].Code != test.code ||
				!strings.Contains(reported[0].Message+" "+reported[0].Remediation, test.want) {
				t.Fatalf("refusal = %+v (%v)", reported, err)
			}
			if len(h.client.sessions) != 0 || len(h.prompt.asked) != 0 || len(h.trust.written) != 0 {
				t.Fatalf("opened %d, asked %d, wrote %d", len(h.client.sessions), len(h.prompt.asked), len(h.trust.written))
			}
			if test.code == "access.unavailable" && h.lender.lends != 0 {
				t.Fatal("a resolution refusal opened material")
			}
		})
	}
}

// Host keys are proved one Machine at a time in canonical order before any
// session opens, and each first-use record is published against the records
// the previous confirmation left.
func TestHostKeysAreProvedOneAtATimeBeforeAnySessionOpens(t *testing.T) {
	h := newHarness(t, nil)
	var mu sync.Mutex
	var seen []int
	h.client.run = func(machine.Session, io.Writer, io.Writer) (int, error) {
		mu.Lock()
		defer mu.Unlock()
		seen = append(seen, len(h.prompt.asked), len(h.trust.written))
		return 0, nil
	}
	if _, err := execNames(t, h, []string{"peer", "host"}, machine.SSHOptions{}, "true"); err != nil {
		t.Fatal(err)
	}
	if len(h.prompt.asked) != 2 || !strings.HasPrefix(h.prompt.asked[0], "host ") || !strings.HasPrefix(h.prompt.asked[1], "peer ") {
		t.Fatalf("asked = %v", h.prompt.asked)
	}
	if !slices.Equal(seen, []int{2, 2, 2, 2}) {
		t.Fatalf("sessions saw (asked, written) = %v, want every proof first", seen)
	}
	records, err := trust.Decode(h.trust.data)
	if err != nil {
		t.Fatal(err)
	}
	if _, found := records.Find("host"); !found {
		t.Fatal("the second confirmation lost the first record")
	}
	if _, found := records.Find("peer"); !found {
		t.Fatal("the second confirmation was not recorded")
	}
}

// interrupting answers each confirmation and interrupts the invocation once
// the last one is answered, as an operator pressing Ctrl-C right after it.
type interrupting struct {
	*prompt
	cancel context.CancelFunc
	after  int
}

func (p interrupting) ConfirmHostKey(ctx context.Context, contextName, name, address, keyType, fingerprint string) error {
	err := p.prompt.ConfirmHostKey(ctx, contextName, name, address, keyType, fingerprint)
	if len(p.asked) == p.after {
		p.cancel()
	}
	return err
}

// An interrupt that lands once every host key is proved opens no session and
// is reported as the interrupt, never as clients that could not start.
func TestAnInterruptBeforeTheSessionsOpenStartsNone(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	asked := &prompt{}
	h := newHarness(t, func(options *Options) { options.Confirmer = interrupting{prompt: asked, cancel: cancel, after: 2} })
	_, err := h.service.Exec(ctx, ExecRequest{Names: []string{"peer", "host"}, Command: []string{"true"}})
	if !errors.Is(err, context.Canceled) || len(h.client.sessions) != 0 || strings.Contains(h.errStream.String(), "access.exit") {
		t.Fatalf("err = %v, sessions %d, stderr %q", err, len(h.client.sessions), h.errStream.String())
	}
}

// The invocation exits with the highest status any session returned, and
// names every session that did not exit zero in canonical order once all
// have ended.
func TestSessionStatusesAggregateWithWarningsInCanonicalOrder(t *testing.T) {
	h := newHarness(t, nil)
	codes := map[string]int{"declared": 0, "guest": 3, "peer": 1}
	h.client.run = func(session machine.Session, _ io.Writer, _ io.Writer) (int, error) {
		return codes[session.Machine], nil
	}
	result, err := execNames(t, h, []string{"peer", "guest", "declared"}, machine.SSHOptions{}, "true")
	if err != nil {
		t.Fatal(err)
	}
	if result.ExitCode != 3 || result.Context != "lab" || len(result.Sessions) != 3 {
		t.Fatalf("result = %+v", result)
	}
	for i, want := range []string{"declared", "guest", "peer"} {
		if got := result.Sessions[i]; got.Machine != want || got.ExitCode != codes[want] || got.Context != "lab" {
			t.Fatalf("session %d = %+v, want %s", i, got, want)
		}
	}
	want := "[WARN] access.exit: guest exited with status 3\n[WARN] access.exit: peer exited with status 1\n"
	if h.errStream.String() != want {
		t.Fatalf("stderr = %q, want %q", h.errStream.String(), want)
	}
}

// A client that could not start counts as the SSH client's failure status,
// and its warning carries the reason as one printable line.
func TestASessionThatCouldNotStartCountsAs255(t *testing.T) {
	h := newHarness(t, nil)
	h.client.run = func(session machine.Session, _ io.Writer, _ io.Writer) (int, error) {
		if session.Machine == "guest" {
			return 0, errors.New("client\nmissing\x1b[2J")
		}
		return 4, nil
	}
	result, err := execNames(t, h, []string{"peer", "guest"}, machine.SSHOptions{}, "true")
	if err != nil {
		t.Fatal(err)
	}
	if result.ExitCode != 255 || result.Sessions[0].ExitCode != 255 || result.Sessions[1].ExitCode != 4 {
		t.Fatalf("result = %+v", result)
	}
	want := "[WARN] access.exit: guest could not start (status 255): client?missing?[2J\n" +
		"[WARN] access.exit: peer exited with status 4\n"
	if h.errStream.String() != want {
		t.Fatalf("stderr = %q, want %q", h.errStream.String(), want)
	}
}

// Each session's output and errors reach the operator's matching stream as
// whole lines under that Machine's name, a final unterminated line included.
func TestSeveralSessionsWritePrefixedLinesToTheirStreams(t *testing.T) {
	h := newHarness(t, nil)
	out := &bytes.Buffer{}
	h.service.options.Streams.Out = out
	h.client.run = func(session machine.Session, stdout, stderr io.Writer) (int, error) {
		for _, chunk := range []string{"one\ntw", "o\n", "last"} {
			if _, err := io.WriteString(stdout, chunk); err != nil {
				return 0, err
			}
		}
		_, err := io.WriteString(stderr, "warned "+session.Machine+"\n")
		return 0, err
	}
	if _, err := execNames(t, h, []string{"peer", "guest"}, machine.SSHOptions{}, "true"); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"guest", "peer"} {
		var lines []string
		for _, line := range strings.Split(out.String(), "\n") {
			if strings.HasPrefix(line, name+": ") {
				lines = append(lines, line)
			}
		}
		if !slices.Equal(lines, []string{name + ": one", name + ": two", name + ": last"}) {
			t.Fatalf("%s wrote %q", name, out.String())
		}
		if !strings.Contains(h.errStream.String(), name+": warned "+name+"\n") {
			t.Fatalf("stderr = %q", h.errStream.String())
		}
	}
	if strings.Count(out.String(), "\n") != 6 {
		t.Fatalf("stdout = %q", out.String())
	}
}
