package account

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"errors"
	"io"
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets"
	"golang.org/x/crypto/ssh"
)

type sshRequest struct {
	user     string
	kind     machine.IdentityKind
	password string
	command  []string
	input    string
	terminal bool
}

type sshFixture struct {
	received []sshRequest
	results  []string
}

func (f *sshFixture) Run(ctx context.Context, session machine.Session, in io.Reader, out, stderr io.Writer) (int, error) {
	if len(f.results) == 0 || !session.HostKey.Present() {
		return 255, nil
	}
	result := f.results[0]
	f.results = f.results[1:]
	request := sshRequest{session.User, session.Kind, string(session.Password), session.Command, "", session.ForceTTY}
	if result == "tty-refused" {
		f.received = append(f.received, request)
		_, _ = io.WriteString(stderr, "fixture-private banner\nsudo: sorry, you must have a tty to run sudo\n")
		return 1, nil
	}
	if result == "no-tty" {
		f.received = append(f.received, request)
		_, _ = io.WriteString(out, session.Command[4]+"-denied\r\n")
		return 1, nil
	}
	if result == "ssh-refused" {
		f.received = append(f.received, request)
		return 255, nil
	}
	if session.ForceTTY {
		_, _ = io.WriteString(out, "login notice\n"+session.Command[4]+"\n")
		stderr = out
		if result == "conflicting-tty" {
			f.received = append(f.received, request)
			_, _ = io.WriteString(out, session.Command[4]+"-denied\n")
			return 1, nil
		}
	}
	if slices.Contains(session.Command, sudoChallenge) {
		_, _ = io.WriteString(stderr, sudoChallenge)
	}
	if in != nil {
		input, _ := io.ReadAll(in)
		request.input = string(input)
	}
	f.received = append(f.received, request)
	switch result {
	case "refuse":
		return 1, nil
	case "ssh-dropped":
		return 255, nil
	case "timed-out":
		return 124, nil
	case "warned-refuse", "warned-timeout":
		_, _ = io.WriteString(stderr, "sudo: unable to resolve host fixture: Name or service not known\n")
		if result == "warned-timeout" {
			return 124, nil
		}
		return 1, nil
	case "not-listed":
		_, _ = io.WriteString(stderr, "operator is not in the sudoers file.\n")
		return 1, nil
	case "password-required":
		_, _ = io.WriteString(stderr, "sudo: a password is required\n")
		return 1, nil
	case "sudo-failed":
		_, _ = io.WriteString(stderr, "sudo: account validation failure, is your account locked?\n")
		return 1, nil
	case "wrong-password":
		_, _ = io.WriteString(stderr, "Sorry, try again.\n"+sudoChallenge)
		<-ctx.Done()
		return 255, nil
	}
	_, err := io.WriteString(out, result)
	return 0, err
}

func (*sshFixture) IdentityFile(_ context.Context, path string) (string, error) { return path, nil }

func enrollmentFixture(t *testing.T, responses ...string) (Capability, lifecycle.Execution, *sshFixture, *[]string) {
	t.Helper()
	r := Request{Version: Implementation,
		Placement: machine.Placement{Machine: "node", Address: "192.0.2.7", Port: 22, Connection: machine.ConnectionSSH, User: "bootwright", Privilege: "sudo", PrivateKeyRef: "admin-key", KnownHostsRef: "host-pin"},
		Bootstrap: machine.Bootstrap{Auth: "askPassword", Sudo: true, SudoPrompt: true}}
	canonical, err := reconciliation.Freeze(r, "fixture")
	if err != nil {
		t.Fatal(err)
	}
	block := reconciliation.Block{BlockDefinition: enrollmentBlock(r, canonical), RequestDigest: strings.Repeat("a", 64)}
	pub, err := ssh.NewPublicKey(ed25519.PublicKey(bytes.Repeat([]byte{7}, 32)))
	if err != nil {
		t.Fatal(err)
	}
	f := &sshFixture{results: responses}
	var prompts []string
	remote := Remote{
		Launcher: f,
		Caller:   func(context.Context) (string, error) { return "operator", nil },
		Prompt: func(_ context.Context, label string, buffer []byte) (int, error) {
			prompts = append(prompts, label)
			if strings.HasPrefix(label, "SSH") {
				return copy(buffer, "fixture-login\n"), nil
			}
			return copy(buffer, "fixture-sudo\n"), nil
		},
		Hardware: "def read(): return []",
		Entropy:  func(b []byte) (int, error) { return len(b), nil },
	}
	material := map[string]secrets.Material{
		"admin-key": secrets.NewMaterial(map[secrets.Part][]byte{secrets.PublicKeyPart: ssh.MarshalAuthorizedKey(pub), secrets.PrivateKeyPart: []byte("private-key-fixture")}),
		"host-pin":  secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: append([]byte("192.0.2.7 "), ssh.MarshalAuthorizedKey(pub)...)}),
	}
	return Capability{Remote: remote}, lifecycle.Execution{Context: "fixture", ApplyOperation: "op-" + strings.Repeat("a", 32), Block: block, Material: material}, f, &prompts
}

