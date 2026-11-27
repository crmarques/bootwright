//go:build linux && amd64

package contextfs

import (
	"context"
	"errors"
	"slices"
	"strings"
	"syscall"
	"testing"

	"github.com/crmarques/bootwright/internal/desiredstate"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/infrastructureservices"
	"github.com/crmarques/bootwright/internal/infrastructureservices/artifactcatalog"
	"github.com/crmarques/bootwright/internal/managedos"
	"github.com/crmarques/bootwright/internal/managedos/media"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/workspace/catalogpurge"
	"github.com/crmarques/bootwright/internal/workspace/contextfs/kernel"
	"github.com/crmarques/bootwright/internal/workspace/contexts"
)

func purgeNoReferences(context.Context, catalogpurge.Kind, desiredstate.Sources) ([]string, error) {
	return nil, nil
}

func purgeArtifact(t *testing.T, store *Store, name string) artifactcatalog.Entry {
	t.Helper()
	entry, _, err := store.AddArtifact(t.Context(), artifactcatalog.Entry{Name: name}, strings.NewReader("test archive"))
	if err != nil {
		t.Fatal(err)
	}
	return entry
}

func purgeClaim(t *testing.T, store *Store, class, subject string) {
	t.Helper()
	ctx := t.Context()
	publishControllerState(t, store, syntheticControllerState(t))
	if err := store.MutateLifecycle(ctx, "example", func(tx lifecycle.Transaction) error {
		return tx.Reserve(ctx, []infrastructureservices.HostReservation{{
			Context: "example", Kind: class, Service: class, Shared: true, Keys: []string{class + ":" + subject},
		}})
	}); err != nil {
		t.Fatal(err)
	}
}

