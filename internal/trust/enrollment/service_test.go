package enrollment

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/trust"
)

const (
	publicKey = "AAAAC3NzaC1lZDI1NTE5AAAAICG+RadYRVwHqO7OQsOpyh8SDf1tbTQ8PUmgn7UEmou/"
	otherKey  = "AAAAC3NzaC1lZDI1NTE5AAAAIMEyPr7qCaU9aM6pIjE3rMbT4dQK3S3dJH8vMWB0ZBQr"
)

func object(name string, spec api.Value) api.Object {
	return api.NewObject(api.Machine, name, api.MapValue(), spec)
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
	network := func(address string) api.Value {
		return m("addresses", list(m("name", "ssh", "address", address)))
	}
	return api.NewCatalog([]api.Object{
		object("node-a", m("os", m("provided", true), "network", network("192.0.2.10/24"),
			"access", m("ssh", m("addressRef", "ssh", "user", "admin", "auth", m("operatorIdentity", m()))))),
		object("node-b", m("os", m("provided", true), "network", network("192.0.2.11/24"),
			"access", m("ssh", m("addressRef", "ssh", "user", "admin", "port", api.IntegerValue("2222"),
				"auth", m("operatorIdentity", m()))))),
		object("installed", m("os", m("provided", false, "installProfileRef", "rhel"), "network", network("192.0.2.12/24"),
			"access", m("ssh", m("addressRef", "ssh", "user", "bootwright", "auth", m("privateKeyRef", "fleet"))))),
		object("declared", m("os", m("provided", true), "network", network("192.0.2.13/24"),
			"access", m("ssh", m("addressRef", "ssh", "user", "admin", "knownHostsRef", "pinned",
				"auth", m("operatorIdentity", m()))))),
		object("controller", m("os", m("provided", true), "access", m("local", true))),
	})
}

type stateSource struct{}

func (stateSource) RenderEffective(context.Context, compilation.EffectiveRequest) (*compilation.EffectiveResult, error) {
	return &compilation.EffectiveResult{Effective: catalog()}, nil
}

type store struct {
	data    []byte
	written [][]byte
}

func (s *store) ReadHostKeys(context.Context, string) ([]byte, error) { return s.data, nil }

func (s *store) ReplaceHostKeys(_ context.Context, _ string, data, expected []byte) error {
	if string(expected) != string(s.data) {
		return errors.New("expectation")
	}
	s.written = append(s.written, data)
	s.data = data
	return nil
}

type observer struct {
	keys     map[string]trust.HostKey
	fail     map[string]error
	observed []string
}

func (o *observer) Observe(_ context.Context, address string, _ int) (trust.HostKey, error) {
	o.observed = append(o.observed, address)
	if err, ok := o.fail[address]; ok {
		return trust.HostKey{}, err
	}
	if key, ok := o.keys[address]; ok {
		return key, nil
	}
	return trust.HostKey{Type: "ssh-ed25519", PublicKey: publicKey}, nil
}

// events is the one ordered log the presenter and the prompt share, so a test
// sees which of them an operator met first.
type events struct{ log []string }

type prompt struct {
	asked   int
	decline error
	events  *events
}

func (p *prompt) Confirm(context.Context, string, string) error {
	p.asked++
	p.events.log = append(p.events.log, "confirm")
	return p.decline
}

type presenter struct {
	shown  []Report
	fail   error
	events *events
}

func (p *presenter) PresentTrustPlan(_ context.Context, report Report) error {
	p.shown = append(p.shown, report)
	p.events.log = append(p.events.log, "present")
	return p.fail
}

type harness struct {
	service   Service
	trust     *store
	observer  *observer
	prompt    *prompt
	presenter *presenter
	events    *events
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	log := &events{}
	h := &harness{
		trust: &store{}, observer: &observer{keys: map[string]trust.HostKey{}, fail: map[string]error{}},
		prompt: &prompt{events: log}, presenter: &presenter{events: log}, events: log,
	}
	h.service = New(stateSource{}, h.trust, func(context.Context) (string, error) { return "lab", nil }, Options{
		Observer: h.observer, Confirmer: h.prompt, Presenter: h.presenter,
		Clock: func() time.Time { return time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC) },
	})
	return h
}

func enroll(t *testing.T, h *harness, request EnrollRequest) (*Report, error) {
	t.Helper()
	return h.service.Enroll(context.Background(), request)
}