func TestEnrollmentUsesSeparateCredentialsAndProvesAHandoff(t *testing.T) {
	capability, execution, ssh, prompts := enrollmentFixture(t, "absent", "[]", "ready", "bootwright", "0")
	ctx, release := BeginInvocation(context.Background())
	defer release()

	probe := lifecycle.Probe{Context: execution.Context, ApplyOperation: execution.ApplyOperation, Block: execution.Block, Material: execution.Material}
	if err := capability.PromptApplyStart(ctx, probe); err != nil || len(*prompts) != 2 || len(ssh.received) != 0 {
		t.Fatalf("collecting credentials asked %d prompts over %d connections: %v", len(*prompts), len(ssh.received), err)
	}
	if check := capability.ApplyStartCheck(execution.Block); check != "prove first access to Machine/node, changing nothing" {
		t.Fatalf("start check is described as %q", check)
	}
	if err := capability.CheckApplyStart(ctx, probe); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(*prompts, []string{"SSH password for operator on Machine/node: ", "Sudo password for operator on Machine/node (Enter reuses the SSH password): "}) {
		t.Fatalf("start check prompts %q", *prompts)
	}
	r, err := decode(execution.Block)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := capability.Remote.(Remote).HardwareAddresses(ctx, execution.Context, r.Placement, r.Bootstrap, execution.Material); err != nil {
		t.Fatal(err)
	}
	result, err := capability.Apply(ctx, execution)
	if err != nil || result.Outcome != reconciliation.OutcomeChanged || len(ssh.received) != 5 {
		t.Fatalf("outcome=%s connections=%d err=%v", result.Outcome, len(ssh.received), err)
	}
	if !slices.Equal(*prompts, []string{"SSH password for operator on Machine/node: ", "Sudo password for operator on Machine/node (Enter reuses the SSH password): "}) {
		t.Fatalf("prompts %q", *prompts)
	}
	check, claim, enrollment, login, sudo := ssh.received[0], ssh.received[1], ssh.received[2], ssh.received[3], ssh.received[4]
	if !slices.Contains(check.command, "observe") || slices.Contains(check.command, "apply") ||
		!slices.Contains(check.command, sudoExecutable) || check.password != "fixture-login" || check.input != "fixture-sudo\n" ||
		claim.user != "operator" || claim.input != "" || claim.command[0] != "/usr/bin/python3" ||
		enrollment.user != "operator" || enrollment.password != "fixture-login" || enrollment.input != "fixture-sudo\n" || !slices.Contains(enrollment.command, sudoExecutable) ||
		login.user != "bootwright" || sudo.user != "bootwright" || login.kind != machine.IdentityKey || sudo.kind != machine.IdentityKey ||
		login.password != "" || sudo.input != "" {
		t.Fatal("the hardware claim, enrollment and successor received the wrong access")
	}
	for _, request := range ssh.received {
		for _, argument := range request.command {
			if strings.Contains(argument, "fixture-login") || strings.Contains(argument, "fixture-sudo") || strings.Contains(argument, "private-key-fixture") {
				t.Fatal("credential reached the remote command arguments")
			}
		}
	}
}

