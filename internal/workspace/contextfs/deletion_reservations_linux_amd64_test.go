//go:build linux && amd64

package contextfs

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/infrastructureservices"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/workspace/contexts"
)

// A context deleted with the objects it owns abandoned still holds the host
// keys its operation reserved, and no context is left to release them. The
// deletion releases exactly those, beside the rest of what the controller
// record holds for it, so another context can then reserve the same keys,
// and every other context's reservations stay.
func TestADeletedContextsReservationsAreReleasedForAnotherContext(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store, record := lifecycleFixture(t)
	_, sources := fixture(t)
	publish(t, store, "second", sources)
	publish(t, store, "third", sources)
	reserveFixture(t, store, record)
	machine := []string{"libvirt-domain:lab-rhel-01", "path:/var/lib/bootwright/machines/rhel-01", "socket:192.0.2.1:8000", "unit:bootwright-bmc-lab-rhel-01"}
	reserve := func(name string, keys []string) error {
		return store.MutateLifecycle(ctx, name, func(tx lifecycle.Transaction) error {
			return tx.Reserve(ctx, []infrastructureservices.HostReservation{{Context: name, Kind: "substrate-machine", Service: "rhel-01", Keys: keys}})
		})
	}
	if err := store.MutateLifecycle(ctx, record.Name, func(tx lifecycle.Transaction) error {
		if err := tx.Reserve(ctx, []infrastructureservices.HostReservation{{Context: record.Name, Kind: "substrate-machine", Service: "rhel-01", Keys: machine}}); err != nil {
			return err
		}
		return tx.PublishEvidence(ctx, []byte(`{"version":1,"operation":"unknown","ownership":"retained"}`))
	}); err != nil {
		t.Fatalf("the abandoned context's reservation failed: %#v", diagnostics.Of(err))
	}
	if err := reserve("third", []string{"unit:bootwright-proxy"}); err != nil {
		t.Fatalf("an unrelated reservation failed: %#v", diagnostics.Of(err))
	}
	err := reserve("second", machine)
	if reported := diagnostics.Of(err); len(reported) == 0 || reported[0].Code != "controller.conflict" || !strings.Contains(reported[0].Message, record.Name) {
		t.Fatalf("a held key was reserved again: %#v", reported)
	}
	var released []string
	if err := store.Transact(ctx, false, nil, func(tx contexts.Transaction) error {
		if _, err := tx.MutationState(ctx, record.Name); err != nil {
			return err
		}
		keys, err := tx.HostReservations(ctx, record.Name)
		if err != nil {
			return err
		}
		released = keys
		return tx.Delete(ctx, record)
	}); err != nil {
		t.Fatalf("deleting the context failed: %#v", diagnostics.Of(err))
	}
	if !slices.Equal(released, machine) {
		t.Fatalf("the deletion reported %v, want %v", released, machine)
	}
	if err := store.ReadController(ctx, "", func(view prerequisites.StorageView) error {
		for _, reservation := range view.State.Reservations {
			if reservation.Context == record.Name {
				t.Fatalf("the deleted context still reserves %v", reservation.Keys)
			}
		}
		if len(view.State.Reservations) != 1 || view.State.Reservations[0].Context != "third" {
			t.Fatalf("another context's reservation did not survive: %+v", view.State.Reservations)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := reserve("second", machine); err != nil {
		t.Fatalf("another context could not reserve the released keys: %#v", diagnostics.Of(err))
	}
}

// A deletion drops what the controller record holds for the context after it
// marks the context deleting and before it removes any file, so a deletion
// stopped before its directory goes has already released its reservations, and
// its retry finds them released and completes without naming them again.
func TestADeletionRetryAfterItsClaimsWereDroppedFindsThemReleased(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store, record := lifecycleFixture(t)
	reserveFixture(t, store, record)
	keys := []string{"unit:bootwright-bmc-lab-rhel-01"}
	if err := store.MutateLifecycle(ctx, record.Name, func(tx lifecycle.Transaction) error {
		if err := tx.Reserve(ctx, []infrastructureservices.HostReservation{{Context: record.Name, Kind: "substrate-machine", Service: "rhel-01", Keys: keys}}); err != nil {
			return err
		}
		return tx.PublishEvidence(ctx, []byte(`{"version":1,"operation":"unknown","ownership":"retained"}`))
	}); err != nil {
		t.Fatalf("the context's reservation failed: %#v", diagnostics.Of(err))
	}
	claimed := func() int {
		count := 0
		if err := store.ReadController(ctx, "", func(view prerequisites.StorageView) error {
			for _, reservation := range view.State.Reservations {
				if reservation.Context == record.Name {
					count++
				}
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		return count
	}
	if claimed() != 1 {
		t.Fatalf("the context holds %d reservations before its deletion", claimed())
	}
	store.fail = func(at string) error {
		if at == string(checkpointBeforeContextRmdir) {
			return errors.New("synthetic stop before the directory goes")
		}
		return nil
	}
	if err := deleteExample(store, record); err == nil {
		t.Fatal("the stopped deletion succeeded")
	}
	store.fail = nil
	if claimed() != 0 {
		t.Fatal("a deletion stopped before its directory went still holds its reservations")
	}
	var retried []string
	if err := store.Transact(ctx, false, nil, func(tx contexts.Transaction) error {
		var err error
		retried, err = tx.HostReservations(ctx, record.Name)
		return err
	}); err != nil || len(retried) != 0 {
		t.Fatalf("the retry names %v released keys (%v)", retried, err)
	}
	if err := deleteExample(store, record); err != nil {
		t.Fatalf("the retry failed: %#v", diagnostics.Of(err))
	}
	if _, err := os.Lstat(filepath.Join(store.options.Root, "contexts", record.Name)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the retried deletion left the context directory (%v)", err)
	}
}

// The controller record's byte bound is reached before its entry bound when
// every key is maximal: a publication past it refuses context.state before it
// writes, and the reservations it held stay as they were.
func TestAReservationSetPastTheRecordByteBoundRefusesWritingNothing(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store, record := lifecycleFixture(t)
	reserveFixture(t, store, record)
	claims := make([]infrastructureservices.HostReservation, maxControllerReservations)
	for index := range claims {
		keys := make([]string, 64)
		for key := range keys {
			prefix := fmt.Sprintf("path:/bytes/%03d/%02d/", index, key)
			keys[key] = prefix + strings.Repeat("a", 256-len(prefix))
		}
		claims[index] = infrastructureservices.HostReservation{Context: record.Name, Kind: "substrate-machine", Service: fmt.Sprintf("machine-%03d", index), Keys: keys}
	}
	err := store.MutateLifecycle(ctx, record.Name, func(tx lifecycle.Transaction) error { return tx.Reserve(ctx, claims) })
	reported := diagnostics.Of(err)
	if len(reported) != 1 || reported[0].Code != "context.state" || reported[0].Message != "persisted record exceeds its encoding limit" {
		t.Fatalf("reservations past the byte bound reported %#v", reported)
	}
	if err := store.ReadController(ctx, "", func(view prerequisites.StorageView) error {
		if len(view.State.Reservations) != 0 {
			t.Fatalf("the refused publication left %d reservations", len(view.State.Reservations))
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// Reservations that would take the controller record past its bound refuse
// controller.conflict before anything is published, naming the bound, the
// entries this apply needs and those other contexts hold, with the exits that
// free some; at the bound they are published.
func TestReservationsPastTheRecordBoundRefuseBeforePublication(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store, record := lifecycleFixture(t)
	_, sources := fixture(t)
	publish(t, store, "second", sources)
	reserveFixture(t, store, record)
	claims := func(name string, count int) []infrastructureservices.HostReservation {
		claimed := make([]infrastructureservices.HostReservation, count)
		for index := range claimed {
			service := fmt.Sprintf("machine-%03d", index)
			claimed[index] = infrastructureservices.HostReservation{Context: name, Kind: "substrate-machine", Service: service, Keys: []string{"unit:bootwright-bmc-" + name + "-" + service}}
		}
		return claimed
	}
	reserve := func(name string, count int) error {
		return store.MutateLifecycle(ctx, name, func(tx lifecycle.Transaction) error { return tx.Reserve(ctx, claims(name, count)) })
	}
	if err := reserve(record.Name, 200); err != nil {
		t.Fatalf("200 reservations refused: %#v", diagnostics.Of(err))
	}
	err := reserve("second", maxControllerReservations-200+1)
	reported := diagnostics.Of(err)
	if len(reported) != 1 || reported[0].Code != "controller.conflict" ||
		reported[0].Message != "the controller record holds at most 256 host reservations: this apply needs 57 and other contexts hold 200" ||
		!strings.Contains(reported[0].Remediation, "bootwright destroy --context <name>") || !strings.Contains(reported[0].Remediation, "bootwright context delete --name <name> --purge") {
		t.Fatalf("reservations past the bound reported %#v", reported)
	}
	var held []string
	if err := store.Transact(ctx, false, nil, func(tx contexts.Transaction) error {
		var err error
		held, err = tx.HostReservations(ctx, "second")
		return err
	}); err != nil || len(held) != 0 {
		t.Fatalf("the refused reservations left %v (%v)", held, err)
	}
	if err := reserve("second", maxControllerReservations-200); err != nil {
		t.Fatalf("reservations at the bound refused: %#v", diagnostics.Of(err))
	}
}

// An abandoning deletion names each installer work area the context's cluster
// media or controller-local managed-OS installations still claim, by its
// object and the Machine the context is bound to, and no other kind of claim,
// no path and no other context's area.
func TestADeletionNamesEachInstallerWorkAreaAndItsMachine(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store, record := lifecycleFixture(t)
	_, sources := fixture(t)
	publish(t, store, "second", sources)
	reserveFixture(t, store, record)
	claim := func(name, kind, service string, keys ...string) infrastructureservices.HostReservation {
		return infrastructureservices.HostReservation{Context: name, Kind: kind, Service: service, Keys: keys}
	}
	reserve := func(name string, claims ...infrastructureservices.HostReservation) {
		t.Helper()
		if err := store.MutateLifecycle(ctx, name, func(tx lifecycle.Transaction) error { return tx.Reserve(ctx, claims) }); err != nil {
			t.Fatalf("reserving for %s failed: %#v", name, diagnostics.Of(err))
		}
	}
	reserve(record.Name,
		claim(record.Name, infrastructureservices.ClusterMediaReservation, "sno", "path:/var/lib/bootwright-hosted/"+record.Name+"/clusters/sno", "path:/srv/artifacts/private/sno"),
		claim(record.Name, infrastructureservices.ClusterMediaReservation, "compact", "path:/var/lib/bootwright-hosted/"+record.Name+"/clusters/compact"),
		claim(record.Name, infrastructureservices.OSInstallReservation, "rhel-01", "path:/srv/artifacts/private/rhel-01"),
		claim(record.Name, "substrate-machine", "rhel-01", "unit:bootwright-bmc-rhel-01"))
	reserve("second", claim("second", infrastructureservices.ClusterMediaReservation, "other", "path:/var/lib/bootwright-hosted/second/clusters/other"))
	if err := store.MutateLifecycle(ctx, record.Name, func(tx lifecycle.Transaction) error {
		return tx.Bind(ctx, "controller", syntheticHost(t))
	}); err != nil {
		t.Fatalf("binding %s failed: %#v", record.Name, diagnostics.Of(err))
	}
	const machine = "controller"
	var areas []contexts.WorkArea
	var foreign error
	if err := store.Inspect(ctx, record.Name, func(tx contexts.Transaction) error {
		var err error
		areas, err = tx.InstallerWorkAreas(ctx, record.Name)
		_, foreign = tx.InstallerWorkAreas(ctx, "second")
		return err
	}); err != nil {
		t.Fatalf("reading the work areas failed: %#v", diagnostics.Of(err))
	}
	want := []contexts.WorkArea{
		{Kind: "cluster", Object: "compact", Machine: machine},
		{Kind: "cluster", Object: "sno", Machine: machine},
		{Kind: "machine", Object: "rhel-01", Machine: machine},
	}
	if !slices.Equal(areas, want) {
		t.Fatalf("the deletion names %+v, want %+v", areas, want)
	}
	if foreign == nil {
		t.Fatal("a deletion of one context read another context's work areas")
	}
}
