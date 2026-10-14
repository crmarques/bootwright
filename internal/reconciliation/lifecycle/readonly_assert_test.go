package lifecycle

import (
	"crypto/sha256"
	"encoding/hex"
	"maps"
	"slices"
	"testing"
)

type writeCounts struct {
	mutations, runs              int
	operations                   string
	runEntries                   int
	bound, released, issued      int
	binds                        int
	reservations                 int
	evidence                     string
	applies, destroys, observes  int
	probes, removals             int
	produced, journal            int
	runAreaDirectories, guardRun int
}

func areaDigest(area *memoryArea) string {
	area.mutex.Lock()
	defer area.mutex.Unlock()
	digest := sha256.New()
	for _, name := range slices.Sorted(maps.Keys(area.files)) {
		digest.Write([]byte(name + "\x00"))
		digest.Write(area.files[name])
		digest.Write([]byte{0})
	}
	for _, name := range slices.Sorted(maps.Keys(area.directories)) {
		digest.Write([]byte("dir " + name + "\x00"))
	}
	return hex.EncodeToString(digest.Sum(nil))
}

func countWrites(h *harness) writeCounts {
	h.workspace.runArea.mutex.Lock()
	runEntries, runDirectories := len(h.workspace.runArea.files), len(h.workspace.runArea.directories)
	h.workspace.runArea.mutex.Unlock()
	h.capability.mutex.Lock()
	defer h.capability.mutex.Unlock()
	evidence := sha256.Sum256(h.workspace.evidence)
	return writeCounts{
		mutations: h.workspace.mutations, runs: h.workspace.runs,
		operations: areaDigest(h.workspace.area), runEntries: runEntries, runAreaDirectories: runDirectories,
		bound: len(h.binder.bound), released: len(h.binder.released), issued: h.binder.issued,
		binds: h.workspace.binds, reservations: len(h.workspace.reservations),
		evidence: hex.EncodeToString(evidence[:]),
		applies:  len(h.capability.applies), destroys: len(h.capability.destroys), observes: len(h.capability.observes),
		probes: len(h.capability.probes), removals: len(h.capability.removals),
		produced: len(h.binder.produced), journal: len(h.binder.journal), guardRun: h.guard.entered(),
	}
}

func assertNothingWritten(t *testing.T, h *harness, before writeCounts) {
	t.Helper()
	if after := countWrites(h); after != before {
		t.Fatalf("a read-only call wrote or ran something: before %+v, after %+v", before, after)
	}
}
