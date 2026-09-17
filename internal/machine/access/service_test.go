package access

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/trust"
)

const (
	publicKey   = "AAAAC3NzaC1lZDI1NTE5AAAAICG+RadYRVwHqO7OQsOpyh8SDf1tbTQ8PUmgn7UEmou/"
	fingerprint = "SHA256:8oT3qNBeFE7en6Ce9uTOcz+5ihUnqv435ifHqGftBJA"
	otherKey    = "AAAAC3NzaC1lZDI1NTE5AAAAIMEyPr7qCaU9aM6pIjE3rMbT4dQK3S3dJH8vMWB0ZBQr"
)

func object(kind api.Kind, name string, spec api.Value) api.Object {
	return api.NewObject(kind, name, api.MapValue(), spec)
}

func m(kv ...any) api.Value {
	fields := []api.FieldValue{}
	for i := 0; i < len(kv); i += 2 {
		var value api.Value
		switch typed := kv[i+1].(type) {
		case api.Value:
			value = typed
		case string:
			value = api.StringValue(typed)
		case bool:
			value = api.BoolValue(typed)
		}
		fields = append(fields, api.FieldValue{Name: kv[i].(string), Value: value})
	}
	return api.MapValue(fields...)
}

func list(values ...api.Value) api.Value { return api.ListValue(values...) }

func catalog() api.Catalog {
	network := m("addresses", list(m("name", "ssh", "address", "192.0.2.10/24")))
	host := object(api.Machine, "host", m(
		"os", m("provided", true), "network", network,
		"access", m("ssh", m("addressRef", "ssh", "user", "operator", "port", api.IntegerValue("2222"),
			"auth", m("operatorIdentity", m())))))
	guest := object(api.Machine, "guest", m(
		"os", m("provided", false, "installProfileRef", "rhel"), "network", network,
		"access", m("ssh", m("addressRef", "ssh", "user", "bootwright",
			"auth", m("privateKeyRef", "fleet")))))
	declared := object(api.Machine, "declared", m(
		"os", m("provided", true), "network", network,
		"access", m("ssh", m("addressRef", "ssh", "user", "admin", "knownHostsRef", "declared-host-key",
			"auth", m("privateKeyRef", "admin-key")))))
	secured := object(api.Machine, "secured", m(
		"os", m("provided", true), "network", network,
		"access", m("ssh", m("addressRef", "ssh", "user", "admin", "auth", m("passwordRef", "admin-password")))))
	local := object(api.Machine, "controller", m("os", m("provided", true), "access", m("local", true)))
	return api.NewCatalog([]api.Object{host, guest, declared, secured, local})
}

func code(t *testing.T, err error) string {
	t.Helper()
	reported := diagnostics.Of(err)
	if len(reported) == 0 {
		t.Fatalf("error carries no diagnostic: %v", err)
	}
	return reported[0].Code
}

type stateSource struct{}

func (stateSource) RenderEffective(context.Context, compilation.EffectiveRequest) (*compilation.EffectiveResult, error) {
	return &compilation.EffectiveResult{Effective: catalog()}, nil
}

// lender models the store: it hands out exactly the declarations requested and
// records them, so a test can prove nothing else was opened.
type lender struct {
	requested []string
	material  map[string]secrets.Material
}

func (l *lender) WithMaterial(ctx context.Context, request lifecycle.MaterialRequest,
	use func(context.Context, map[string]secrets.Material) error) error {
	l.requested = append([]string(nil), request.Secrets...)
	opened := map[string]secrets.Material{}
	for _, name := range request.Secrets {
		if value, ok := l.material[name]; ok {
			opened[name] = value
		}
	}
	return use(ctx, opened)
}

type owner struct{ realized bool }

func (o owner) Ownership(context.Context, string) (map[string]machine.OwnershipState, error) {
	if !o.realized {
		return map[string]machine.OwnershipState{}, nil
	}
	return map[string]machine.OwnershipState{
		"Machine/guest": {Verb: machine.VerbApply, State: machine.BlockDone},
	}, nil
}

type proof struct {
	evidence machine.HostKeyEvidence
	found    bool
	err      error
}

