package prerequisites

import (
	"bytes"
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
		return retirement{}, failure("controller.conflict", "only client areas and the current execution bundle hold this host's "+strconv.Itoa(MaxRetainedBundles)+" bundle areas or resolutions, and neither is ever retired, so the new execution bundle has no room", ClientAreaBoundRemedy)
	}
	if !purge {
		return retirement{}, failure("controller.conflict", "this host already retains the "+strconv.Itoa(MaxRetainedBundles)+" bundle areas or resolutions it may hold, so the new execution bundle has no room", PurgeRemedy)
	}
	return retiring, nil
}

// PurgeRemedy is the remedy of a bound that superseded execution bundles hold.
const PurgeRemedy = "run bootwright setup --purge-old-bundles to retire the superseded execution bundles first"

// ClientAreaBoundRemedy is the remedy of a bound that client areas and the
// current execution bundle fill. This build retires no client area, so no
// command of it frees that room, and the remedy names none (B322).
const ClientAreaBoundRemedy = "no command of this build frees that room, because it never retires a client area; " +
	"keep using the build whose execution bundle this host holds, or set this build up on another controller host"

// stranded reports a pending receipt an earlier build published at the bound:
// the bundle it names holds no area while the host holds every area it may,
// so resuming it needs a retirement first.
func stranded(view StorageView) bool {
	return strandedAt(view.State.Receipt, view.Areas)
}

func strandedAt(receipt SetupReceipt, areas []HeldArea) bool {
	return receipt.ID != "" && receipt.Incomplete() && len(areas) >= MaxRetainedBundles && !holdsArea(areas, receipt.CatalogDigest)
}

func holdsArea(areas []HeldArea, id string) bool {
	return slices.ContainsFunc(areas, func(held HeldArea) bool { return held.ID == id })
}

// errUnresumable marks the refusal of a pending receipt of setup's own that
// this executable cannot resume and whose setup never took effect, which is
// the one receipt setup --purge-old-bundles cancels.
var errUnresumable = errors.New("a pending setup receipt cannot be resumed by this executable")

// unresumable refuses that receipt. Only canceling it settles it, so the
// refusal names the command that does, and it says the receipt is at this
// host's bound only when it is.
func unresumable(view StorageView) error {
	message := "a setup left pending on this host never took effect and cannot be resumed by this executable"
	if stranded(view) {
		message = "an earlier build left a setup pending at this host's bound that this executable cannot resume"
	}
	return errors.Join(errUnresumable, failure("controller.conflict", message, "run "+purgeInvocation+" to cancel it, retire the superseded execution bundles and set this host up afresh"))
}

// neverStarted reports a pending receipt whose setup cannot have taken effect:
// its first action, the execution bundle, holds at most its intent, which an
// earlier build recorded before it met the bound reserving that bundle's area
// or lost the publication that followed, and every later action is still
// planned. A bundle area holds private files no other action reads before
// the receipt seals it, so publishing into one changes nothing else.
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

// nativePrepared reports a receipt whose execution bundle was published and
// whose native transaction holds its intent with the preparation the
// receipt's own resolution admits, for a plan of at least one action, and
// returns that preparation. The installer records that preparation before it
// stages the payloads and the transaction is authorized only after them, so a
// refusal in between, such as a download, leaves this shape, and such a
// transaction took effect only if the host's package inventory moved off the
// before-state the preparation names.
func nativePrepared(receipt SetupReceipt) (NativePreparation, bool) {
	definition := receipt.Definition
	if definition == nil || definition.Native == nil || len(definition.Native.Actions) == 0 || len(receipt.Actions) != 2 {
		return NativePreparation{}, false
	}
	bundle, native := receipt.Actions[0], receipt.Actions[1]
	if bundle.ID != "execution-bundle" || bundle.Phase != "observed" || bundle.Outcome != "changed" && bundle.Outcome != "unchanged" ||
		native.ID != "container-runtime" || native.Phase != "intent" || len(native.Preparation) == 0 {
		return NativePreparation{}, false
	}
	preparation, err := ReadNativePreparation(native.Preparation, *definition)
	if err != nil {
		return NativePreparation{}, false
	}
	return preparation, true
}

// abandonable reports a pending receipt of setup's own, at the bound or below
// it, that setup cancels once this executable cannot resume it (D93), which
// its caller has decided: one whose setup never started, or one whose native
// transaction never started, which only a package inventory read now and
// still equal to the preparation's before-state, and not its after-state,
// proves. Whatever route it recorded, nothing it acquired took effect. A
// receipt in any other shape, or one whose inventory cannot be read, may have
// taken effect.
func (s Service) abandonable(ctx context.Context, view StorageView, platform Platform) bool {
	receipt := view.State.Receipt
	if receipt.ID == "" || !receipt.Incomplete() || receipt.Context != (SetupContext{}) {
		return false
	}
	if neverStarted(receipt) {
		return true
	}
	preparation, prepared := nativePrepared(receipt)
	if !prepared {
		return false
	}
	inventory, reads := s.options.NativeInspector.(NativeInventory)
	if !reads {
		return false
	}
	digest, err := inventory.Inventory(ctx, platform)
	return err == nil && digest == preparation.InventorySHA256 && digest != preparation.AfterInventorySHA256
}

