package prerequisites

import (
	"context"
	"errors"
	"slices"
	"strconv"
)

// retirement is what a setup at its bound gives up before it publishes: the
// areas it removes, each with the resolutions it carries, and the superseded
// resolutions of the bundles it keeps.
type retirement struct {
	bundles, resolutions []string
}

// room decides what publishing this inspection's bundle needs from a host at
// its bound, and returns what a retirement must remove first; nothing when the
// host has room. Every superseded execution bundle is among it, and so is an
// area whose retirement an interruption left unfinished. The bundle the
// receipt names, the one a carried resolution is read from, the new bundle
// itself and every client area are never among it.
//
// A receipt whose setup failed or was canceled is replaced by the next one, so
// at the bound it counts like a completed one. A pending receipt is resumed
// exactly and never replaced, so it admits a retirement only when an earlier
// build left it stranded, its bundle holding no area at the bound: then the
// bundle it names is the new one, and its resolution, already retained, stays.
func (i inspection) room(view StorageView, purge bool) (retirement, error) {
	receipt, target := view.State.Receipt, i.definition.CatalogDigest
	holds := func(id string) bool {
		return slices.ContainsFunc(view.Areas, func(held HeldArea) bool { return held.ID == id })
	}
	if target == "" || receipt.ID == "" || receipt.Incomplete() && holds(receipt.CatalogDigest) {
		return retirement{}, nil
	}
	area := !holds(target)
	resolution := i.definition.Bootstrap != nil && !slices.ContainsFunc(view.State.RetainedDefinitions,
		func(definition Definition) bool { return definition.ResolutionDigest == i.definition.ResolutionDigest })
	if !full(view, area, resolution, retirement{}) {
		return retirement{}, nil
	}
	kept := []string{receipt.CatalogDigest, i.retainedDigest, target}
	var retiring retirement
	for _, held := range view.Areas {
		if held.Retiring && !slices.Contains(kept, held.ID) {
			retiring.bundles = append(retiring.bundles, held.ID)
		}
	}
	for _, definition := range view.State.RetainedDefinitions {
		id := definition.CatalogDigest
		if id != "" && holds(id) && !slices.Contains(kept, id) && !slices.Contains(retiring.bundles, id) {
			retiring.bundles = append(retiring.bundles, id)
		}
	}
	slices.Sort(retiring.bundles)
	if !receipt.Incomplete() {
		retiring.resolutions = i.supersededResolutions(view.State, kept)
	}
	if full(view, area, resolution, retiring) {
		return retirement{}, failure("controller.conflict", "only client areas and the current execution bundle hold this host's "+strconv.Itoa(MaxRetainedBundles)+" bundle areas or resolutions, and neither is ever retired, so the new execution bundle has no room", "")
	}
	if !purge {
		return retirement{}, failure("controller.conflict", "this host already retains the "+strconv.Itoa(MaxRetainedBundles)+" bundle areas or resolutions it may hold, so the new execution bundle has no room", "run bootwright setup --purge-old-bundles to retire the superseded execution bundles first")
	}
	return retiring, nil
}

// stranded reports a pending receipt an earlier build published at the bound:
// the bundle it names holds no area while the host holds every area it may,
// so resuming it needs a retirement first.
func stranded(view StorageView) bool {
	receipt := view.State.Receipt
	return receipt.ID != "" && receipt.Incomplete() && len(view.Areas) >= MaxRetainedBundles &&
		!slices.ContainsFunc(view.Areas, func(held HeldArea) bool { return held.ID == receipt.CatalogDigest })
}

// errUnresumable marks the refusal of a stranded receipt that this executable
// cannot resume and whose setup never took effect, which is the one receipt
// setup --purge-old-bundles abandons.
var errUnresumable = errors.New("a setup receipt stranded at the bound cannot be resumed by this executable")

// unresumable refuses that receipt. Only abandoning it settles it, so the
// refusal names the command that does.
func unresumable() error {
	return errors.Join(errUnresumable, failure("controller.conflict", "an earlier build left a setup pending at this host's bound that this executable cannot resume", "run bootwright setup --purge-old-bundles to cancel it, retire the superseded execution bundles and set this host up afresh"))
}

// neverStarted reports a stranded receipt whose setup cannot have taken
// effect. The build that published it recorded the intent of its first
// action, the execution bundle, and then met the bound reserving that
// bundle's area, so that action holds at most its intent and every later
// action is still planned. A receipt in any other shape may have taken effect.
func neverStarted(receipt SetupReceipt) bool {
	if len(receipt.Actions) == 0 || receipt.Actions[0].ID != "execution-bundle" {
		return false
	}
	for index, action := range receipt.Actions {
		if action.Phase != "planned" && (index != 0 || action.Phase != "intent") {
			return false
		}
	}
	return true
}

// abandonable reports a receipt setup abandons once this executable cannot
// resume it: one setup recorded, naming no context, stranded at the bound and
// never started. Whatever route it recorded, nothing was acquired over it.
func abandonable(view StorageView) bool {
	receipt := view.State.Receipt
	return receipt.Context == (SetupContext{}) && stranded(view) && neverStarted(receipt)
}

// canceled is a receipt neverStarted admits once it is abandoned: the intent
// of its execution bundle is observed as never started, because the store
// holds no area for the bundle the receipt names, and every later action
// stays planned.
func canceled(receipt SetupReceipt) SetupReceipt {
	receipt.Actions = slices.Clone(receipt.Actions)
	if receipt.Actions[0].Phase == "intent" {
		receipt.Actions[0].Phase, receipt.Actions[0].Outcome = "observed", "canceled"
		receipt.Actions[0].Evidence = object(map[string]any{"bundleArea": "absent"})
	}
	receipt.Status = "canceled"
	return receipt
}

