package sshtrust

import (
	"context"
	"sort"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/infra/locality"
)

type FirstUseInteraction struct {
	Begin    func()
	Warn     func(name, detail string)
	Skip     func(name, detail string)
	Recorded func(name, detail string)
	Confirm  func(record HostRecord) bool
}

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
		return nil
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].Metadata.Name < candidates[j].Metadata.Name })
	interact.Begin()
	accepted := 0
	for _, machine := range candidates {
		report, record, write, err := EvaluateHost(ctx, machine, store, false, deps.Scan, policy)
		if err != nil {
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
