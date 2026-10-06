package access

import (
	"errors"
	"strings"
	"testing"

	diag "github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/trust"
)

func recorded(t *testing.T, name, address string, port int, key string) []byte {
	t.Helper()
	parsed, err := trust.ParseAuthorizedKey("ssh-ed25519 " + key)
	if err != nil {
		t.Fatal(err)
	}
	records := trust.Store{FormatVersion: trust.FormatVersion}
	records.Upsert(trust.Record{
		Machine: name, Address: address, Port: port,
		KeyType: parsed.Type, PublicKey: parsed.PublicKey, Fingerprint: parsed.Fingerprint(),
		Source: trust.SourceEnrollment, Recorded: "2026-09-16T00:00:00Z",
	})
	data, err := records.Encode()
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// The Machine's own declared Secret outranks everything else, and it is bound
// to the exact address and port desired state names.
func TestADeclaredHostKeyOutranksEveryOtherSource(t *testing.T) {
	h := newHarness(t, nil)
	h.trust.data = recorded(t, "declared", "192.0.2.10", 22, otherKey)
	if _, err := exec(t, h, "declared", machine.SSHOptions{}, "true"); err != nil {
		t.Fatal(err)
	}
	if got := h.client.sessions[0].HostKey.PublicKey; got != publicKey {
		t.Fatalf("pinned %q, want the declared key", got)
	}
	if h.observer.probes != 0 {
		t.Fatal("a declared host key was observed anyway")
	}
}

func TestADeclaredHostKeyForAnotherTargetRefuses(t *testing.T) {
	h := newHarness(t, nil)
	h.lender.material["declared-host-key"] = secrets.NewMaterial(map[secrets.Part][]byte{
		secrets.ValuePart: []byte("elsewhere.test ssh-ed25519 " + publicKey + "\n"),
	})
	if _, err := exec(t, h, "declared", machine.SSHOptions{}, "true"); err == nil {
		t.Fatal("a key declared for another host was pinned")
	} else if code(t, err) != "trust.identity" {
		t.Fatalf("code = %q", code(t, err))
	}
	if len(h.client.sessions) != 0 {
		t.Fatal("a session opened against an unproved host")
	}
}

// An installed Machine is proved against its own installation, so it consults
// no trust store and never needs a trust command.
func TestAnInstalledMachineIsProvedByItsOwnInstallation(t *testing.T) {
	h := newHarness(t, nil)
	h.trust.data = recorded(t, "guest", "192.0.2.10", 22, otherKey)
	if _, err := exec(t, h, "guest", machine.SSHOptions{}, "true"); err != nil {
		t.Fatal(err)
	}
	if got := h.client.sessions[0].HostKey.PublicKey; got != publicKey {
		t.Fatalf("pinned %q, want the installed key", got)
	}
	if h.observer.probes != 0 || len(h.trust.written) != 0 {
		t.Fatal("an installed Machine consulted or wrote trust")
	}
}

// A Machine this context no longer owns proves nothing: a host answering on
// its address would otherwise be trusted on first sight.
func TestAnInstallationThisContextDoesNotOwnProvesNothing(t *testing.T) {
	h := newHarness(t, func(o *Options) { o.Ownership = owner{realized: false} })
	_, err := exec(t, h, "guest", machine.SSHOptions{}, "true")
	if err == nil || code(t, err) != "access.target" {
		t.Fatalf("err = %v", err)
	}
	if len(h.client.sessions) != 0 {
		t.Fatal("a session opened against an unowned installation")
	}
}

func TestAnInstallationThatProvedNoKeyRefuses(t *testing.T) {
	h := newHarness(t, func(o *Options) { o.Evidence = proof{found: false} })
	if _, err := exec(t, h, "guest", machine.SSHOptions{}, "true"); code(t, err) != "access.target" {
		t.Fatalf("code = %q", code(t, err))
	}
	moved := newHarness(t, func(o *Options) {
		parsed, _ := trust.ParseAuthorizedKey("ssh-ed25519 " + publicKey)
		o.Evidence = proof{evidence: machine.HostKeyEvidence{Address: "192.0.2.99", HostKey: parsed}, found: true}
	})
	if _, err := exec(t, moved, "guest", machine.SSHOptions{}, "true"); code(t, err) != "trust.identity" {
		t.Fatalf("code = %q", code(t, err))
	}
}

func TestARecordedHostKeyIsPinnedWithoutObserving(t *testing.T) {
	h := newHarness(t, nil)
	h.trust.data = recorded(t, "host", "192.0.2.10", 2222, publicKey)
	if _, err := exec(t, h, "host", machine.SSHOptions{}, "true"); err != nil {
		t.Fatal(err)
	}
	if got := h.client.sessions[0].HostKey.PublicKey; got != publicKey {
		t.Fatalf("pinned %q", got)
	}
	if h.observer.probes != 0 || len(h.trust.written) != 0 {
		t.Fatal("a recorded key was observed or rewritten")
	}
}

// A record made for another endpoint is not a proof of this one.
func TestARecordForAnotherEndpointRefuses(t *testing.T) {
	h := newHarness(t, nil)
	h.trust.data = recorded(t, "host", "192.0.2.10", 22, publicKey)
	_, err := exec(t, h, "host", machine.SSHOptions{}, "true")
	if err == nil || code(t, err) != "trust.identity" {
		t.Fatalf("err = %v", err)
	}
	if reported := remediations(t, err); !strings.Contains(reported, "machine trust --context lab --machines host --replace host") {
		t.Fatalf("remediation = %q", reported)
	}
}

// First use observes, shows the fingerprint, and records only what the
// operator explicitly accepted.
func TestFirstUseRecordsOnlyWhatTheOperatorConfirms(t *testing.T) {
	h := newHarness(t, nil)
	if _, err := exec(t, h, "host", machine.SSHOptions{}, "true"); err != nil {
		t.Fatal(err)
	}
	if h.observer.probes != 1 {
		t.Fatalf("probes = %d", h.observer.probes)
	}
	if len(h.prompt.asked) != 1 || h.prompt.asked[0] != "host [192.0.2.10]:2222 ssh-ed25519 "+fingerprint {
		t.Fatalf("prompt = %v", h.prompt.asked)
	}
	if len(h.trust.written) != 1 {
		t.Fatalf("writes = %d", len(h.trust.written))
	}
	records, err := trust.Decode(h.trust.written[0])
	if err != nil {
		t.Fatal(err)
	}
	record, found := records.Find("host")
	if !found || record.PublicKey != publicKey || record.Port != 2222 || record.Source != trust.SourceFirstUse {
		t.Fatalf("record = %+v", record)
	}
	if h.client.sessions[0].HostKey.PublicKey != publicKey {
		t.Fatal("the session did not pin the key just confirmed")
	}
}

func TestADeclinedFirstUseRecordsNothingAndOpensNothing(t *testing.T) {
	h := newHarness(t, nil)
	h.prompt.decline = errors.New("declined")
	if _, err := exec(t, h, "host", machine.SSHOptions{}, "true"); err == nil {
		t.Fatal("a declined key opened a session")
	}
	if len(h.trust.written) != 0 || len(h.client.sessions) != 0 {
		t.Fatal("a declined key was recorded or used")
	}
}

// Without a terminal to ask on there is no first use at all: the session
// refuses and names the command that records trust deliberately.
func TestWithoutATerminalAnUnprovedKeyRefuses(t *testing.T) {
	h := newHarness(t, func(o *Options) { o.Terminal = func() (bool, error) { return false, nil } })
	_, err := exec(t, h, "host", machine.SSHOptions{}, "true")
	if err == nil || code(t, err) != "trust.identity" {
		t.Fatalf("err = %v", err)
	}
	if h.observer.probes != 0 || len(h.trust.written) != 0 {
		t.Fatal("a non-interactive invocation observed or recorded a key")
	}
	if reported := remediations(t, err); !strings.Contains(reported, "machine trust --context lab --machines host") {
		t.Fatalf("remediation = %q", reported)
	}
}

func TestAnUnreachableEndpointIsUnknownRatherThanUntrusted(t *testing.T) {
	h := newHarness(t, nil)
	h.observer.err = errors.New("no SSH host key was observed")
	if _, err := exec(t, h, "host", machine.SSHOptions{}, "true"); err == nil {
		t.Fatal("an unreachable endpoint opened a session")
	}
	if len(h.trust.written) != 0 {
		t.Fatal("an unreachable endpoint was recorded")
	}
}

// A first-use record is published against the exact records it read, so a
// concurrent decision is not lost.
func TestFirstUsePublishesAgainstTheRecordsItRead(t *testing.T) {
	h := newHarness(t, nil)
	h.trust.data = recorded(t, "other", "192.0.2.50", 22, otherKey)
	if _, err := exec(t, h, "host", machine.SSHOptions{}, "true"); err != nil {
		t.Fatal(err)
	}
	records, err := trust.Decode(h.trust.data)
	if err != nil {
		t.Fatal(err)
	}
	if len(records.Hosts) != 2 {
		t.Fatalf("records = %+v", records.Hosts)
	}
	if _, found := records.Find("other"); !found {
		t.Fatal("first use replaced an unrelated record")
	}
}

func remediations(t *testing.T, err error) string {
	t.Helper()
	var out []string
	for _, reported := range diag.Of(err) {
		out = append(out, reported.Message+" "+reported.Remediation)
	}
	return strings.Join(out, "; ")
}

func TestASessionNeverOpensWithoutItsPinnedKey(t *testing.T) {
	h := newHarness(t, func(o *Options) { o.Trust = nil; o.Observer = nil; o.Confirmer = nil })
	if _, err := exec(t, h, "host", machine.SSHOptions{}, "true"); code(t, err) != "trust.identity" {
		t.Fatalf("code = %q", code(t, err))
	}
	if len(h.client.sessions) != 0 {
		t.Fatal("a session opened with no host-key source at all")
	}
}