func (p proof) HostKey(context.Context, string, string) (machine.HostKeyEvidence, bool, error) {
	return p.evidence, p.found, p.err
}

type store struct {
	data    []byte
	written [][]byte
	err     error
}

func (s *store) ReadHostKeys(context.Context, string) ([]byte, error) { return s.data, s.err }

func (s *store) ReplaceHostKeys(_ context.Context, _ string, data, expected []byte) error {
	if string(expected) != string(s.data) {
		return errors.New("expectation")
	}
	s.written = append(s.written, data)
	s.data = data
	return nil
}

type observer struct {
	key    trust.HostKey
	err    error
	probes int
}

func (o *observer) Observe(context.Context, string, int) (trust.HostKey, error) {
	o.probes++
	return o.key, o.err
}

type prompt struct {
	asked   []string
	decline error
}

func (p *prompt) ConfirmHostKey(_ context.Context, name, address, keyType, fingerprint string) error {
	p.asked = append(p.asked, name+" "+address+" "+keyType+" "+fingerprint)
	return p.decline
}

type client struct {
	sessions []machine.Session
	// keys copies what each session carried while it ran. The material is
	// cleared as soon as the session ends, so a test that read the session
	// afterwards would be reading the zeroing rather than the credential.
	keys    []string
	code    int
	err     error
	offered string
	idErr   error
}

func (c *client) Run(_ context.Context, session machine.Session, _ io.Reader, _, _ io.Writer) (int, error) {
	c.sessions = append(c.sessions, session)
	c.keys = append(c.keys, string(session.PrivateKey))
	return c.code, c.err
}

func (c *client) IdentityFile(path string) (string, error) {
	if c.idErr != nil {
		return "", c.idErr
	}
	if path == "" {
		return "", nil
	}
	if c.offered != "" {
		return c.offered, nil
	}
	return path, nil
}

type harness struct {
	service   Service
	lender    *lender
	trust     *store
	observer  *observer
	prompt    *prompt
	client    *client
	errStream *strings.Builder
}

func newHarness(t *testing.T, adjust func(*Options)) *harness {
	t.Helper()
	trusted, err := trust.ParseAuthorizedKey("ssh-ed25519 " + publicKey)
	if err != nil {
		t.Fatal(err)
	}
	h := &harness{
		lender: &lender{material: map[string]secrets.Material{
			"fleet":             secrets.NewMaterial(map[secrets.Part][]byte{secrets.PrivateKeyPart: []byte("FLEET KEY")}),
			"admin-key":         secrets.NewMaterial(map[secrets.Part][]byte{secrets.PrivateKeyPart: []byte("ADMIN KEY")}),
			"declared-host-key": secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: []byte("192.0.2.10 ssh-ed25519 " + publicKey + "\n")}),
		}},
		trust:     &store{},
		observer:  &observer{key: trusted},
		prompt:    &prompt{},
		client:    &client{},
		errStream: &strings.Builder{},
	}
	options := Options{
		Lender: h.lender, Ownership: owner{realized: true},
		Evidence:  proof{evidence: machine.HostKeyEvidence{Address: "192.0.2.10", HostKey: trusted}, found: true},
		Trust:     h.trust,
		Observer:  h.observer,
		Confirmer: h.prompt,
		Launcher:  h.client,
		Streams:   Streams{Err: h.errStream},
		Terminal:  func() (bool, error) { return true, nil },
		Clock:     func() time.Time { return time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC) },
	}
	if adjust != nil {
		adjust(&options)
	}
	h.service = New(stateSource{}, func(context.Context) (string, error) { return "lab", nil }, options)
	return h
}

func exec(t *testing.T, h *harness, name string, options machine.SSHOptions, words ...string) (*machine.SessionResult, error) {
	t.Helper()
	return h.service.Exec(context.Background(), ExecRequest{Name: name, SSH: options, Command: words})
}