func TestCatalogPurgeProtectsSavedAndFrozenReferencesByDefault(t *testing.T) {
	for _, kind := range []catalogpurge.Kind{catalogpurge.Artifact, catalogpurge.Media} {
		t.Run(string(kind), func(t *testing.T) {
			ctx := t.Context()
			store := mediaFixture(t)
			_, sources := fixture(t)
			publish(t, store, "another", sources)
			names := []string{"declared", "reserved", "unused"}
			if kind == catalogpurge.Media {
				for i, name := range names {
					names[i] = name + ".iso"
					addMedia(t, store, names[i], "test media", false)
				}
				purgeClaim(t, store, infrastructureservices.MediaClass, names[1])
			} else {
				for _, name := range names {
					entry := purgeArtifact(t, store, name)
					if name == names[1] {
						purgeClaim(t, store, artifactcatalog.ClaimClass, entry.Name+":"+entry.Identity())
					}
				}
			}
			references := func(context.Context, catalogpurge.Kind, desiredstate.Sources) ([]string, error) {
				return []string{names[0], names[0]}, nil
			}
			read := func(force bool) catalogpurge.Plan {
				t.Helper()
				plan, err := store.PlanCatalogPurge(ctx, kind, force, references)
				if err != nil {
					t.Fatal(err)
				}
				return plan
			}
			plan := read(false)
			if len(plan.Entries) != 3 || !slices.Equal(plan.Entries[0].ReferencedBy, []string{"another", "example"}) ||
				!slices.Equal(plan.Entries[1].ReservedBy, []string{"example"}) || !plan.Entries[2].Selected(plan.Force) {
				t.Fatalf("unexpected plan: %+v", plan)
			}
			if count, err := store.CommitCatalogPurge(ctx, plan, references); count != 1 || err != nil {
				t.Fatalf("removed %d, error %v", count, err)
			}
			forced := read(true)
			if len(forced.Entries) != 2 {
				t.Fatalf("normal purge removed a referenced entry: %+v", forced)
			}
			if count, err := store.CommitCatalogPurge(ctx, forced, references); count != 2 || err != nil {
				t.Fatalf("force removed %d, error %v", count, err)
			}
			if len(read(false).Entries) != 0 {
				t.Fatal("successful forced purge left registered entries")
			}
			if err := store.ReadMedia(ctx, func(view media.View) error {
				if kind != catalogpurge.Media {
					return nil
				}
				reserved, err := view.Reservations(ctx)
				if !slices.Equal(reserved[names[1]], []string{"example"}) {
					t.Fatal("purge removed a context's reservation")
				}
				return err
			}); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestCatalogPurgeReprovesContextReferencesAndRegistrationIdentity(t *testing.T) {
	ctx := t.Context()
	store := mediaFixture(t)
	entry := purgeArtifact(t, store, "archive")
	plan, err := store.PlanCatalogPurge(ctx, catalogpurge.Artifact, false, purgeNoReferences)
	if err != nil {
		t.Fatal(err)
	}
	references := func(context.Context, catalogpurge.Kind, desiredstate.Sources) ([]string, error) {
		return []string{entry.Name}, nil
	}
	if count, err := store.CommitCatalogPurge(ctx, plan, references); count != 0 || err == nil {
		t.Fatalf("changed references accepted: %d, %v", count, err)
	}
	if err := store.DeleteArtifact(ctx, entry); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.AddArtifact(ctx, artifactcatalog.Entry{Name: entry.Name}, strings.NewReader("replacement")); err != nil {
		t.Fatal(err)
	}
	if count, err := store.CommitCatalogPurge(ctx, plan, purgeNoReferences); count != 0 || err == nil {
		t.Fatalf("replaced entry accepted: %d, %v", count, err)
	}
}

func TestForceCatalogPurgeCannotRaceAnActiveContext(t *testing.T) {
	ctx := t.Context()
	store := mediaFixture(t)
	purgeArtifact(t, store, "archive")
	plan, err := store.PlanCatalogPurge(ctx, catalogpurge.Artifact, true, purgeNoReferences)
	if err != nil {
		t.Fatal(err)
	}
	var held *directory
	var parent *directory
	if err := store.withRead(ctx, syscall.LOCK_SH, readScope{}, func(root *directory, registry contexts.Registry) error {
		var err error
		parent, held, err = openContext(root, registry.Contexts[0])
		if err != nil {
			return err
		}
		return kernel.Lock(held)
	}); err != nil {
		t.Fatal(err)
	}
	defer parent.Close()
	defer kernel.UnlockAndClose(held.File)
	count, err := store.CommitCatalogPurge(ctx, plan, purgeNoReferences)
	var busy *kernel.BusyError
	if count != 0 || !errors.As(err, &busy) {
		t.Fatalf("active context was not protected: %d %v", count, err)
	}
}

func TestCatalogPurgeRefusesUnprovedReferencesBeforeDeletingAnything(t *testing.T) {
	ctx := t.Context()
	store := mediaFixture(t)
	purgeArtifact(t, store, "archive")
	for _, force := range []bool{false, true} {
		_, err := store.PlanCatalogPurge(ctx, catalogpurge.Artifact, force, func(context.Context, catalogpurge.Kind, desiredstate.Sources) ([]string, error) {
			return nil, errors.New("unsafe to disclose")
		})
		if found := diagnostics.Of(err); len(found) != 1 || found[0].Code != "artifact.catalog" || strings.Contains(found[0].Message, "unsafe to disclose") {
			t.Fatal(found)
		}
	}
	if rows, err := store.ListArtifacts(ctx); err != nil || len(rows) != 1 {
		t.Fatalf("refusal disturbed catalog: %v %v", rows, err)
	}
}

func TestCatalogPurgeProtectsAnActiveMediaReplacement(t *testing.T) {
	ctx := t.Context()
	store := mediaFixture(t)
	const image = "install.iso"
	addMedia(t, store, image, "original image", false)
	stage := claimStage(t, store, image)
	for _, force := range []bool{false, true} {
		plan, err := store.PlanCatalogPurge(ctx, catalogpurge.Media, force, purgeNoReferences)
		if err != nil {
			t.Fatal(err)
		}
		if count, err := store.CommitCatalogPurge(ctx, plan, purgeNoReferences); count != 0 || err == nil {
			t.Fatalf("purge ignored an active import: %d, %v", count, err)
		}
		expectMedia(t, store, map[string]string{image: mediaDigest("original image")})
	}
	if err := stage.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestCatalogPurgeNamesAnIncompleteMediaWithdrawal(t *testing.T) {
	ctx := t.Context()
	store := mediaFixture(t)
	const image = "install.iso"
	addMedia(t, store, image, "original image", false)
	plan, err := store.PlanCatalogPurge(ctx, catalogpurge.Media, false, purgeNoReferences)
	if err != nil {
		t.Fatal(err)
	}
	store.fail = func(point string) error {
		if point == string(checkpointBeforeMediaImageRemoval) {
			return errors.New("injected interruption")
		}
		return nil
	}
	count, err := store.CommitCatalogPurge(ctx, plan, purgeNoReferences)
	if count != 0 || !slices.ContainsFunc(diagnostics.Of(err), func(d diagnostics.Diagnostic) bool {
		return strings.Contains(d.Message, image) && strings.Contains(d.Message, "partially removed") &&
			strings.Contains(d.Remediation, "bootwright catalog media delete")
	}) {
		t.Fatalf("partial withdrawal was not explained: %d, %+v", count, diagnostics.Of(err))
	}
	if _, err := store.PlanCatalogPurge(ctx, catalogpurge.Media, true, purgeNoReferences); err == nil {
		t.Fatal("an incomplete media entry was treated as a qualified registration")
	}
}

func TestCatalogPurgePreservesARetainedMediaReplacementThatAppearsAfterPreview(t *testing.T) {
	ctx := t.Context()
	store := mediaFixture(t)
	const image = "install.iso"
	addMedia(t, store, image, "original image", false)
	plan, err := store.PlanCatalogPurge(ctx, catalogpurge.Media, true, purgeNoReferences)
	if err != nil {
		t.Fatal(err)
	}
	retained := retainStage(t, store, image, "replacement image")
	if count, err := store.CommitCatalogPurge(ctx, plan, purgeNoReferences); count != 1 || err != nil {
		t.Fatalf("purge could not withdraw just the registration: %d %v", count, err)
	}
	expectMedia(t, store, map[string]string{})
	if err := store.ReadMedia(ctx, func(view media.View) error {
		got, err := view.Retained(ctx)
		if !slices.Equal(got, []managedos.MediaEntry{retained}) {
			t.Fatalf("purge withdrew an unreviewed retained import: %+v", got)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
}
