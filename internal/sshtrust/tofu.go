package sshtrust

import (
	"context"
	"sort"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/infra/locality"
)

// FirstUseInteraction supplies the interactive pieces of the trust-on-first-use
// flow. Prompting and printing stay with the caller: Confirm asks the operator
// to accept a scanned key, Begin announces the flow before the first candidate
// is processed, and Warn/Skip/Recorded report per-host outcomes.
type FirstUseInteraction struct {
	Begin    func()
	Warn     func(name, detail string)
	Skip     func(name, detail string)
	Recorded func(name, detail string)
	Confirm  func(record HostRecord) bool
}

// OfferTrustOnFirstUse is the trust-on-first-use flow for preflight and apply:
// for every selected machine that requires managed SSH trust and has no
// recorded host key yet, scan the host, show the operator the key fingerprint,
// and record the key only after an explicit per-host yes. Machines with an
// existing record are never touched here — a changed key keeps failing closed
// with the `bootwright machine trust --replace` ceremony, because a changed key is
// the man-in-the-middle signal that deserves a deliberate step. Callers gate
// this on interactive text runs; non-interactive runs keep failing closed
// without recording anything. A declined or unreachable host is left for the
// host check that follows, which fails with the `bootwright machine trust`
// remediation.
func OfferTrustOnFirstUse(ctx context.Context, contextDir string, state v1alpha1.State, policy locality.Policy, deps Deps, interact FirstUseInteraction, scope map[string]bool) error {
	machines := MachinesInScope(ManagedTrustMachines(state, policy), scope)
	if len(machines) == 0 {
		return nil
	}
	store, err := Load(StorePathForContext(contextDir))
	if err != nil {
		return err
	}
	var candidates []v1alpha1.Machine
	for _, machine := range machines {
		if _, ok := store.Find(machine.Metadata.Name); ok {
			continue
		}
		candidates = append(candidates, machine)
	}
	if len(candidates) == 0 {
		return nil
	}
	if _, err := deps.LookPath("ssh-keyscan", nil); err != nil {
		// The host check that follows reports the missing tool with its
		// remediation; first-use recording is simply not offered.
		return nil
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].Metadata.Name < candidates[j].Metadata.Name })
	interact.Begin()
	accepted := 0
	for _, machine := range candidates {
		report, record, write, err := EvaluateHost(ctx, machine, store, false, deps.Scan, policy)
		if err != nil {
			// Unreachable host or scan failure: leave it to the host check.
			interact.Warn("Machine/"+machine.Metadata.Name, err.Error())
			continue
		}
		if !write || report.Action != "add" {
			continue
		}
		if !interact.Confirm(record) {
			interact.Skip("Machine/"+machine.Metadata.Name, "not trusted; run bootwright machine trust to record it later")
			continue
		}
		store.Upsert(record)
		accepted++
		interact.Recorded("Machine/"+machine.Metadata.Name, "recorded "+record.KeyType+" "+record.FingerprintSHA256)
	}
	if accepted == 0 {
		return nil
	}
	return Save(DirForContext(contextDir), store)
}