func TestHardwareRecoveryCanCollectOnlyTheSSHPasswordBeforeProgress(t *testing.T) {
	capability, execution, ssh, prompts := enrollmentFixture(t, "[]")
	ctx, release := BeginInvocation(context.Background())
	defer release()
	request, err := decode(execution.Block)
	if err != nil {
		t.Fatal(err)
	}
	remote := capability.Remote.(Remote)
	if err := remote.PromptSSH(ctx, execution.Context, request.Placement, request.Bootstrap, execution.Material); err != nil {
		t.Fatal(err)
	}
	want := []string{"SSH password for operator on Machine/node: "}
	if !slices.Equal(*prompts, want) || len(ssh.received) != 0 {
		t.Fatalf("prompts=%v, connections=%d", *prompts, len(ssh.received))
	}
	if _, err := remote.HardwareAddresses(ctx, execution.Context, request.Placement, request.Bootstrap, execution.Material); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(*prompts, want) || len(ssh.received) != 1 {
		t.Fatalf("hardware read did not reuse the SSH credential: prompts=%v, connections=%d", *prompts, len(ssh.received))
	}
}

func TestEnrollmentLeavesSudoRefusalUnknown(t *testing.T) {
	capability, execution, _, _ := enrollmentFixture(t, "absent", "ready", "bootwright", "refuse")
	ctx, release := BeginInvocation(context.Background())
	defer release()
	result, err := capability.Apply(ctx, execution)
	if err == nil || result.Outcome != reconciliation.OutcomeUnknown {
		t.Fatalf("outcome=%s err=%v", result.Outcome, err)
	}
}

func TestEnrollmentExplainsRemoteSudoTTYRefusal(t *testing.T) {
	ctx, release := BeginInvocation(context.Background())
	defer release()
	capability, execution, _, _ := enrollmentFixture(t, "tty-refused", "no-tty", "absent", "ready", "bootwright", "tty-refused")
	check := func(err error, account string) {
		t.Helper()
		stops := diagnostics.Of(err)
		if len(stops) != 1 || stops[0].Code != "machine.enrollment-sudo-tty" ||
			!strings.Contains(stops[0].Message, "requires a terminal for "+account) ||
			strings.Contains(stops[0].Message, "fixture-private") ||
			!strings.Contains(stops[0].Remediation, "without a terminal") {
			t.Fatalf("unexpected diagnostic: %v", stops)
		}
	}
	probe := lifecycle.Probe{Context: execution.Context, ApplyOperation: execution.ApplyOperation, Block: execution.Block, Material: execution.Material}
	denied := capability.CheckApplyStart(ctx, probe)
	stops := diagnostics.Of(denied)
	if !machine.IsEnrollmentSudoTTY(denied) || len(stops) != 1 || !strings.Contains(stops[0].Message, "could not prepare one") ||
		!strings.Contains(stops[0].Remediation, "terminal allocation") || strings.Contains(stops[0].Message, "fixture-private") {
		t.Fatalf("unexpected bootstrap diagnostic: %v", stops)
	}
	outcome, err := capability.Apply(ctx, execution)
	if outcome.Outcome != reconciliation.OutcomeUnknown {
		t.Fatalf("unexpected outcome %s", outcome.Outcome)
	}
	check(err, "bootwright")
}

func TestEnrollmentWithdrawalNeedsAnIndependentPositiveProof(t *testing.T) {
	capability, execution, ssh, _ := enrollmentFixture(t, "refuse", "absent")
	ctx, release := BeginInvocation(context.Background())
	defer release()
	observation, err := capability.ObserveRemoval(ctx, execution)
	if err == nil || observation.Effect != reconciliation.EffectUnknown {
		t.Fatalf("failed connection observation=%+v err=%v", observation, err)
	}
	observation, err = capability.ObserveRemoval(ctx, execution)
	if err != nil || observation.Effect != reconciliation.EffectCompleted || ssh.received[1].user != "operator" {
		t.Fatalf("positive observation=%+v err=%v", observation, err)
	}
}