// An installed Machine resolves the account and credential its desired state
// authorizes, with no export step and no flag from the operator.
func TestAnInstalledMachineOpensAsTheIdentityItsStateAuthorizes(t *testing.T) {
	h := newHarness(t, nil)
	result, err := exec(t, h, "guest", machine.SSHOptions{}, "pwd")
	if err != nil {
		t.Fatal(err)
	}
	if result.Machine != "guest" || result.Context != "lab" || result.ExitCode != 0 {
		t.Fatalf("result = %+v", result)
	}
	if len(h.client.sessions) != 1 {
		t.Fatalf("sessions = %+v", h.client.sessions)
	}
	session := h.client.sessions[0]
	if session.Kind != machine.IdentityKey || session.User != "bootwright" {
		t.Fatalf("identity = %+v", session.Identity)
	}
	if h.client.keys[0] != "FLEET KEY" {
		t.Fatalf("the session did not carry its own credential: %q", h.client.keys[0])
	}
	if session.HostKey.PublicKey != publicKey {
		t.Fatalf("host key = %+v", session.HostKey)
	}
	if strings.Join(session.Command, " ") != "pwd" {
		t.Fatalf("command = %v", session.Command)
	}
	if strings.Join(h.lender.requested, ",") != "fleet" {
		t.Fatalf("opened %v, want the credential alone", h.lender.requested)
	}
}

func TestTheClientsExitStatusIsTheResult(t *testing.T) {
	h := newHarness(t, nil)
	h.client.code = 7
	result, err := exec(t, h, "guest", machine.SSHOptions{}, "false")
	if err != nil || result.ExitCode != 7 {
		t.Fatalf("result = %+v (%v)", result, err)
	}
}

func TestEachIdentityArmResolvesItsOwnCredential(t *testing.T) {
	for _, test := range []struct {
		name, object, user string
		kind               machine.IdentityKind
		opened             string
	}{
		{"declared key", "declared", "admin", machine.IdentityKey, "admin-key,declared-host-key"},
		{"operator identity", "host", "operator", machine.IdentityOperator, ""},
		{"declared password", "secured", "admin", machine.IdentityPassword, ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			h := newHarness(t, nil)
			if _, err := exec(t, h, test.object, machine.SSHOptions{}, "true"); err != nil {
				t.Fatal(err)
			}
			session := h.client.sessions[0]
			if session.Kind != test.kind || session.User != test.user {
				t.Fatalf("identity = %+v", session.Identity)
			}
			if strings.Join(h.lender.requested, ",") != test.opened {
				t.Fatalf("opened %v, want %q", h.lender.requested, test.opened)
			}
		})
	}
}

// The product never answers a password prompt on the operator's behalf, so it
// names the reveal they perform themselves before the client asks.
func TestAPasswordMachineNamesTheRevealRatherThanAnsweringIt(t *testing.T) {
	h := newHarness(t, nil)
	if _, err := exec(t, h, "secured", machine.SSHOptions{}, "true"); err != nil {
		t.Fatal(err)
	}
	notice := h.errStream.String()
	if !strings.HasPrefix(notice, "[WARN] access.credential:") || !strings.Contains(notice, "admin-password") {
		t.Fatalf("advisory = %q", notice)
	}
	if strings.Contains(notice, "PASSWORD") {
		t.Fatalf("the advisory carried material: %q", notice)
	}
}

// A credential this context holds opens exactly the account it was authored
// for. Another account receives none, rather than being handed a key that
// belongs to someone else.
func TestABorrowedAccountIsNeverHandedAnotherAccountsCredential(t *testing.T) {
	h := newHarness(t, nil)
	_, err := exec(t, h, "guest", machine.SSHOptions{User: "root"}, "id")
	if err == nil {
		t.Fatal("a borrowed account was opened with no credential of its own")
	}
	if got := code(t, err); got != "access.unavailable" {
		t.Fatalf("code = %q", got)
	}
	if len(h.client.sessions) != 0 {
		t.Fatalf("a session was opened: %+v", h.client.sessions)
	}

	offered := newHarness(t, nil)
	offered.client.offered = "/home/operator/.ssh/id_ed25519"
	if _, err := exec(t, offered, "guest", machine.SSHOptions{User: "root", IdentityFile: "~/.ssh/id_ed25519"}, "id"); err != nil {
		t.Fatal(err)
	}
	session := offered.client.sessions[0]
	if session.User != "root" || session.Kind != machine.IdentityOperator {
		t.Fatalf("identity = %+v", session.Identity)
	}
	if offered.client.keys[0] != "" {
		t.Fatalf("a borrowed account carried a stored credential: %q", offered.client.keys[0])
	}
	if session.IdentityFile != "/home/operator/.ssh/id_ed25519" {
		t.Fatalf("offered key = %q", session.IdentityFile)
	}
}

