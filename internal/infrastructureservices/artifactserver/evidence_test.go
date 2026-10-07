package artifactserver

import (
	"encoding/json"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

func fixtureRequest(t *testing.T) Request {
	t.Helper()
	requests, err := Requests(catalogOf(controller(), artifactServer()), "controller", testContext)
	if err != nil {
		t.Fatal(err)
	}
	return requests[0]
}

func presenceEvidence(request Request, digest, fingerprint string) []byte {
	evidence := Evidence{
		Container: request.Image, ContentRoot: true, Postcondition: true, Request: digest, Unit: "active",
	}
	for _, target := range request.ProbeTargets() {
		listener := ListenerEvidence{
			Address: target.Address, Name: target.Name, Port: target.Port,
			Protocol: target.Protocol, Status: "HTTP/1.1 404 Not Found",
		}
		if target.Protocol == "https" {
			listener.Fingerprint = fingerprint
		}
		evidence.Listeners = append(evidence.Listeners, listener)
	}
	data, _ := json.Marshal(evidence)
	return data
}

func absenceEvidence(digest string) []byte {
	data, _ := json.Marshal(Evidence{Absent: true, Postcondition: true, Request: digest, Listeners: []ListenerEvidence{}})
	return data
}

const testDigest = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
const testFingerprint = "fedcba9876543210fedcba9876543210fedcba9876543210fedcba9876543210"

func TestPresenceEvidenceProvesTheExactFrozenService(t *testing.T) {
	request := fixtureRequest(t)
	if err := ValidatePresence(presenceEvidence(request, testDigest, testFingerprint), request, testDigest, testFingerprint); err != nil {
		t.Fatal(err)
	}
	mutate := func(change func(*Evidence)) []byte {
		var evidence Evidence
		_ = json.Unmarshal(presenceEvidence(request, testDigest, testFingerprint), &evidence)
		change(&evidence)
		data, _ := json.Marshal(evidence)
		return data
	}
	for name, change := range map[string]func(*Evidence){
		"other request":     func(e *Evidence) { e.Request = strings.Repeat("9", 64) },
		"no postcondition":  func(e *Evidence) { e.Postcondition = false },
		"absent":            func(e *Evidence) { e.Absent = true },
		"inactive unit":     func(e *Evidence) { e.Unit = "failed" },
		"other image":       func(e *Evidence) { e.Container = "registry.example.test/other@sha256:" + strings.Repeat("c", 64) },
		"no content root":   func(e *Evidence) { e.ContentRoot = false },
		"missing listener":  func(e *Evidence) { e.Listeners = e.Listeners[:1] },
		"extra listener":    func(e *Evidence) { e.Listeners = append(e.Listeners, e.Listeners[0]) },
		"other certificate": func(e *Evidence) { e.Listeners[1].Fingerprint = strings.Repeat("a", 64) },
		"http certificate":  func(e *Evidence) { e.Listeners[0].Fingerprint = testFingerprint },
		"no status line":    func(e *Evidence) { e.Listeners[0].Status = "connected" },
		"other port":        func(e *Evidence) { e.Listeners[0].Port = 9999 },
		"other address":     func(e *Evidence) { e.Listeners[0].Address = "203.0.113.1" },
	} {
		t.Run(name, func(t *testing.T) {
			if err := ValidatePresence(mutate(change), request, testDigest, testFingerprint); err == nil {
				t.Fatal("unproved evidence was accepted")
			}
		})
	}
}

func TestEvidenceMustBeBoundedAndClosed(t *testing.T) {
	request := fixtureRequest(t)
	for name, data := range map[string][]byte{
		"empty":         nil,
		"unknown field": []byte(`{"absent":false,"surprise":1}`),
		"trailing":      append(presenceEvidence(request, testDigest, testFingerprint), '{'),
		"a closing }":   append(presenceEvidence(request, testDigest, testFingerprint), '}'),
		"a closing ]":   append(presenceEvidence(request, testDigest, testFingerprint), ']'),
		"oversized":     []byte(`{"container":"` + strings.Repeat("x", maxEvidenceBytes) + `"}`),
	} {
		t.Run(name, func(t *testing.T) {
			if err := ValidatePresence(data, request, testDigest, testFingerprint); err == nil {
				t.Fatal("unbounded or open evidence was accepted")
			}
		})
	}
}

func TestAbsenceEvidenceProvesRemoval(t *testing.T) {
	if err := ValidateAbsence(absenceEvidence(testDigest), testDigest); err != nil {
		t.Fatal(err)
	}
	mutate := func(change func(*Evidence)) []byte {
		var evidence Evidence
		_ = json.Unmarshal(absenceEvidence(testDigest), &evidence)
		change(&evidence)
		data, _ := json.Marshal(evidence)
		return data
	}
	for name, change := range map[string]func(*Evidence){
		"not absent":       func(e *Evidence) { e.Absent = false },
		"no postcondition": func(e *Evidence) { e.Postcondition = false },
		"unit remains":     func(e *Evidence) { e.Unit = "active" },
		"container":        func(e *Evidence) { e.Container = "registry.example.test/x@sha256:" + strings.Repeat("d", 64) },
		"content root":     func(e *Evidence) { e.ContentRoot = true },
		"listener":         func(e *Evidence) { e.Listeners = []ListenerEvidence{{Name: "https"}} },
		"other request":    func(e *Evidence) { e.Request = strings.Repeat("7", 64) },
	} {
		t.Run(name, func(t *testing.T) {
			if err := ValidateAbsence(mutate(change), testDigest); err == nil {
				t.Fatal("unproved removal evidence was accepted")
			}
		})
	}
	request := fixtureRequest(t)
	if err := ValidateAbsence(presenceEvidence(request, testDigest, testFingerprint), testDigest); err == nil {
		t.Fatal("presence evidence proved removal")
	}
	if err := ValidatePresence(absenceEvidence(testDigest), request, testDigest, testFingerprint); err == nil {
		t.Fatal("absence evidence proved presence")
	}
}

// Readiness proves a listener only through an address it probes. A request an
// earlier build froze with a wildcard bind and no endpoint, or none for one of
// its listeners, names nothing an answer could prove, so such a server never
// reads as complete: everything present is a partial realization instead.
func TestPresenceRefusesAnUnprobedListener(t *testing.T) {
	httpsOnly := field("endpoints", api.ListValue(api.MapValue(text("name", "ip-https"), text("listenerRef", "https"), text("addressRef", "ip"))))
	for name, test := range map[string]struct {
		server api.Object
		want   string
	}{
		"no endpoint at all":       {artifactServer(text("bindAddress", "0.0.0.0"), field("endpoints", api.ListValue())), "the frozen artifact-server request names no address readiness can probe"},
		"a listener with no probe": {artifactServer(text("bindAddress", "::"), httpsOnly), "listener http has no address readiness can probe"},
	} {
		t.Run(name, func(t *testing.T) {
			requests, err := Requests(catalogOf(controller(), test.server), "controller", testContext)
			if err != nil {
				t.Fatal(err)
			}
			request := requests[0]
			var evidence Evidence
			if err := json.Unmarshal(presenceEvidence(request, testDigest, testFingerprint), &evidence); err != nil {
				t.Fatal(err)
			}
			if evidence.Listeners == nil {
				evidence.Listeners = []ListenerEvidence{}
			}
			data, err := json.Marshal(evidence)
			if err != nil {
				t.Fatal(err)
			}
			reported := diagnostics.Of(ValidatePresence(data, request, testDigest, testFingerprint))
			if len(reported) != 1 || reported[0].Code != "lifecycle.state" || reported[0].Message != test.want {
				t.Fatalf("presence = %#v, want the refusal %q", reported, test.want)
			}
			if err := ValidatePartial(data, request, testDigest, testFingerprint); err != nil {
				t.Fatalf("a present server readiness cannot prove was not partial: %v", err)
			}
		})
	}
}