func TestSudoPasswordWaitsForTheRemotePrompt(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	in, errOut := sudoPasswordExchange(ctx, []byte("fixture-password"), sudoChallenge)
	_, _ = io.WriteString(errOut, "[sudo: no challenge]")
	cancel()
	data, err := io.ReadAll(in)
	if err != nil || len(data) != 0 {
		t.Fatal("the password was offered without the controlled sudo prompt")
	}

	in, errOut = sudoPasswordExchange(context.Background(), []byte("second-password"), sudoChallenge)
	_, _ = io.WriteString(errOut, sudoChallenge[:7])
	_, _ = io.WriteString(errOut, sudoChallenge[7:])
	data, err = io.ReadAll(in)
	if err != nil || string(data) != "second-password\n" {
		t.Fatal("the split prompt did not supply the expected password")
	}
}

func TestARepeatedSudoPromptRefusesThePasswordOnce(t *testing.T) {
	marker := "fixture-terminal-ready\n"
	refusals := 0
	in, exchange := sudoPasswordExchange(t.Context(), []byte("fixture-password"), marker, sudoChallenge)
	exchange.refuse = func() { refusals++ }
	_, _ = io.WriteString(exchange, marker+sudoChallenge)
	if data, err := io.ReadAll(in); err != nil || string(data) != "fixture-password\n" || exchange.refused {
		t.Fatal("the first prompt did not release the password alone")
	}
	_, _ = io.WriteString(exchange, "\r\nSorry, try again.\r\n"+sudoChallenge[:9])
	_, _ = io.WriteString(exchange, sudoChallenge[9:]+sudoChallenge)
	if !exchange.refused || refusals != 1 {
		t.Fatalf("a repeated prompt ended the session %d times", refusals)
	}
}

func TestAStalledExchangeNamesWhatTheRemoteSideAwaited(t *testing.T) {
	marker := "fixture-terminal-ready\n"
	_, exchange := sudoPasswordExchange(t.Context(), []byte("fixture-password"), marker, sudoChallenge)
	if exchange.stalled() != errSessionStalled {
		t.Fatal("a session without terminal readiness did not name the session")
	}
	_, _ = io.WriteString(exchange, marker)
	if exchange.stalled() != errSudoPromptStalled {
		t.Fatal("a ready terminal without a prompt did not name sudo")
	}
	_, _ = io.WriteString(exchange, sudoChallenge)
	if exchange.stalled() != errRemoteStalled {
		t.Fatal("an answered prompt did not name the remote command")
	}
	if _, plain := sudoPasswordExchange(t.Context(), []byte("fixture-password"), sudoChallenge); plain.stalled() != errSessionStalled {
		t.Fatal("a terminal-free session without a prompt claimed that sudo had started")
	}
	if _, idle := sudoPasswordExchange(t.Context(), nil, sudoChallenge); idle.stalled() != errRemoteStalled {
		t.Fatal("a session without a password did not name the remote command")
	}
}

func TestBootstrapRefusalsNameTheirCause(t *testing.T) {
	for result, want := range map[string]string{
		"ssh-refused":       "SSH session as operator did not open or ended early",
		"ssh-dropped":       "SSH session as operator ended before the remote command finished",
		"wrong-password":    "remote sudo did not accept the password for operator",
		"not-listed":        "does not authorize operator to run the enrollment as root",
		"password-required": "remote sudo requires a password for operator",
		"sudo-failed":       "remote sudo reported an error for operator",
		"warned-refuse":     "refused the host state for bootwright",
		"warned-timeout":    "did not finish the remote command for operator",
		"timed-out":         "did not finish the remote command for operator",
		"refuse":            "refused the host state for bootwright",
	} {
		for _, responses := range [][]string{{result}, {"tty-refused", result}} {
			t.Run(strings.Join(responses, " then "), func(t *testing.T) {
				capability, execution, _, _ := enrollmentFixture(t, responses...)
				ctx, release := BeginInvocation(t.Context())
				defer release()
				probe := lifecycle.Probe{Context: execution.Context, ApplyOperation: execution.ApplyOperation, Block: execution.Block, Material: execution.Material}
				stops := diagnostics.Of(capability.CheckApplyStart(ctx, probe))
				if len(stops) != 1 || stops[0].Code != "machine.enrollment" || !strings.Contains(stops[0].Message, want) ||
					strings.ContainsAny(stops[0].Message+stops[0].Remediation, "{}") || strings.Contains(stops[0].Message, "sudoers file") {
					t.Fatalf("unexpected diagnostic: %v", stops)
				}
			})
		}
	}
}