// abandoned names the receipt a setup --purge-old-bundles cancels: one
// stranded at the bound that this executable cannot resume. The inspection
// that refuses it to preflight and to a setup without the flag decides,
// first and without streaming, so all three judge it alike.
func (s Service) abandoned(ctx context.Context, view StorageView) SetupReceipt {
	if !s.abandon || view.Context.Name != "" || !stranded(view) {
		return SetupReceipt{}
	}
	judge := s
	judge.abandon = false
	if _, err := judge.inspect(ctx, view, false, ""); !errors.Is(err, errUnresumable) {
		return SetupReceipt{}
	}
	return view.State.Receipt
}

// abandon decides again, from the store's own snapshot under the mutation
// that publishes, that the receipt this plan cancels is still the stranded one
// it was approved over, and returns that snapshot with the receipt canceled.
func (i inspection) abandon(view StorageView) (StorageView, error) {
	if i.abandoned.ID == "" {
		return view, nil
	}
	receipt := view.State.Receipt
	if receipt.ID != i.abandoned.ID || receipt.PlanDigest != i.abandoned.PlanDigest || !stranded(view) || !neverStarted(receipt) {
		return view, failure("controller.conflict", "controller state changed after plan confirmation", setupCommand())
	}
	view.State.Receipt = canceled(receipt)
	return view, nil
}

// abandonment is the plan line that names the cancellation, if any.
func (i inspection) abandonment() []string {
	if i.abandoned.ID == "" {
		return nil
	}
	return []string{"Cancel the setup an earlier build left pending at this host's bound, which never published its bundle"}
}

// supersededResolutions are the retained resolutions of a kept bundle that
// nothing reads any more. A retry after a failed setup, or a solve against a
// changed package inventory, names the same bundle under a new resolution, so
// one bundle can come to hold every resolution the host may retain. Of those
// the receipt's own stays, as does the one this setup publishes and the latest
// naming each kept bundle, which keeps that bundle identifiable as an
// execution bundle.
func (i inspection) supersededResolutions(state HostState, kept []string) []string {
	needed := []string{i.definition.ResolutionDigest}
	if state.Receipt.Definition != nil {
		needed = append(needed, state.Receipt.Definition.ResolutionDigest)
	}
	var superseded []string
	for index, definition := range state.RetainedDefinitions {
		if definition.CatalogDigest == "" || !slices.Contains(kept, definition.CatalogDigest) ||
			definition.ResolutionDigest == "" || slices.Contains(needed, definition.ResolutionDigest) {
			continue
		}
		if slices.ContainsFunc(state.RetainedDefinitions[index+1:], func(later Definition) bool { return later.CatalogDigest == definition.CatalogDigest }) {
			superseded = append(superseded, definition.ResolutionDigest)
		}
	}
	slices.Sort(superseded)
	return superseded
}

// full reports whether the host, once the named areas with the resolutions
// they carry and the named resolutions are retired, still lacks the area or
// the resolution the new bundle needs.
func full(view StorageView, area, resolution bool, retiring retirement) bool {
	areas, resolutions := 0, 0
	for _, held := range view.Areas {
		if !slices.Contains(retiring.bundles, held.ID) {
			areas++
		}
	}
	for _, definition := range view.State.RetainedDefinitions {
		if !slices.Contains(retiring.bundles, definition.CatalogDigest) && !slices.Contains(retiring.resolutions, definition.ResolutionDigest) {
			resolutions++
		}
	}
	return area && areas >= MaxRetainedBundles || resolution && resolutions >= MaxRetainedBundles
}

// makeRoom retires, under the mutation that publishes the new bundle, what a
// host at its bound gives up first, and returns the areas it removed. It
// decides again from the store's own snapshot, so an area reserved after the
// plan was presented is counted, and it refuses rather than publishing a
// receipt whose bundle could never be reserved. A receipt the plan abandons is
// recorded canceled first, once both decisions hold, and nothing is retired
// before that cancellation is durable.
func (s Service) makeRoom(ctx context.Context, tx StorageTransaction, approved inspection, purge bool) ([]string, error) {
	view, err := approved.abandon(tx.Snapshot())
	if err != nil {
		return nil, err
	}
	retiring, err := approved.room(view, purge)
	if err != nil {
		return nil, err
	}
	if approved.abandoned.ID != "" {
		if err := publish(ctx, tx, view.State); err != nil {
			return nil, err
		}
	}
	if len(retiring.bundles) != 0 {
		if err := tx.RetireBundles(ctx, retiring.bundles); err != nil {
			return nil, err
		}
	}
	if len(retiring.resolutions) != 0 {
		if err := tx.RetireResolutions(ctx, retiring.resolutions); err != nil {
			return retiring.bundles, err
		}
	}
	return retiring.bundles, nil
}

// actions are the plan lines that name this retirement.
func (r retirement) actions() []string {
	var actions []string
	if count := len(r.bundles); count != 0 {
		noun := "bundles"
		if count == 1 {
			noun = "bundle"
		}
		actions = append(actions, "Retire "+strconv.Itoa(count)+" superseded execution "+noun+" to make room for the new one")
	}
	if count := len(r.resolutions); count != 0 {
		noun := "resolutions of kept execution bundles"
		if count == 1 {
			noun = "resolution of a kept execution bundle"
		}
		actions = append(actions, "Retire "+strconv.Itoa(count)+" superseded "+noun+" to make room for the new one")
	}
	return actions
}