// Naming the Machine's own account is not borrowing: it resolves to exactly
// the identity that account already carries.
func TestNamingTheMachinesOwnAccountKeepsItsCredential(t *testing.T) {
	h := newHarness(t, nil)
	if _, err := exec(t, h, "guest", machine.SSHOptions{User: "bootwright"}, "id"); err != nil {
		t.Fatal(err)
	}
	session := h.client.sessions[0]
	if session.Kind != machine.IdentityKey || h.client.keys[0] != "FLEET KEY" {
		t.Fatalf("identity = %+v key = %q", session.Identity, h.client.keys[0])
	}
}

// The offered key is preferred and the declared credential stays behind it, so
// an operator can reach the Machine's own account with a key of their own.
func TestAnOfferedKeyIsAddedToTheMachinesOwnIdentity(t *testing.T) {
	h := newHarness(t, nil)
	h.client.offered = "/keys/mine"
	if _, err := exec(t, h, "guest", machine.SSHOptions{IdentityFile: "/keys/mine"}, "id"); err != nil {
		t.Fatal(err)
	}
	session := h.client.sessions[0]
	if session.IdentityFile != "/keys/mine" || h.client.keys[0] != "FLEET KEY" {
		t.Fatalf("identity = %+v key = %q", session.Identity, h.client.keys[0])
	}
}

func TestAnUnusableOfferedKeyRefusesBeforeAnythingIsOpened(t *testing.T) {
	h := newHarness(t, nil)
	h.client.idErr = errors.New("mode 0644")
	if _, err := exec(t, h, "guest", machine.SSHOptions{IdentityFile: "/keys/loose"}, "id"); err == nil {
		t.Fatal("an unusable key was accepted")
	}
	if len(h.lender.requested) != 0 || len(h.client.sessions) != 0 {
		t.Fatal("material was opened for a refused invocation")
	}
}

func TestAnUnreachableTargetNeverOpensASession(t *testing.T) {
	for _, test := range []struct{ name, object, want string }{
		{"unnamed", "", "access.target"},
		{"outside the selected graph", "absent", "access.target"},
		{"reached locally", "controller", "access.unavailable"},
	} {
		t.Run(test.name, func(t *testing.T) {
			h := newHarness(t, nil)
			if _, err := exec(t, h, test.object, machine.SSHOptions{}, "true"); code(t, err) != test.want {
				t.Fatalf("code = %q, want %q", code(t, err), test.want)
			}
			if len(h.client.sessions) != 0 || len(h.lender.requested) != 0 {
				t.Fatal("a refused target reached the host or the store")
			}
		})
	}
}

func TestAnInteractiveSessionTakesNoCommandAndACommandIsRequired(t *testing.T) {
	h := newHarness(t, nil)
	if _, err := h.service.Rsh(context.Background(), RshRequest{Name: "guest"}); err != nil {
		t.Fatal(err)
	}
	if len(h.client.sessions[0].Command) != 0 {
		t.Fatalf("an interactive session carried a command: %v", h.client.sessions[0].Command)
	}
	if _, err := exec(t, h, "guest", machine.SSHOptions{}); code(t, err) != "cli.usage" {
		t.Fatal("an empty command vector was accepted")
	}
}

func TestACommandValueThatCannotBeEncodedRefusesTheSession(t *testing.T) {
	h := newHarness(t, nil)
	_, err := exec(t, h, "guest", machine.SSHOptions{}, "systemctl", "status\nrm -rf /")
	if err == nil || code(t, err) != "access.handoff" {
		t.Fatalf("err = %v", err)
	}
	if len(h.client.sessions) != 0 {
		t.Fatal("a session ran an altered command")
	}
}