func TestAnUnprivilegedBootstrapReadNamesOnlyAnSSHCause(t *testing.T) {
	for result, want := range map[string]string{
		"ssh-refused": "SSH session as operator did not open or ended early",
		"sudo-failed": "could not prove first access as operator",
	} {
		capability, execution, _, _ := enrollmentFixture(t, result)
		ctx, release := BeginInvocation(t.Context())
		request, err := decode(execution.Block)
		if err != nil {
			t.Fatal(err)
		}
		_, err = capability.Remote.(Remote).HardwareAddresses(ctx, execution.Context, request.Placement, request.Bootstrap, execution.Material)
		release()
		if stops := diagnostics.Of(err); len(stops) != 1 || !strings.Contains(stops[0].Message, want) {
			t.Fatalf("%s: unexpected diagnostic: %v", result, stops)
		}
	}
}

func TestAnInterruptedSessionNamesNoSSHCause(t *testing.T) {
	capability, execution, _, _ := enrollmentFixture(t, "ssh-refused")
	scoped, release := BeginInvocation(t.Context())
	defer release()
	ctx, cancel := context.WithCancel(scoped)
	remote := capability.Remote.(Remote)
	remote.Launcher = interruptingLauncher{remote.Launcher, cancel}
	request, err := decode(execution.Block)
	if err != nil {
		t.Fatal(err)
	}
	_, err = remote.HardwareAddresses(ctx, execution.Context, request.Placement, request.Bootstrap, execution.Material)
	if stops := diagnostics.Of(err); len(stops) != 1 || !strings.Contains(stops[0].Message, "could not prove first access as operator") {
		t.Fatalf("unexpected diagnostic: %v", stops)
	}
}

type interruptingLauncher struct {
	Launcher
	interrupt func()
}

func (l interruptingLauncher) Run(ctx context.Context, session machine.Session, in io.Reader, out, stderr io.Writer) (int, error) {
	code, err := l.Launcher.Run(ctx, session, in, out, stderr)
	go l.interrupt()
	return code, err
}

func TestSameKeyMaterialUnderDifferentNamesIsRefused(t *testing.T) {
	_, execution, _, _ := enrollmentFixture(t)
	r, err := decode(execution.Block)
	if err != nil {
		t.Fatal(err)
	}
	r.FleetKeyRef, r.ClusterKeyRef = "fleet", "native"
	execution.Material["fleet"] = execution.Material["admin-key"]
	execution.Material["native"] = execution.Material["admin-key"]
	if _, err := publicKeys(r, execution.Material); err == nil {
		t.Fatal("duplicate key material was admitted as root-equivalent access")
	}
}

