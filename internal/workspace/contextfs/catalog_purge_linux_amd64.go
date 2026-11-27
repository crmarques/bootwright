//go:build linux && amd64

package contextfs

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"syscall"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/infrastructureservices/artifactcatalog"
	"github.com/crmarques/bootwright/internal/managedos"
	"github.com/crmarques/bootwright/internal/workspace/catalogpurge"
	"github.com/crmarques/bootwright/internal/workspace/contextfs/kernel"
	"github.com/crmarques/bootwright/internal/workspace/contexts"
)

func (s *Store) PlanCatalogPurge(ctx context.Context, kind catalogpurge.Kind, force bool, references catalogpurge.References) (catalogpurge.Plan, error) {
	var plan catalogpurge.Plan
	err := s.catalogPurge(ctx, kind, force, references, nil, func(current catalogpurge.Plan, _ func(string) error) error {
		plan = current
		return nil
	})
	return plan, err
}

func (s *Store) CommitCatalogPurge(ctx context.Context, expected catalogpurge.Plan, references catalogpurge.References) (int, error) {
	removed := 0
	err := s.catalogPurge(ctx, expected.Kind, expected.Force, references, &expected, func(plan catalogpurge.Plan, remove func(string) error) error {
		for _, row := range plan.Entries {
			if !row.Selected(plan.Force) {
				continue
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			if err := remove(row.Name); err != nil {
				return purgeRemovalFailure(plan.Kind, row.Name, err)
			}
			removed++
		}
		return nil
	})
	return removed, err
}

func (s *Store) catalogPurge(ctx context.Context, kind catalogpurge.Kind, force bool, references catalogpurge.References, expected *catalogpurge.Plan, use func(catalogpurge.Plan, func(string) error) error) error {
	if !kind.Valid() || references == nil {
		return catalogpurge.Failure(kind, "catalog purge has no qualified store or reference reader", "use catalog artifact purge or catalog media purge")
	}
	lock := syscall.LOCK_SH
	if expected != nil {
		lock = syscall.LOCK_EX
	}
	absent := catalogpurge.Failure(kind, "this host has no prepared catalog store", "run bootwright install first")
	return s.withRead(ctx, lock, readScope{absent: absent, registered: true}, func(root *directory, registry contexts.Registry) error {
		stored, err := readControllerStored(ctx, root, registry)
		if err != nil {
			return safeError(err)
		}
		container := artifactContainer
		if kind == catalogpurge.Media {
			container = mediaContainer
		}
		dir, err := kernel.OpenDirectory(root, container)
		if err != nil && !errors.Is(err, syscall.ENOENT) {
			return safeError(err)
		}
		if dir != nil {
			defer dir.Close()
		}
		err = nil
		area := &mediaArea{store: s, root: root, dir: dir, stored: stored, write: expected != nil, active: func() bool { return true }}
		plan := catalogpurge.Plan{Kind: kind, Force: force, Entries: []catalogpurge.Entry{}}
		remove := func(name string) error { return s.removeArtifact(ctx, dir, name) }
		if kind == catalogpurge.Media {
			plan.Entries, err = mediaPurgeEntries(ctx, area)
			remove = func(name string) error { return area.remove(ctx, name) }
		} else if dir != nil {
			plan.Entries, err = artifactPurgeEntries(ctx, dir, stored)
		}
		if err != nil {
			return safeError(err)
		}
		if len(plan.Entries) != 0 {
			if err := catalogPurgeReferences(ctx, root, registry, &plan, references); err != nil {
				return safeError(err)
			}
		}
		if expected != nil {
			if !reflect.DeepEqual(plan, *expected) {
				return catalogpurge.Failure(kind, "the catalog or its context references changed during purge confirmation", "review the updated purge with --dry-run")
			}
			if kind == catalogpurge.Media {
				if err := idlePurgeMedia(area, plan); err != nil {
					return safeError(err)
				}
			}
			if force && len(plan.Entries) != 0 {
				if err := idlePurgeContexts(root, registry); err != nil {
					return safeError(err)
				}
			}
		}
		return safeError(use(plan, remove))
	})
}

func artifactPurgeEntries(ctx context.Context, dir *directory, stored controllerStored) ([]catalogpurge.Entry, error) {
	names, err := kernel.DirectoryNames(dir, artifactcatalog.MaxEntries+maxArtifactStages)
	if err != nil {
		return nil, err
	}
	slices.Sort(names)
	entries := []catalogpurge.Entry{}
	for _, name := range names {
		if !artifactcatalog.ValidName(name) {
			continue
		}
		entry, _, err := readArtifact(ctx, dir, name, true)
		if err != nil {
			return nil, err
		}
		entries = append(entries, catalogpurge.Entry{Name: name, Identity: entry.Identity(), ReservedBy: artifactHolders(stored, name)})
	}
	return entries, nil
}

func mediaPurgeEntries(ctx context.Context, area *mediaArea) ([]catalogpurge.Entry, error) {
	names, err := area.Names(ctx)
	if err != nil {
		return nil, err
	}
	images, err := area.Entries(ctx)
	if err != nil {
		return nil, err
	}
	if len(names) != len(images) {
		return nil, catalogpurge.Failure(catalogpurge.Media, "the media catalog holds an incomplete entry", "inspect the media store and recover or delete the damaged entry by name")
	}
	reserved, err := area.Reservations(ctx)
	if err != nil {
		return nil, err
	}
	entries := make([]catalogpurge.Entry, 0, len(images))
	for _, image := range images {
		if image.Failure != "" || image.Observed != image.Size {
			return nil, catalogpurge.Failure(catalogpurge.Media, "the media catalog holds an unreadable or incomplete image "+image.Name,
				"inspect it with bootwright catalog media list and recover or delete it by name")
		}
		record, err := managedos.EncodeMediaRecord(image.MediaEntry)
		if err != nil {
			return nil, err
		}
		entries = append(entries, catalogpurge.Entry{Name: image.Name, Identity: string(record), ReservedBy: reserved[image.Name]})
	}
	return entries, nil
}

func catalogPurgeReferences(ctx context.Context, root *directory, registry contexts.Registry, plan *catalogpurge.Plan, references catalogpurge.References) error {
	index := make(map[string]int, len(plan.Entries))
	for n, entry := range plan.Entries {
		index[entry.Name] = n
	}
	for _, record := range registry.Contexts {
		if err := ctx.Err(); err != nil {
			return err
		}
		if record.Revision == "" {
			continue
		}
		inputs, err := readSnapshot(ctx, root, record)
		if err != nil {
			return err
		}
		names, err := references(ctx, plan.Kind, inputs)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return catalogpurge.Failure(plan.Kind, "catalog references could not be proved for context "+record.Name,
				"repair that context's saved input before purging")
		}
		for _, name := range names {
			if n, found := index[name]; found {
				entry := &plan.Entries[n]
				entry.ReferencedBy = append(entry.ReferencedBy, record.Name)
			}
		}
	}
	for n := range plan.Entries {
		names := plan.Entries[n].ReferencedBy
		slices.Sort(names)
		plan.Entries[n].ReferencedBy = slices.Compact(names)
	}
	return nil
}

