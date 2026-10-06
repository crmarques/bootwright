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
	// errStream is the operator's error stream, so a test sees what was
	// written there before the prompt.
	errStream *strings.Builder
	before    []string
}

func (p *prompt) ConfirmHostKey(_ context.Context, contextName, name, address, keyType, fingerprint string) error {
	p.asked = append(p.asked, name+" "+address+" "+keyType+" "+fingerprint)
	if p.errStream != nil {
		p.before = append(p.before, p.errStream.String())
	}
	if contextName != "lab" {
		return errors.New("the prompt named another context: " + contextName)
	}
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

func (c *client) IdentityFile(_ context.Context, path string) (string, error) {
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
		client:    &client{},
		errStream: &strings.Builder{},
	}
	h.prompt = &prompt{errStream: h.errStream}
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

// The installed proof holds only at the address its installation proved, so a
// session dialing another names that address as the one to declare, and a
// Machine the context has not installed names the apply of that context.
func TestAnInstalledProofRefusalNamesItsRemedy(t *testing.T) {
	moved := newHarness(t, func(o *Options) {
		parsed, err := trust.ParseAuthorizedKey("ssh-ed25519 " + publicKey)
		if err != nil {
			t.Fatal(err)
		}
		o.Evidence = proof{evidence: machine.HostKeyEvidence{Address: "198.51.100.11", HostKey: parsed}, found: true}
	})
	_, err := exec(t, moved, "guest", machine.SSHOptions{}, "true")
	reported := diagnostics.Of(err)
	if len(reported) != 1 || reported[0].Code != "trust.identity" ||
		reported[0].Message != "the installation of Machine/guest proved a host key for 198.51.100.11, not for 192.0.2.10" ||
		reported[0].Remediation != "declare an ssh address of 198.51.100.11 on Machine/guest, the address its installation proved, "+
			"or remove the ssh address it declares" {
		t.Fatalf("refusal = %+v", reported)
	}
	unowned := newHarness(t, func(o *Options) { o.Ownership = owner{realized: false} })
	_, err = exec(t, unowned, "guest", machine.SSHOptions{}, "true")
	reported = diagnostics.Of(err)
	if len(reported) != 1 || reported[0].Code != "access.target" ||
		reported[0].Remediation != "bootwright apply --context lab, or reach the Machine with its own declared access" {
		t.Fatalf("refusal = %+v", reported)
	}
	if len(moved.client.sessions)+len(unowned.client.sessions) != 0 {
		t.Fatal("a refused proof opened a session")
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

// A first use whose endpoint a Machine this context no longer declares still
// holds takes the endpoint over in the one confirmed write, and names the
// record it removes on the operator's error stream before asking.
func TestAFirstUseTakesOverAnUndeclaredEndpoint(t *testing.T) {
	h := newHarness(t, nil)
	h.trust.data = recorded(t, "retired", "192.0.2.10", 2222, otherKey)
	if _, err := exec(t, h, "host", machine.SSHOptions{}, "true"); err != nil {
		t.Fatal(err)
	}
	notice := "[WARN] trust.identity: confirming also removes the host key this context trusted for retired, " +
		"which it no longer declares, at [192.0.2.10]:2222\n"
	if len(h.prompt.before) != 1 || h.prompt.before[0] != notice {
		t.Fatalf("before the prompt the operator read %q, want %q", h.prompt.before, notice)
	}
	if len(h.trust.written) != 1 {
		t.Fatalf("writes = %d", len(h.trust.written))
	}
	records, err := trust.Decode(h.trust.written[0])
	if err != nil {
		t.Fatal(err)
	}
	if len(records.Hosts) != 1 || records.Hosts[0].Machine != "host" || records.Hosts[0].PublicKey != publicKey {
		t.Fatalf("records = %+v", records.Hosts)
	}

	declined := newHarness(t, nil)
	declined.trust.data = recorded(t, "retired", "192.0.2.10", 2222, otherKey)
	declined.prompt.decline = errors.New("declined")
	if _, err := exec(t, declined, "host", machine.SSHOptions{}, "true"); err == nil {
		t.Fatal("a declined first use opened a session")
	}
	if len(declined.trust.written) != 0 || len(declined.client.sessions) != 0 {
		t.Fatal("a declined first use removed or recorded a key")
	}
}

// A record of a Machine still declared is never removed, so a first use that
// would pin its endpoint to a second key refuses before the operator is asked
// to accept a key no write could record. When that Machine no longer uses this
// context's trust, a re-trust of it would refuse, so the remedy names the
// input change that drops its record instead.
func TestAFirstUseThatWouldPinTwoKeysRefusesBeforeAsking(t *testing.T) {
	h := newHarness(t, nil)
	h.trust.data = recorded(t, "secured", "192.0.2.10", 2222, otherKey)
	_, err := exec(t, h, "host", machine.SSHOptions{}, "true")
	if err == nil || code(t, err) != "trust.identity" {
		t.Fatalf("err = %v", err)
	}
	if reported := diagnostics.Of(err)[0]; !strings.Contains(reported.Message, "[192.0.2.10]:2222") ||
		reported.Remediation != "re-trust secured with bootwright machine trust --context lab --machines secured --replace secured" {
		t.Fatalf("refusal = %+v", reported)
	}
	if len(h.prompt.asked) != 0 || len(h.trust.written) != 0 || len(h.client.sessions) != 0 {
		t.Fatalf("asked %d, wrote %d, opened %d", len(h.prompt.asked), len(h.trust.written), len(h.client.sessions))
	}

	exempt := newHarness(t, nil)
	exempt.trust.data = recorded(t, "declared", "192.0.2.10", 2222, otherKey)
	_, err = exec(t, exempt, "host", machine.SSHOptions{}, "true")
	if reported := diagnostics.Of(err); len(reported) != 1 || reported[0].Code != "trust.identity" ||
		!strings.Contains(reported[0].Message, "declared no longer uses this context's SSH trust (declares an explicit knownHostsRef)") ||
		reported[0].Remediation != "drop declared from the input with bootwright context update --name lab --input-dir <dir>, repeat this command, then restore declared the same way; "+
			"this needs a context with no incomplete operation and an input in which no other object references declared, "+
			"and after a completed apply the next apply no longer settles: it refuses the changed input until a destroy" {
		t.Fatalf("a pin held by a Machine that no longer uses the store gave %+v", reported)
	}
	if len(exempt.prompt.asked) != 0 || len(exempt.trust.written) != 0 || len(exempt.client.sessions) != 0 {
		t.Fatalf("asked %d, wrote %d, opened %d", len(exempt.prompt.asked), len(exempt.trust.written), len(exempt.client.sessions))
	}
}

// Every remedy and next step that names a command names the context it acts
// on, so an operator with another context selected never acts on that one.
func TestEveryHostKeyRemedyNamesTheContext(t *testing.T) {
	for _, test := range []struct {
		name   string
		object string
		adjust func(*harness)
		want   string
	}{
		{"unknown name", "absent", nil, "bootwright machine list --context lab"},
		{"recorded endpoint mismatch", "host", func(h *harness) {
			h.trust.data = recorded(t, "host", "192.0.2.10", 22, publicKey)
		}, "bootwright machine trust --context lab --machines host --replace host"},
		{"non-interactive first use", "host", func(h *harness) {
			h.service.options.Terminal = func() (bool, error) { return false, nil }
		}, "bootwright machine trust --context lab --machines host"},
	} {
		t.Run(test.name, func(t *testing.T) {
			h := newHarness(t, nil)
			if test.adjust != nil {
				test.adjust(h)
			}
			_, err := exec(t, h, test.object, machine.SSHOptions{}, "true")
			if reported := diagnostics.Of(err); len(reported) != 1 || !strings.Contains(reported[0].Remediation, test.want) {
				t.Fatalf("refusal = %+v (%v), want a remedy naming %q", reported, err, test.want)
			}
		})
	}
	t.Run("password advisory", func(t *testing.T) {
		h := newHarness(t, nil)
		if _, err := exec(t, h, "secured", machine.SSHOptions{}, "true"); err != nil {
			t.Fatal(err)
		}
		if notice := h.errStream.String(); !strings.Contains(notice, "bootwright secret show --context lab --name admin-password --part password") {
			t.Fatalf("advisory = %q", notice)
		}
	})
}