func TestPromptedCredentialsFollowTheBootstrapUserAcrossMachines(t *testing.T) {
	ctx, release := BeginInvocation(t.Context())
	defer release()
	var labels []string
	remote := Remote{Prompt: func(_ context.Context, label string, buffer []byte) (int, error) {
		labels = append(labels, label)
		switch {
		case strings.HasPrefix(label, "SSH"):
			return copy(buffer, "fixture-login\n"), nil
		case strings.Contains(label, "(Enter reuses the SSH password)"):
			return copy(buffer, "\n"), nil
		default:
			return copy(buffer, "fixture-sudo\n"), nil
		}
	}}
	check := func(ctx context.Context, machineName, user, purpose, want string) {
		t.Helper()
		got, err := remote.password(ctx, machineName, user, purpose, "", nil)
		defer clear(got)
		if err != nil || string(got) != want {
			t.Fatalf("%s %s on %s did not receive its expected password: %v", user, purpose, machineName, err)
		}
	}
	check(ctx, "first", "operator", "SSH", "fixture-login")
	check(ctx, "first", "operator", "remote sudo", "fixture-login")
	check(ctx, "second", "operator", "SSH", "fixture-login")
	check(ctx, "second", "operator", "remote sudo", "fixture-login")
	check(ctx, "second", "other", "remote sudo", "fixture-sudo")
	next, end := BeginInvocation(t.Context())
	defer end()
	check(next, "first", "operator", "remote sudo", "fixture-sudo")
	if len(labels) != 4 {
		t.Fatalf("prompted %d times, want 4", len(labels))
	}
}

func TestEnrollmentProvesTerminalSudoBeforeChangingTheAccount(t *testing.T) {
	for _, effect := range []string{"apply", "destroy"} {
		t.Run(effect, func(t *testing.T) {
			want := "ready"
			if effect == "destroy" {
				want = "absent"
			}
			capability, execution, ssh, prompts := enrollmentFixture(t, "tty-refused", "absent", want, "bootwright", "0")
			ctx, release := BeginInvocation(t.Context())
			defer release()
			var result lifecycle.Result
			var err error
			if effect == "apply" {
				probe := lifecycle.Probe{Context: execution.Context, ApplyOperation: execution.ApplyOperation, Block: execution.Block, Material: execution.Material}
				if err := capability.CheckApplyStart(ctx, probe); err != nil {
					t.Fatal(err)
				}
				result, err = capability.Apply(ctx, execution)
			} else {
				result, err = capability.Destroy(ctx, execution)
			}
			if err != nil || result.Outcome != reconciliation.OutcomeChanged || len(*prompts) != 2 || len(ssh.received) < 3 {
				t.Fatalf("outcome %s, connections %d, prompts %d, error %v", result.Outcome, len(ssh.received), len(*prompts), err)
			}
			first, fallback, changed := ssh.received[0], ssh.received[1], ssh.received[2]
			if first.terminal || !slices.Contains(first.command, "observe") || first.input != "" ||
				!fallback.terminal || !slices.Contains(fallback.command, "observe") || fallback.input != "fixture-sudo\n" ||
				!changed.terminal || !slices.Contains(changed.command, effect) || changed.input != "fixture-sudo\n" {
				t.Fatal("the terminal fallback must prove sudo with a read-only observation before the single effect")
			}
			for _, request := range ssh.received[3:] {
				if request.terminal || request.user != "bootwright" {
					t.Fatal("the managed handoff must keep SSH and sudo terminal-free")
				}
			}
		})
	}
}

func TestTerminalSudoPasswordRequiresReadinessAndTheSudoPrompt(t *testing.T) {
	marker := "fixture-terminal-ready\n"
	ctx, cancel := context.WithCancel(t.Context())
	in, stdout := sudoPasswordExchange(ctx, []byte("fixture-password"), marker, sudoChallenge)
	_, _ = io.WriteString(stdout, sudoChallenge+marker)
	cancel()
	if data, err := io.ReadAll(in); err != nil || len(data) != 0 {
		t.Fatal("the password was offered for a prompt that preceded terminal readiness")
	}

	in, stdout = sudoPasswordExchange(t.Context(), []byte("fixture-password"), marker, sudoChallenge)
	_, _ = io.WriteString(stdout, marker[:5])
	_, _ = io.WriteString(stdout, marker[5:]+sudoChallenge[:5])
	_, _ = io.WriteString(stdout, sudoChallenge[5:])
	if data, err := io.ReadAll(in); err != nil || string(data) != "fixture-password\n" {
		t.Fatal("the split terminal readiness and sudo prompt did not release the password")
	}
}

