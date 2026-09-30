package prerequisites

import (
	"context"
	"slices"
	"strconv"
)

// room decides what publishing this inspection's bundle needs from a host at
// its bound, and returns the areas a retirement must remove first; nothing
// when the host has room. Every superseded execution bundle is one of them, and
// so is an area whose retirement an interruption left unfinished. The bundle
// the receipt names, the one a carried resolution is read from, the new bundle
// itself and every client area are never among them.
//
// A receipt whose setup failed or was canceled is replaced by the next one, so
// at the bound it counts like a completed one. Only a pending receipt admits
// no retirement: setup resumes it exactly and publishes no new receipt.
func (i inspection) room(view StorageView, purge bool) ([]string, error) {
	receipt, target := view.State.Receipt, i.definition.CatalogDigest
	if target == "" || receipt.ID == "" || receipt.Incomplete() {
		return nil, nil
	}
	holds := func(id string) bool {
		return slices.ContainsFunc(view.Areas, func(held HeldArea) bool { return held.ID == id })
	}
	area := !holds(target)
	resolution := i.definition.Bootstrap != nil && !slices.ContainsFunc(view.State.RetainedDefinitions,
		func(definition Definition) bool { return definition.ResolutionDigest == i.definition.ResolutionDigest })
	if !full(view, area, resolution, nil) {
		return nil, nil
	}
	kept := []string{receipt.CatalogDigest, i.retainedDigest, target}
	var retiring []string
	for _, held := range view.Areas {
		if held.Retiring && !slices.Contains(kept, held.ID) {
			retiring = append(retiring, held.ID)
		}
	}
	for _, definition := range view.State.RetainedDefinitions {
		id := definition.CatalogDigest
		if id != "" && holds(id) && !slices.Contains(kept, id) && !slices.Contains(retiring, id) {
			retiring = append(retiring, id)
		}
	}
	slices.Sort(retiring)
	if full(view, area, resolution, retiring) {
		return nil, failure("controller.conflict", "only client areas and the current execution bundle hold this host's "+strconv.Itoa(MaxRetainedBundles)+" bundle areas or resolutions, and neither is ever retired, so the new execution bundle has no room", "")
	}
	if !purge {
		return nil, failure("controller.conflict", "this host already retains the "+strconv.Itoa(MaxRetainedBundles)+" bundle areas or resolutions it may hold, so the new execution bundle has no room", "run bootwright setup --purge-old-bundles to retire the superseded execution bundles first")
	}
	return retiring, nil
}

// full reports whether the host, once the named areas and the resolutions
// they carry are retired, still lacks the area or the resolution the new
// bundle needs.
func full(view StorageView, area, resolution bool, retiring []string) bool {
	areas, resolutions := 0, 0
	for _, held := range view.Areas {
		if !slices.Contains(retiring, held.ID) {
			areas++
		}
	}
	for _, definition := range view.State.RetainedDefinitions {
		if !slices.Contains(retiring, definition.CatalogDigest) {
			resolutions++
		}
	}
	return area && areas >= MaxRetainedBundles || resolution && resolutions >= MaxRetainedBundles
}

// makeRoom retires, under the mutation that publishes the new bundle, what a
// host at its bound gives up first. It decides again from the store's own
// snapshot, so an area reserved after the plan was presented is counted, and
// it refuses rather than publishing a receipt whose bundle could never be
// reserved.
func (s Service) makeRoom(ctx context.Context, tx StorageTransaction, approved inspection, purge bool) ([]string, error) {
	retiring, err := approved.room(tx.Snapshot(), purge)
	if err != nil || len(retiring) == 0 {
		return nil, err
	}
	if err := tx.RetireBundles(ctx, retiring); err != nil {
		return nil, err
	}
	return retiring, nil
}

func retirementAction(count int) string {
	noun := "bundles"
	if count == 1 {
		noun = "bundle"
	}
	return "Retire " + strconv.Itoa(count) + " superseded execution " + noun + " to make room for the new one"
}