func idlePurgeContexts(root *directory, registry contexts.Registry) error {
	for _, record := range registry.Contexts {
		if record.Mode != contexts.Ready {
			continue
		}
		container, dir, err := openContext(root, record)
		if err != nil {
			return err
		}
		err = kernel.Lock(dir)
		if err == nil {
			kernel.UnlockAndClose(dir.File)
		} else {
			dir.Close()
		}
		container.Close()
		var busy *kernel.BusyError
		if errors.As(err, &busy) {
			return kernel.Busy("catalog purge cannot remove reserved data while context " + record.Name + " is active")
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func idlePurgeMedia(area *mediaArea, plan catalogpurge.Plan) error {
	for _, entry := range plan.Entries {
		if !entry.Selected(plan.Force) {
			continue
		}
		held, _, err := area.stageHeld(mediaStageName(entry.Name))
		if err != nil {
			return err
		}
		if held {
			return acquiringFailure(entry.Name)
		}
	}
	return nil
}

func purgeRemovalFailure(kind catalogpurge.Kind, name string, cause error) error {
	remedy := "inspect the catalog and restore the entry from a trusted backup if it is needed"
	if kind == catalogpurge.Media {
		remedy += ", or finish removing the incomplete entry by name with bootwright catalog media delete when unreserved"
	}
	summary := catalogpurge.Failure(kind, fmt.Sprintf("purge could not complete removal of %s, which may be partially removed", name), remedy)
	return &diagnostics.Failure{Diagnostics: append(diagnostics.Of(safeError(cause)), diagnostics.Of(summary)...)}
}
