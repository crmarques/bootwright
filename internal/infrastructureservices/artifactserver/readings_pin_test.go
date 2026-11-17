package artifactserver

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

// Every evidence form an adapter could return is read by each verb's effect
// and by each check, and the readings are pinned, so moving the artifact
// server onto the managed service's rules shows every reading that moved.
func TestEveryArtifactServerEvidenceReadingIsPinned(t *testing.T) {
	material, fingerprint := issue(t, validOptions())
	call := execution(t, material)
	request, err := DecodeRequest(call.Block.Request)
	if err != nil {
		t.Fatal(err)
	}
	digest := call.Block.RequestDigest
	present := func(change func(*Evidence)) json.RawMessage {
		var evidence Evidence
		if err := json.Unmarshal(presenceEvidence(request, digest, fingerprint), &evidence); err != nil {
			t.Fatal(err)
		}
		change(&evidence)
		data, err := json.Marshal(evidence)
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	absent := func(change func(*Evidence)) json.RawMessage {
		evidence := Evidence{Absent: true, Postcondition: true, Request: digest, Listeners: []ListenerEvidence{}}
		change(&evidence)
		data, err := json.Marshal(evidence)
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	https := func(change func(*ListenerEvidence)) func(*Evidence) {
		return func(e *Evidence) {
			for index := range e.Listeners {
				if e.Listeners[index].Protocol == "https" {
					change(&e.Listeners[index])
				}
			}
		}
	}
	forms := map[string]json.RawMessage{
		"present":                          present(func(*Evidence) {}),
		"present, another request":         present(func(e *Evidence) { e.Request = strings.Repeat("9", 64) }),
		"present, no postcondition":        present(func(e *Evidence) { e.Postcondition = false }),
		"present, absent flag":             present(func(e *Evidence) { e.Absent = true }),
		"present, started before files":    present(func(e *Evidence) { e.StartedAfterFiles = false }),
		"present, stale and unclaimed":     present(func(e *Evidence) { e.StartedAfterFiles, e.Postcondition = false, false }),
		"present, unit failed":             present(func(e *Evidence) { e.Unit = "failed" }),
		"present, unit failed unclaimed":   present(func(e *Evidence) { e.Unit, e.Postcondition = "failed", false }),
		"present, another image":           present(func(e *Evidence) { e.Container = "registry.example.test/other:1" }),
		"present, no container":            present(func(e *Evidence) { e.Container = "" }),
		"present, no content root":         present(func(e *Evidence) { e.ContentRoot = false }),
		"present, no root unclaimed":       present(func(e *Evidence) { e.ContentRoot, e.Postcondition = false, false }),
		"present, silent":                  present(func(e *Evidence) { e.Listeners = []ListenerEvidence{} }),
		"present, one listener missing":    present(func(e *Evidence) { e.Listeners = e.Listeners[1:] }),
		"present, extra listener":          present(func(e *Evidence) { e.Listeners = append(e.Listeners, e.Listeners[0]) }),
		"present, renamed listener":        present(func(e *Evidence) { e.Listeners[0].Name = "other" }),
		"present, moved listener":          present(func(e *Evidence) { e.Listeners[0].Port++ }),
		"present, no status line":          present(func(e *Evidence) { e.Listeners[0].Status = "" }),
		"present, another certificate":     present(https(func(l *ListenerEvidence) { l.Fingerprint = strings.Repeat("a", 64) })),
		"present, malformed certificate":   present(https(func(l *ListenerEvidence) { l.Fingerprint = "zz" })),
		"present, no certificate":          present(https(func(l *ListenerEvidence) { l.Fingerprint = "" })),
		"present, http with a certificate": present(func(e *Evidence) { e.Listeners[0].Fingerprint = strings.Repeat("b", 64) }),
		"absent":                           absent(func(*Evidence) {}),
		"absent, another request":          absent(func(e *Evidence) { e.Request = strings.Repeat("9", 64) }),
		"absent, unclaimed":                absent(func(e *Evidence) { e.Postcondition = false }),
		"absent, started after files":      absent(func(e *Evidence) { e.StartedAfterFiles = true }),
		"absent, unit left":                absent(func(e *Evidence) { e.Unit = "failed" }),
		"absent, container left":           absent(func(e *Evidence) { e.Container = request.Image }),
		"absent, root left":                absent(func(e *Evidence) { e.ContentRoot = true }),
		"absent, listener left":            absent(func(e *Evidence) { e.Listeners = []ListenerEvidence{{Name: "x"}} }),
		"nothing, unclaimed":               absent(func(e *Evidence) { e.Absent, e.Postcondition = false, false }),
		"garbage":                          json.RawMessage(`{`),
	}
	names := slices.Sorted(func(yield func(string) bool) {
		for name := range forms {
			if !yield(name) {
				return
			}
		}
	})
	read := func(err error) string {
		if err == nil {
			return "accepted"
		}
		return described(err)
	}
	var lines []string
	for _, name := range names {
		evidence := forms[name]
		capability := New(&fakeRunner{result: lifecycle.RunResult{Outcome: reconciliation.OutcomeUnchanged, Evidence: evidence}}, fixedClock{})
		apply, applied := capability.Observe(context.Background(), call)
		removal, removed := capability.ObserveRemoval(context.Background(), call)
		lines = append(lines, fmt.Sprintf("%s:\n  apply %s: %s\n  removal %s: %s\n  presence: %s\n  absence: %s\n  partial: %s\n  unremoved: %s\n  unfinished: %s",
			name, apply.Effect, read(applied), removal.Effect, read(removed),
			read(ValidatePresence(evidence, request, digest, fingerprint)),
			read(ValidateAbsence(evidence, digest)),
			read(ValidatePartial(evidence, request, digest, fingerprint)),
			read(ValidateUnremoved(evidence, request, digest)),
			read(ValidateRemovalUnfinished(evidence, request, digest))))
	}
	matchesTextGolden(t, "evidence-readings", []byte(strings.Join(lines, "\n")+"\n"))
}
