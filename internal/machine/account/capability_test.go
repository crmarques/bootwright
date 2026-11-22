package account

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"io"
	"slices"
	"strings"
	"testing"

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
}

type sshFixture struct {
	received []sshRequest
	results  []string
}

func (f *sshFixture) Run(_ context.Context, session machine.Session, in io.Reader, out, stderr io.Writer) (int, error) {
	var input []byte
	if slices.Contains(session.Command, sudoChallenge) {
		_, _ = io.WriteString(stderr, sudoChallenge)
	}
	if in != nil {
		input, _ = io.ReadAll(in)
	}
	f.received = append(f.received, sshRequest{session.User, session.Kind, string(session.Password), session.Command, string(input)})
	if !session.HostKey.Present() || len(f.results) == 0 {
		return 255, nil
	}
	result := f.results[0]
	f.results = f.results[1:]
	if result == "refuse" {
		return 1, nil
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
	}
	material := map[string]secrets.Material{
		"admin-key": secrets.NewMaterial(map[secrets.Part][]byte{secrets.PublicKeyPart: ssh.MarshalAuthorizedKey(pub), secrets.PrivateKeyPart: []byte("private-key-fixture")}),
		"host-pin":  secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: append([]byte("192.0.2.7 "), ssh.MarshalAuthorizedKey(pub)...)}),
	}
	return Capability{Remote: remote}, lifecycle.Execution{Context: "fixture", ApplyOperation: "op-" + strings.Repeat("a", 32), Block: block, Material: material}, f, &prompts
}

func TestEnrollmentUsesSeparateCredentialsAndProvesAHandoff(t *testing.T) {
	capability, execution, ssh, prompts := enrollmentFixture(t, "[]", "ready", "bootwright", "0")
	ctx, release := BeginInvocation(context.Background())
	defer release()

	r, err := decode(execution.Block)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := capability.Remote.(Remote).HardwareAddresses(ctx, execution.Context, r.Placement, r.Bootstrap, execution.Material); err != nil {
		t.Fatal(err)
	}
	result, err := capability.Apply(ctx, execution)
	if err != nil || result.Outcome != reconciliation.OutcomeChanged || len(ssh.received) != 4 {
		t.Fatalf("outcome=%s connections=%d err=%v", result.Outcome, len(ssh.received), err)
	}
	if !slices.Equal(*prompts, []string{"SSH password for operator on Machine/node: ", "remote sudo password for operator on Machine/node: "}) {
		t.Fatalf("prompts %q", *prompts)
	}
	claim, enrollment, login, sudo := ssh.received[0], ssh.received[1], ssh.received[2], ssh.received[3]
	if claim.user != "operator" || claim.input != "" || claim.command[0] != "/usr/bin/python3" ||
		enrollment.user != "operator" || enrollment.password != "fixture-login" || enrollment.input != "fixture-sudo\n" || enrollment.command[0] != "/usr/bin/sudo" ||
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

func TestEnrollmentLeavesSudoRefusalUnknown(t *testing.T) {
	capability, execution, _, _ := enrollmentFixture(t, "ready", "bootwright", "refuse")
	ctx, release := BeginInvocation(context.Background())
	defer release()
	result, err := capability.Apply(ctx, execution)
	if err == nil || result.Outcome != reconciliation.OutcomeUnknown {
		t.Fatalf("outcome=%s err=%v", result.Outcome, err)
	}
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
	in, errOut := sudoPasswordExchange(ctx, []byte("fixture-password"))
	_, _ = io.WriteString(errOut, "[sudo: no challenge]")
	cancel()
	data, err := io.ReadAll(in)
	if err != nil || len(data) != 0 {
		t.Fatal("the password was offered without the controlled sudo prompt")
	}

	in, errOut = sudoPasswordExchange(context.Background(), []byte("second-password"))
	_, _ = io.WriteString(errOut, sudoChallenge[:7])
	_, _ = io.WriteString(errOut, sudoChallenge[7:])
	data, err = io.ReadAll(in)
	if err != nil || string(data) != "second-password\n" {
		t.Fatal("the split prompt did not supply the expected password")
	}
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