func code(t *testing.T, err error) string {
	t.Helper()
	reported := diagnostics.Of(err)
	if len(reported) == 0 {
		t.Fatalf("error carries no diagnostic: %v", err)
	}
	return reported[0].Code
}

func action(report *Report, name string) HostReport {
	for _, host := range report.Hosts {
		if host.Machine == name {
			return host
		}
	}
	return HostReport{}
}

// Every other source of proof outranks the store, so a Machine that has one is
// reported and left alone rather than observed.
func TestOnlyMachinesUsingContextTrustAreObserved(t *testing.T) {
	h := newHarness(t)
	report, err := enroll(t, h, EnrollRequest{SkipConfirmation: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Hosts) != 5 {
		t.Fatalf("hosts = %+v", report.Hosts)
	}
	for _, test := range []struct{ machine, reason string }{
		{"installed", "installation evidence"},
		{"declared", "knownHostsRef"},
		{"controller", "locally"},
	} {
		host := action(report, test.machine)
		if host.Action != ActionSkip || !strings.Contains(host.Reason, test.reason) {
			t.Fatalf("%s = %+v", test.machine, host)
		}
	}
	if strings.Join(h.observer.observed, ",") != "192.0.2.10,192.0.2.11" {
		t.Fatalf("observed %v", h.observer.observed)
	}
	if report.Recorded != 2 || len(h.trust.written) != 1 {
		t.Fatalf("recorded = %d writes = %d", report.Recorded, len(h.trust.written))
	}
	records, err := trust.Decode(h.trust.data)
	if err != nil {
		t.Fatal(err)
	}
	recorded, found := records.Find("node-b")
	if !found || recorded.Port != 2222 || recorded.Source != trust.SourceEnrollment {
		t.Fatalf("record = %+v", recorded)
	}
}

func TestAnUnchangedKeyIsReusedAndNothingIsWritten(t *testing.T) {
	h := newHarness(t)
	if _, err := enroll(t, h, EnrollRequest{SkipConfirmation: true}); err != nil {
		t.Fatal(err)
	}
	second := newHarness(t)
	second.trust.data = h.trust.data
	report, err := enroll(t, second, EnrollRequest{SkipConfirmation: true})
	if err != nil {
		t.Fatal(err)
	}
	if action(report, "node-a").Action != ActionReuse || report.Pending != 0 || report.Recorded != 0 {
		t.Fatalf("report = %+v", report)
	}
	if len(second.trust.written) != 0 || second.prompt.asked != 0 {
		t.Fatal("an unchanged enrollment wrote or asked")
	}
}

// Superseding a key is the one deliberate way a changed host is accepted.
func TestAChangedKeyRefusesWithoutAnExplicitReTrust(t *testing.T) {
	h := newHarness(t)
	if _, err := enroll(t, h, EnrollRequest{SkipConfirmation: true}); err != nil {
		t.Fatal(err)
	}
	h.observer.keys["192.0.2.10"] = trust.HostKey{Type: "ssh-ed25519", PublicKey: otherKey}
	_, err := enroll(t, h, EnrollRequest{SkipConfirmation: true})
	if err == nil || code(t, err) != "trust.identity" {
		t.Fatalf("err = %v", err)
	}
	if reported := diagnostics.Of(err); !strings.Contains(reported[0].Remediation, "--replace node-a") {
		t.Fatalf("remediation = %q", reported[0].Remediation)
	}
	if len(h.trust.written) != 1 {
		t.Fatal("a changed key was recorded")
	}

	report, err := enroll(t, h, EnrollRequest{Replace: []string{"node-a"}, SkipConfirmation: true})
	if err != nil {
		t.Fatal(err)
	}
	host := action(report, "node-a")
	if host.Action != ActionReplace || host.PreviousFingerprint == "" || host.PreviousFingerprint == host.Fingerprint {
		t.Fatalf("host = %+v", host)
	}
	records, _ := trust.Decode(h.trust.data)
	recorded, _ := records.Find("node-a")
	if recorded.PublicKey != otherKey {
		t.Fatalf("record = %+v", recorded)
	}
}

func TestADryRunReportsThePlanAndWritesNothing(t *testing.T) {
	h := newHarness(t)
	report, err := enroll(t, h, EnrollRequest{DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if !report.DryRun || report.Pending != 2 || report.Recorded != 0 {
		t.Fatalf("report = %+v", report)
	}
	if len(h.trust.written) != 0 || h.prompt.asked != 0 {
		t.Fatal("a dry run wrote or asked")
	}
}

func TestPendingWritesAreConfirmedUnlessAnswered(t *testing.T) {
	h := newHarness(t)
	if _, err := enroll(t, h, EnrollRequest{}); err != nil {
		t.Fatal(err)
	}
	if h.prompt.asked != 1 {
		t.Fatalf("asked = %d", h.prompt.asked)
	}
	declined := newHarness(t)
	declined.prompt.decline = errors.New("declined")
	if _, err := enroll(t, declined, EnrollRequest{}); err == nil {
		t.Fatal("a declined enrollment recorded keys")
	}
	if len(declined.trust.written) != 0 {
		t.Fatal("a declined enrollment wrote")
	}
	skipped := newHarness(t)
	if _, err := enroll(t, skipped, EnrollRequest{SkipConfirmation: true}); err != nil {
		t.Fatal(err)
	}
	if skipped.prompt.asked != 0 {
		t.Fatal("--yes still asked")
	}
}

// What an operator confirms is the one evaluated report this enrollment
// records: every key it adds, and the key a replacement supersedes.
func TestThePlanIsPresentedBeforeItIsConfirmed(t *testing.T) {
	h := newHarness(t)
	if _, err := enroll(t, h, EnrollRequest{Machines: []string{"node-a"}, SkipConfirmation: true}); err != nil {
		t.Fatal(err)
	}
	h.observer.keys["192.0.2.10"] = trust.HostKey{Type: "ssh-ed25519", PublicKey: otherKey}
	report, err := enroll(t, h, EnrollRequest{Replace: []string{"node-a"}})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(h.events.log, []string{"present", "confirm"}) {
		t.Fatalf("events = %v, want the plan before its confirmation", h.events.log)
	}
	if len(h.presenter.shown) != 1 || !report.Presented {
		t.Fatalf("presented %d plans, report presented = %t", len(h.presenter.shown), report.Presented)
	}
	shown := h.presenter.shown[0]
	replaced := trust.HostKey{Type: "ssh-ed25519", PublicKey: otherKey}.Fingerprint()
	original := trust.HostKey{Type: "ssh-ed25519", PublicKey: publicKey}.Fingerprint()
	if shown.Pending != 2 || !reflect.DeepEqual(shown.Hosts, report.Hosts) {
		t.Fatalf("presented %+v, recorded %+v", shown, report)
	}
	if host := action(&shown, "node-a"); host.Action != ActionReplace || host.Fingerprint != replaced || host.PreviousFingerprint != original ||
		host.PreviousAddress != "" || host.PreviousPort != 0 {
		t.Fatalf("replacement = %+v", host)
	}
	if host := action(&shown, "node-b"); host.Action != ActionAdd || host.Fingerprint != original {
		t.Fatalf("addition = %+v", host)
	}
	if len(h.trust.written) != 2 {
		t.Fatalf("writes = %d", len(h.trust.written))
	}
}

func TestADeclinedPlanRecordsNothing(t *testing.T) {
	h := newHarness(t)
	h.prompt.decline = errors.New("declined")
	if _, err := enroll(t, h, EnrollRequest{}); err == nil {
		t.Fatal("a declined plan was recorded")
	}
	if len(h.presenter.shown) != 1 || len(h.trust.written) != 0 {
		t.Fatalf("presented %d plans, wrote %d", len(h.presenter.shown), len(h.trust.written))
	}
}

// A plan that cannot be shown is never confirmed, so an operator never
// accepts a fingerprint they did not see.
func TestAPlanThatCannotBeShownIsNeverConfirmed(t *testing.T) {
	h := newHarness(t)
	closed := errors.New("closed")
	h.presenter.fail = closed
	if _, err := enroll(t, h, EnrollRequest{}); !errors.Is(err, closed) {
		t.Fatalf("err = %v", err)
	}
	if !slices.Equal(h.events.log, []string{"present"}) || h.prompt.asked != 0 || len(h.trust.written) != 0 {
		t.Fatalf("events = %v, asked %d, wrote %d", h.events.log, h.prompt.asked, len(h.trust.written))
	}
}

func TestNothingIsPresentedWithoutAPrompt(t *testing.T) {
	recorded := newHarness(t)
	if _, err := enroll(t, recorded, EnrollRequest{SkipConfirmation: true}); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name    string
		data    []byte
		request EnrollRequest
	}{
		{"--yes", nil, EnrollRequest{SkipConfirmation: true}},
		{"--dry-run", nil, EnrollRequest{DryRun: true}},
		{"nothing pending", recorded.trust.data, EnrollRequest{}},
	} {
		t.Run(test.name, func(t *testing.T) {
			h := newHarness(t)
			h.trust.data = test.data
			report, err := enroll(t, h, test.request)
			if err != nil {
				t.Fatal(err)
			}
			if len(h.presenter.shown) != 0 || report.Presented || h.prompt.asked != 0 {
				t.Fatalf("presented %d plans, report presented = %t, asked %d", len(h.presenter.shown), report.Presented, h.prompt.asked)
			}
		})
	}
}

// A prompt the operator could answer without seeing the plan is refused
// rather than asked blind.
func TestAPromptWithoutAPresenterRefuses(t *testing.T) {
	h := newHarness(t)
	h.service.options.Presenter = nil
	_, err := enroll(t, h, EnrollRequest{})
	if err == nil || code(t, err) != "trust.identity" {
		t.Fatalf("err = %v", err)
	}
	if h.prompt.asked != 0 || len(h.trust.written) != 0 {
		t.Fatalf("asked %d, wrote %d", h.prompt.asked, len(h.trust.written))
	}
}

func TestAnUnchangedKeyAtAMovedEndpointNamesWhereItIsTrusted(t *testing.T) {
	h := newHarness(t)
	key := trust.HostKey{Type: "ssh-ed25519", PublicKey: publicKey}
	records := trust.Store{FormatVersion: trust.FormatVersion}
	records.Upsert(trust.Record{
		Machine: "node-a", Address: "192.0.2.99", Port: 22, KeyType: key.Type, PublicKey: key.PublicKey,
		Fingerprint: key.Fingerprint(), Source: trust.SourceEnrollment,
	})
	data, err := records.Encode()
	if err != nil {
		t.Fatal(err)
	}
	h.trust.data = data
	_, err = enroll(t, h, EnrollRequest{Machines: []string{"node-a"}, SkipConfirmation: true})
	if err == nil || code(t, err) != "trust.identity" {
		t.Fatalf("err = %v", err)
	}
	reported := diagnostics.Of(err)[0]
	for _, want := range []string{"unchanged", key.Fingerprint(), "192.0.2.99", "192.0.2.10"} {
		if !strings.Contains(reported.Message, want) {
			t.Fatalf("message %q does not name %q", reported.Message, want)
		}
	}
	if strings.Contains(reported.Message, "changed from") || !strings.Contains(reported.Remediation, "--context lab --replace node-a") {
		t.Fatalf("diagnostic = %q, remediation %q", reported.Message, reported.Remediation)
	}
	if len(h.trust.written) != 0 {
		t.Fatal("a moved endpoint was recorded without a re-trust")
	}

	report, err := enroll(t, h, EnrollRequest{Machines: []string{"node-a"}, Replace: []string{"node-a"}, SkipConfirmation: true})
	if err != nil {
		t.Fatal(err)
	}
	host := action(report, "node-a")
	if host.Action != ActionReplace || host.PreviousAddress != "192.0.2.99" || host.PreviousPort != 22 || host.PreviousFingerprint != host.Fingerprint {
		t.Fatalf("host = %+v", host)
	}
}

func TestASelectionNamesOnlyTheMachinesItObserves(t *testing.T) {
	h := newHarness(t)
	report, err := enroll(t, h, EnrollRequest{Machines: []string{"node-b"}, SkipConfirmation: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Hosts) != 1 || report.Hosts[0].Machine != "node-b" {
		t.Fatalf("hosts = %+v", report.Hosts)
	}
	if strings.Join(h.observer.observed, ",") != "192.0.2.11" {
		t.Fatalf("observed %v", h.observer.observed)
	}
}

func TestAnUnusableSelectionOrReTrustRefuses(t *testing.T) {
	for _, test := range []struct {
		name    string
		request EnrollRequest
		want    string
	}{
		{"unknown machine", EnrollRequest{Machines: []string{"absent"}}, "access.target"},
		{"unknown replacement", EnrollRequest{Replace: []string{"absent"}}, "access.target"},
		{"replacement outside the selection", EnrollRequest{Machines: []string{"node-a"}, Replace: []string{"node-b"}}, "cli.usage"},
		{"replacement that uses no context trust", EnrollRequest{Replace: []string{"installed"}}, "access.unavailable"},
	} {
		t.Run(test.name, func(t *testing.T) {
			h := newHarness(t)
			request := test.request
			request.SkipConfirmation = true
			_, err := enroll(t, h, request)
			if err == nil || code(t, err) != test.want {
				t.Fatalf("err = %v, want %s", err, test.want)
			}
			if len(h.observer.observed) != 0 || len(h.trust.written) != 0 {
				t.Fatal("a refused request contacted a host or wrote")
			}
		})
	}
}

// An endpoint that could not be read is unknown, so the enrollment fails
// rather than recording a partial view of the fleet.
func TestAnUnreachableEndpointFailsTheEnrollment(t *testing.T) {
	h := newHarness(t)
	h.observer.fail["192.0.2.11"] = errors.New("no SSH host key was observed")
	if _, err := enroll(t, h, EnrollRequest{SkipConfirmation: true}); err == nil {
		t.Fatal("an unreachable endpoint produced a recorded enrollment")
	}
	if len(h.trust.written) != 0 {
		t.Fatal("an incomplete enrollment wrote")
	}
}

func TestAnEnrollmentPublishesAgainstTheRecordsItRead(t *testing.T) {
	h := newHarness(t)
	records := trust.Store{FormatVersion: trust.FormatVersion}
	records.Upsert(trust.Record{
		Machine: "elsewhere", Address: "198.51.100.9", Port: 22,
		KeyType: "ssh-ed25519", PublicKey: otherKey, Source: trust.SourceEnrollment,
	})
	data, err := records.Encode()
	if err != nil {
		t.Fatal(err)
	}
	h.trust.data = data
	if _, err := enroll(t, h, EnrollRequest{SkipConfirmation: true}); err != nil {
		t.Fatal(err)
	}
	after, err := trust.Decode(h.trust.data)
	if err != nil {
		t.Fatal(err)
	}
	if _, found := after.Find("elsewhere"); !found || len(after.Hosts) != 3 {
		t.Fatalf("records = %+v", after.Hosts)
	}
}

func TestAnUnconfiguredServiceIsUnavailable(t *testing.T) {
	if _, err := (Service{}).Enroll(context.Background(), EnrollRequest{}); err == nil {
		t.Fatal("an unconfigured service enrolled")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := (Service{}).Enroll(ctx, EnrollRequest{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled result = %v", err)
	}
}

// storedRecords encodes records as the store holds them.
func storedRecords(t *testing.T, records ...trust.Record) []byte {
	t.Helper()
	store := trust.Store{FormatVersion: trust.FormatVersion}
	for _, record := range records {
		store.Upsert(record)
	}
	data, err := store.Encode()
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func storedRecord(machine, address string, port int, publicKey string) trust.Record {
	key := trust.HostKey{Type: "ssh-ed25519", PublicKey: publicKey}
	return trust.Record{
		Machine: machine, Address: address, Port: port, KeyType: key.Type, PublicKey: key.PublicKey,
		Fingerprint: key.Fingerprint(), Source: trust.SourceEnrollment, Recorded: "2026-09-16T00:00:00Z",
	}
}

// A Machine this context no longer declares keeps no claim on an address the
// context reassigned: the confirmed write that trusts node-a there removes the
// stale record, and both the dry run and the plan show that removal.
func TestAStaleRecordOfAnUndeclaredMachineIsReplacedInTheConfirmedWrite(t *testing.T) {
	stale := storedRecord("retired", "192.0.2.10", 22, otherKey)
	observed := trust.HostKey{Type: "ssh-ed25519", PublicKey: publicKey}
	dry := newHarness(t)
	dry.trust.data = storedRecords(t, stale)
	report, err := enroll(t, dry, EnrollRequest{Machines: []string{"node-a"}, DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if report.Pending != 2 || report.Recorded != 0 || len(dry.trust.written) != 0 || dry.prompt.asked != 0 {
		t.Fatalf("dry run = %+v, wrote %d, asked %d", report, len(dry.trust.written), dry.prompt.asked)
	}
	want := []HostReport{
		{Machine: "node-a", Address: "192.0.2.10", Port: 22, Action: ActionAdd, KeyType: "ssh-ed25519", Fingerprint: observed.Fingerprint()},
		{
			Machine: "retired", Address: "192.0.2.10", Port: 22, Action: ActionRemove, KeyType: "ssh-ed25519",
			Fingerprint: stale.Fingerprint, Reason: "no longer declared; node-a now uses its address",
		},
	}
	if !reflect.DeepEqual(report.Hosts, want) {
		t.Fatalf("hosts = %+v, want %+v", report.Hosts, want)
	}

	confirmed := newHarness(t)
	confirmed.trust.data = storedRecords(t, stale)
	report, err = enroll(t, confirmed, EnrollRequest{Machines: []string{"node-a"}})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(confirmed.events.log, []string{"present", "confirm"}) || len(confirmed.presenter.shown) != 1 {
		t.Fatalf("events = %v", confirmed.events.log)
	}
	if !reflect.DeepEqual(confirmed.presenter.shown[0].Hosts, want) {
		t.Fatalf("presented %+v, want the removal before the prompt", confirmed.presenter.shown[0].Hosts)
	}
	if report.Recorded != 2 || len(confirmed.trust.written) != 1 {
		t.Fatalf("recorded %d, wrote %d", report.Recorded, len(confirmed.trust.written))
	}
	after, err := trust.Decode(confirmed.trust.data)
	if err != nil {
		t.Fatal(err)
	}
	if len(after.Hosts) != 1 || after.Hosts[0].Machine != "node-a" || after.Hosts[0].PublicKey != publicKey {
		t.Fatalf("records = %+v", after.Hosts)
	}
}

// A record of a Machine the context still declares is never removed, so a key
// that would diverge from it refuses before anything is shown or asked, in a
// dry run too, and names the re-trust of that Machine in this context.
func TestADivergentPinRefusesBeforeTheDryRunReturns(t *testing.T) {
	for _, request := range []EnrollRequest{
		{Machines: []string{"node-a"}, DryRun: true},
		{Machines: []string{"node-a"}},
	} {
		h := newHarness(t)
		h.trust.data = storedRecords(t, storedRecord("node-b", "192.0.2.10", 22, otherKey))
		_, err := enroll(t, h, request)
		if err == nil || code(t, err) != "trust.identity" {
			t.Fatalf("dry run %t: err = %v", request.DryRun, err)
		}
		reported := diagnostics.Of(err)[0]
		if !strings.Contains(reported.Message, "192.0.2.10") || !strings.Contains(reported.Message, "node-a") ||
			!strings.Contains(reported.Message, "node-b") ||
			!strings.Contains(reported.Remediation, "--context lab --machines node-b --replace node-b") {
			t.Fatalf("dry run %t: refusal = %+v", request.DryRun, reported)
		}
		if h.prompt.asked != 0 || len(h.presenter.shown) != 0 || len(h.trust.written) != 0 {
			t.Fatalf("dry run %t: asked %d, presented %d, wrote %d", request.DryRun, h.prompt.asked, len(h.presenter.shown), len(h.trust.written))
		}
	}
}

// A record of a declared Machine that no longer uses this context's trust is
// read by nothing, so the write that trusts node-a at its endpoint removes it
// as a remove row naming why, with no input edit (D117).
func TestATakeoverRemovesTheRecordOfAMachineThatNoLongerUsesTheStore(t *testing.T) {
	h := newHarness(t)
	h.trust.data = storedRecords(t, storedRecord("declared", "192.0.2.10", 22, otherKey))
	report, err := enroll(t, h, EnrollRequest{Machines: []string{"node-a"}, SkipConfirmation: true})
	if err != nil {
		t.Fatalf("the takeover refused: %v", err)
	}
	removed := action(report, "declared")
	if removed.Action != ActionRemove ||
		removed.Reason != "declares an explicit knownHostsRef, so it no longer uses this context's SSH trust; node-a now uses its address" ||
		report.Recorded != 2 {
		t.Fatalf("report = %+v", report)
	}
	after, err := trust.Decode(h.trust.data)
	if err != nil {
		t.Fatal(err)
	}
	if len(after.Hosts) != 1 || after.Hosts[0].Machine != "node-a" {
		t.Fatalf("records = %+v", after.Hosts)
	}
}

// The controller Machine is reached locally, so its record is read by nothing
// either, and the takeover removes it; enrollment reads no lifecycle record.
func TestATakeoverRemovesTheControllerMachinesRecord(t *testing.T) {
	h := newHarness(t)
	h.trust.data = storedRecords(t, storedRecord("controller", "192.0.2.10", 22, otherKey))
	report, err := enroll(t, h, EnrollRequest{Machines: []string{"node-a"}, SkipConfirmation: true})
	if err != nil {
		t.Fatalf("the takeover refused: %v", err)
	}
	if removed := action(report, "controller"); removed.Action != ActionRemove ||
		removed.Reason != "reached locally, so it no longer uses this context's SSH trust; node-a now uses its address" || report.Recorded != 2 {
		t.Fatalf("report = %+v", report)
	}
	after, err := trust.Decode(h.trust.data)
	if err != nil {
		t.Fatal(err)
	}
	if len(after.Hosts) != 1 || after.Hosts[0].Machine != "node-a" {
		t.Fatalf("records = %+v", after.Hosts)
	}
}

// The removal of an exempt Machine's record is part of the plan: shown before
// the prompt, written only by the confirmed write, and never by a declined
// prompt or a dry run.
func TestAnExemptRecordsRemovalIsShownAndOnlyTheConfirmedWriteRecordsIt(t *testing.T) {
	stored := storedRecord("declared", "192.0.2.10", 22, otherKey)
	declined := newHarness(t)
	declined.trust.data = storedRecords(t, stored)
	declined.prompt.decline = errors.New("declined")
	if _, err := enroll(t, declined, EnrollRequest{Machines: []string{"node-a"}}); err == nil {
		t.Fatal("a declined takeover was recorded")
	}
	if !slices.Equal(declined.events.log, []string{"present", "confirm"}) || len(declined.trust.written) != 0 {
		t.Fatalf("events = %v, wrote %d", declined.events.log, len(declined.trust.written))
	}
	if shown := declined.presenter.shown[0]; shown.Pending != 2 || action(&shown, "declared").Action != ActionRemove {
		t.Fatalf("presented %+v, want the removal before the prompt", shown)
	}

	dry := newHarness(t)
	dry.trust.data = storedRecords(t, stored)
	report, err := enroll(t, dry, EnrollRequest{Machines: []string{"node-a"}, DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if action(report, "declared").Action != ActionRemove || report.Pending != 2 || report.Recorded != 0 ||
		len(dry.trust.written) != 0 || dry.prompt.asked != 0 {
		t.Fatalf("dry run = %+v, wrote %d, asked %d", report, len(dry.trust.written), dry.prompt.asked)
	}

	confirmed := newHarness(t)
	confirmed.trust.data = storedRecords(t, stored)
	report, err = enroll(t, confirmed, EnrollRequest{Machines: []string{"node-a"}})
	if err != nil {
		t.Fatal(err)
	}
	if report.Recorded != 2 || len(confirmed.trust.written) != 1 {
		t.Fatalf("recorded %d, wrote %d", report.Recorded, len(confirmed.trust.written))
	}
}

// Every remedy that names a command names the context it acts on, so an
// operator with another context selected never re-trusts or lists the wrong
// one.
func TestEveryHostKeyRemedyNamesTheContext(t *testing.T) {
	changed := newHarness(t)
	if _, err := enroll(t, changed, EnrollRequest{Machines: []string{"node-a"}, SkipConfirmation: true}); err != nil {
		t.Fatal(err)
	}
	changed.observer.keys["192.0.2.10"] = trust.HostKey{Type: "ssh-ed25519", PublicKey: otherKey}
	for _, test := range []struct {
		name    string
		h       *harness
		request EnrollRequest
		want    string
	}{
		{"unknown Machine", newHarness(t), EnrollRequest{Machines: []string{"absent"}}, "bootwright machine list --context lab"},
		{"unknown replacement", newHarness(t), EnrollRequest{Replace: []string{"absent"}}, "bootwright machine list --context lab"},
		{"changed key", changed, EnrollRequest{Machines: []string{"node-a"}}, "bootwright machine trust --context lab --machines node-a --replace node-a"},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := enroll(t, test.h, test.request)
			if reported := diagnostics.Of(err); len(reported) != 1 || !strings.Contains(reported[0].Remediation, test.want) {
				t.Fatalf("refusal = %+v (%v), want a remedy naming %q", reported, err, test.want)
			}
		})
	}
}
