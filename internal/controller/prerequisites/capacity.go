package prerequisites

import (
	"context"
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
// receipt whose bundle could never be reserved.
func (s Service) makeRoom(ctx context.Context, tx StorageTransaction, approved inspection, purge bool) ([]string, error) {
	retiring, err := approved.room(tx.Snapshot(), purge)
	if err != nil {
		return nil, err
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
