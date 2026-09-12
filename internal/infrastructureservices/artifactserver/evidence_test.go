package artifactserver

import (
	"encoding/json"
	"strings"
	"testing"
)

func fixtureRequest(t *testing.T) Request {
	t.Helper()
	requests, err := Requests(catalogOf(bastion(), artifactServer()), "bastion", testContext)
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
