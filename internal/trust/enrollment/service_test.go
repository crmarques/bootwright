package enrollment

import (
	"context"
	"errors"
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

type prompt struct {
	asked   int
	decline error
}

func (p *prompt) Confirm(context.Context, string, string) error {
	p.asked++
	return p.decline
}

type harness struct {
	service  Service
	trust    *store
	observer *observer
	prompt   *prompt
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	h := &harness{trust: &store{}, observer: &observer{keys: map[string]trust.HostKey{}, fail: map[string]error{}}, prompt: &prompt{}}
	h.service = New(stateSource{}, h.trust, func(context.Context) (string, error) { return "lab", nil }, Options{
		Observer: h.observer, Confirmer: h.prompt,
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