// canceled is a receipt abandonable admits as setup records it canceled, from
// what it observed. A first action that held its intent is observed never
// started, its evidence saying whether the record holds no area for the bundle
// the receipt names or an unsealed one; a native transaction that held its
// intent is observed never started over an unchanged package inventory, its
// preparation kept. Every other action stays as it was.
func canceled(receipt SetupReceipt, areas []HeldArea) SetupReceipt {
	receipt.Actions = slices.Clone(receipt.Actions)
	switch {
	case neverStarted(receipt) && receipt.Actions[0].Phase == "intent":
		area := "absent"
		if holdsArea(areas, receipt.CatalogDigest) {
			area = "unsealed"
		}
		receipt.Actions[0].Phase, receipt.Actions[0].Outcome = "observed", "canceled"
		receipt.Actions[0].Evidence = object(map[string]any{"bundleArea": area})
	case !neverStarted(receipt):
		native := &receipt.Actions[len(receipt.Actions)-1]
		native.Phase, native.Outcome = "observed", "canceled"
		native.Evidence = object(map[string]any{"nativeInventory": "unchanged"})
	}
	receipt.Status = "canceled"
	return receipt
}

// abandoned names the receipt a setup --purge-old-bundles cancels: a pending
// one of setup's own that this executable cannot resume and whose setup never
// took effect. The inspection that refuses it to preflight and to a setup
// without the flag decides, first and without streaming, so all three judge it
// alike.
func (s Service) abandoned(ctx context.Context, view StorageView) SetupReceipt {
	receipt := view.State.Receipt
	if !s.abandon || view.Context.Name != "" || receipt.ID == "" || !receipt.Incomplete() || receipt.Context != (SetupContext{}) {
		return SetupReceipt{}
	}
	if _, prepared := nativePrepared(receipt); !neverStarted(receipt) && !prepared {
		return SetupReceipt{}
	}
	judge := s
	judge.abandon = false
	if _, err := judge.inspect(ctx, view, false, ""); !errors.Is(err, errUnresumable) {
		return SetupReceipt{}
	}
	return receipt
}

// abandon decides again, from the store's own snapshot under the mutation
// that publishes, that the receipt this plan cancels is still the one it was
// approved over, that setup still cancels it, over a package inventory read
// again, and that it is canceled exactly as the plan presented it, and
// returns that snapshot with the receipt canceled.
func (i inspection) abandon(ctx context.Context, s Service, view StorageView) (StorageView, error) {
	if i.abandoned.ID == "" {
		return view, nil
	}
	changed := failure("controller.conflict", "controller state changed after plan confirmation", setupCommand())
	receipt := view.State.Receipt
	if receipt.ID != i.abandoned.ID || receipt.PlanDigest != i.abandoned.PlanDigest || !s.abandonable(ctx, view, i.platform) {
		return view, changed
	}
	next := canceled(receipt, view.Areas)
	if !slices.EqualFunc(next.Actions, i.view.State.Receipt.Actions, sameAction) {
		return view, changed
	}
	view.State.Receipt = next
	return view, nil
}

func sameAction(a, b SetupAction) bool {
	return a.ID == b.ID && a.Phase == b.Phase && a.Outcome == b.Outcome && bytes.Equal(a.Request, b.Request) &&
		bytes.Equal(a.Evidence, b.Evidence) && bytes.Equal(a.Preparation, b.Preparation)
}

// abandonment is the plan line that names the cancellation, if any: which
// receipt it cancels, that this executable cannot resume it, and why its setup
// never took effect.
func (i inspection) abandonment() []string {
	if i.abandoned.ID == "" {
		return nil
	}
	pending := "Cancel the setup left pending on this host, which this executable cannot resume"
	if strandedAt(i.abandoned, i.view.Areas) {
		pending = "Cancel the setup an earlier build left pending at this host's bound, which this executable cannot resume"
	}
	switch {
	case !neverStarted(i.abandoned):
		return []string{pending + " and whose native package transaction never started, as its unchanged package inventory shows"}
	case holdsArea(i.view.Areas, i.abandoned.CatalogDigest):
		return []string{pending + " and which never completed its bundle"}
	}
	return []string{pending + " and which never published its bundle"}
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
	view, err := approved.abandon(ctx, s, tx.Snapshot())
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