func TestEnrollmentUsesTheSamePromptsForTwoMachineChecksAndEffects(t *testing.T) {
	capability, first, ssh, prompts := enrollmentFixture(t, "absent", "absent", "ready", "bootwright", "0", "ready", "bootwright", "0")
	ctx, release := BeginInvocation(t.Context())
	defer release()
	request, err := decode(first.Block)
	if err != nil {
		t.Fatal(err)
	}
	request.Placement.Machine = "other"
	request.Placement.Address = "192.0.2.8"
	second := first
	second.Material = map[string]secrets.Material{"admin-key": first.Material["admin-key"]}
	pub, _ := second.Material["admin-key"].Part(secrets.PublicKeyPart)
	second.Material["host-pin"] = secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: append([]byte("192.0.2.8 "), pub...)})
	canonical, err := reconciliation.Freeze(request, "fixture")
	if err != nil {
		t.Fatal(err)
	}
	second.Block = reconciliation.Block{BlockDefinition: enrollmentBlock(request, canonical), RequestDigest: strings.Repeat("b", 64)}

	for _, execution := range []lifecycle.Execution{first, second} {
		probe := lifecycle.Probe{Context: execution.Context, ApplyOperation: execution.ApplyOperation, Block: execution.Block, Material: execution.Material}
		if err := capability.CheckApplyStart(ctx, probe); err != nil {
			t.Fatal(err)
		}
	}
	for _, execution := range []lifecycle.Execution{first, second} {
		result, err := capability.Apply(ctx, execution)
		if err != nil || result.Outcome != reconciliation.OutcomeChanged {
			t.Fatalf("enrollment did not complete: %v", err)
		}
	}
	if len(*prompts) != 2 || len(ssh.received) != 8 {
		t.Fatalf("prompts = %d, connections = %d", len(*prompts), len(ssh.received))
	}
	for _, i := range []int{0, 1, 2, 5} {
		request := ssh.received[i]
		if request.password != "fixture-login" || request.input != "fixture-sudo\n" {
			t.Fatal("the bootstrap session received the wrong SSH or sudo password")
		}
	}
}

func TestAReadWithoutEnterDoesNotReuseTheSSHPassword(t *testing.T) {
	ctx, release := BeginInvocation(t.Context())
	defer release()
	remote := Remote{Prompt: func(_ context.Context, _ string, buffer []byte) (int, error) {
		return copy(buffer, "fixture-login\n"), nil
	}}
	got, err := remote.password(ctx, "node", "operator", "SSH", "", nil)
	clear(got)
	if err != nil {
		t.Fatal(err)
	}
	remote.Prompt = func(context.Context, string, []byte) (int, error) { return 0, nil }
	if got, err := remote.password(ctx, "node", "operator", "remote sudo", "", nil); err == nil {
		clear(got)
		t.Fatal("an empty read reused the SSH password")
	}
}

func TestAForcedBootstrapTerminalNeedsEntropyBeforeConnecting(t *testing.T) {
	session := machine.Session{ForceTTY: true, Command: []string{sudoExecutable}}
	remote := Remote{Launcher: &sshFixture{}}
	if _, err := remote.run(t.Context(), session, nil); !errors.Is(err, errBootstrapTTY) {
		t.Fatalf("without an entropy source: %v", err)
	}
	remote.Entropy = func([]byte) (int, error) { return 0, nil }
	if _, err := remote.run(t.Context(), session, nil); !errors.Is(err, errBootstrapTTY) {
		t.Fatalf("with insufficient entropy: %v", err)
	}
}

func TestEnrollmentTerminalFailureAfterReadinessLeavesRemovalUnknown(t *testing.T) {
	for _, result := range []string{"refuse", "conflicting-tty"} {
		t.Run(result, func(t *testing.T) {
			capability, execution, _, _ := enrollmentFixture(t, "tty-refused", result)
			ctx, release := BeginInvocation(t.Context())
			defer release()
			observation, err := capability.ObserveRemoval(ctx, execution)
			if observation.Effect != reconciliation.EffectUnknown || err == nil || machine.IsEnrollmentSudoTTY(err) {
				t.Fatalf("observation = %s, error = %v", observation.Effect, err)
			}
		})
	}
}
