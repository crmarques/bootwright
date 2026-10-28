//go:build linux && amd64

package contextfs

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/workspace/contexts"
)

// One physical server addressed by a DNS name in one context and by an IP
// literal in another holds two different bmc: keys, so registration admits
// both; the system UUID the first proof recorded is what refuses the second,
// claimed beside its bmc: key before any media is inserted, naming the context
// that holds it (B802).
func TestTwoSpellingsOfOneControllerRefuseTheSecondSystemClaim(t *testing.T) {
	ctx := context.Background()
	store, record := lifecycleFixture(t)
	_, sources := fixture(t)
	publish(t, store, "second", sources)
	reserveFixture(t, store, record)
	claim := func(name, endpoint string) error {
		return store.MutateLifecycle(ctx, name, func(tx lifecycle.Transaction) error {
			planned := prerequisites.HostReservation{Context: name, Kind: "substrate-physical", Service: "metal-01", Keys: []string{endpoint}}
			if err := tx.Reserve(ctx, []prerequisites.HostReservation{planned}); err != nil {
				return err
			}
			planned.Keys = append(planned.Keys, "bmc-system:4c4c4544-0042-3510-8052-b4c04f4d4e31")
			return tx.Claim(ctx, planned)
		})
	}
	if err := claim(record.Name, "bmc:bmc-01.metal.example.test:443/1"); err != nil {
		t.Fatalf("the first context's claim failed: %#v", diagnostics.Of(err))
	}
	err := claim("second", "bmc:198.51.100.21:443/1")
	reported := diagnostics.Of(err)
	if len(reported) == 0 || reported[0].Code != "controller.conflict" || !strings.Contains(reported[0].Message, record.Name) ||
		!strings.Contains(reported[0].Message, "bmc-system reservation") {
		t.Fatalf("a second spelling of one controller claimed its system: %#v", reported)
	}
	if err := store.ReadController(ctx, "", func(view prerequisites.StorageView) error {
		for _, reservation := range view.State.Reservations {
			if reservation.Context == "second" && len(reservation.Keys) != 1 {
				t.Fatalf("the refused claim was published: %+v", reservation)
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// A bridge-use claim an abandoned context left never blocks another context's
// removal for good: the orphan-acknowledged deletion of that context releases
// it with every other key it held (D38), so the managed bridge's removal is
// stranded only while the using context is still a context (B803).
func TestAnAbandonedContextsBridgeUseIsReleasedWithItsDeletion(t *testing.T) {
	ctx := context.Background()
	store, record := lifecycleFixture(t)
	_, sources := fixture(t)
	publish(t, store, "second", sources)
	reserveFixture(t, store, record)
	reserve := func(name string, reservation prerequisites.HostReservation) {
		t.Helper()
		if err := store.MutateLifecycle(ctx, name, func(tx lifecycle.Transaction) error {
			if err := tx.Reserve(ctx, []prerequisites.HostReservation{reservation}); err != nil {
				return err
			}
			return tx.PublishEvidence(ctx, []byte(`{"version":1,"operation":"unknown","ownership":"retained"}`))
		}); err != nil {
			t.Fatalf("%s's reservation failed: %#v", name, diagnostics.Of(err))
		}
	}
	reserve(record.Name, prerequisites.HostReservation{Context: record.Name, Kind: "substrate-host-use", Service: "edge",
		Keys: []string{"bridge-use:virbr-lab"}, Shared: true})
	reserve("second", prerequisites.HostReservation{Context: "second", Kind: "substrate-host", Service: "lab", Keys: []string{"bridge:virbr-lab"}})
	stranded := func() []prerequisites.Conflict {
		var found []prerequisites.Conflict
		if err := store.ReadController(ctx, "", func(view prerequisites.StorageView) error {
			found = prerequisites.ReleaseConflicts(view.State.Reservations, "second")
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		return found
	}
	if found := stranded(); len(found) != 1 || found[0].Held.Context != record.Name {
		t.Fatalf("the live user's claim strands %+v", found)
	}
	if err := store.Transact(ctx, false, nil, func(tx contexts.Transaction) error {
		if _, err := tx.MutationState(ctx, record.Name); err != nil {
			return err
		}
		return tx.Delete(ctx, record)
	}); err != nil {
		t.Fatalf("deleting the using context failed: %#v", diagnostics.Of(err))
	}
	if found := stranded(); len(found) != 0 {
		t.Fatalf("the deleted context's bridge-use still strands %+v", found)
	}
}

// A removal's re-proof reads the controller record as it is now, not as its
// transaction opened: a bridge-use another context publishes while this
// context's transaction is open is what the re-proof finds (B803).
func TestAReproofReadsAClaimPublishedAfterTheTransactionOpened(t *testing.T) {
	ctx := context.Background()
	store, record := lifecycleFixture(t)
	_, sources := fixture(t)
	publish(t, store, "second", sources)
	reserveFixture(t, store, record)
	use := prerequisites.HostReservation{Context: record.Name, Kind: "substrate-host-use", Service: "edge",
		Keys: []string{"bridge-use:virbr-lab"}, Shared: true}
	var found error
	if err := store.MutateLifecycle(ctx, "second", func(tx lifecycle.Transaction) error {
		if err := tx.Reserve(ctx, []prerequisites.HostReservation{{Context: "second", Kind: "substrate-host", Service: "lab", Keys: []string{"bridge:virbr-lab"}}}); err != nil {
			return err
		}
		if err := store.MutateLifecycle(ctx, record.Name, func(other lifecycle.Transaction) error {
			return other.Reserve(ctx, []prerequisites.HostReservation{use})
		}); err != nil {
			return err
		}
		if len(prerequisites.ReleaseConflicts(tx.Controller().State.Reservations, "second")) != 0 {
			t.Fatal("the transaction's snapshot already held the later claim")
		}
		found = tx.ReproveReservations(ctx, func(held []prerequisites.HostReservation) error {
			return prerequisites.ReleaseFailure(held, "second", "InfraProvider", "lab")
		}, nil)
		return nil
	}); err != nil {
		t.Fatalf("the transactions failed: %#v", diagnostics.Of(err))
	}
	if reported := diagnostics.Of(found); len(reported) != 1 || reported[0].Code != "controller.conflict" || !strings.Contains(reported[0].Message, "context "+record.Name) {
		t.Fatalf("the re-proof read %#v", reported)
	}
}

// A removal's re-proof marks the claim its effect releases under the same root
// lock, so another context that claims the guarded key while the effect runs,
// or after an interrupted effect, refuses naming the removing context, and the
// removal still completes and releases what it held (B839).
func TestAClaimOnAKeyARemovalIsReleasingRefusesUntilTheRemovalCompletes(t *testing.T) {
	ctx := context.Background()
	scenarios := map[string]struct {
		removing, using     prerequisites.HostReservation
		kind, object, named string
	}{
		"a bridge another context starts using": {
			removing: prerequisites.HostReservation{Context: "second", Kind: "substrate-host", Service: "lab", Keys: []string{"bridge:virbr-lab"}},
			using:    prerequisites.HostReservation{Kind: "substrate-host-use", Service: "edge", Keys: []string{"bridge-use:virbr-lab"}, Shared: true},
			kind:     "InfraProvider", object: "lab", named: "is removing bridge virbr-lab that InfraProvider/lab holds",
		},
		"an emulated BMC endpoint another context drives": {
			removing: prerequisites.HostReservation{Context: "second", Kind: "substrate-machine-bmc", Service: "node-01",
				Keys: []string{"bmc:192.0.2.1:8000/node-01"}, Shared: true},
			using: prerequisites.HostReservation{Kind: "substrate-physical", Service: "metal-01", Keys: []string{"bmc:192.0.2.1:8000/node-01"}},
			kind:  "Machine", object: "node-01", named: "is removing the management controller endpoint that Machine/node-01 holds",
		},
	}
	for name, scenario := range scenarios {
		for _, interrupted := range []bool{false, true} {
			t.Run(name+map[bool]string{false: "", true: " after an interrupted effect"}[interrupted], func(t *testing.T) {
				store, record := lifecycleFixture(t)
				_, sources := fixture(t)
				publish(t, store, "second", sources)
				reserveFixture(t, store, record)
				using := scenario.using
				using.Context = record.Name
				use := func() error {
					return store.MutateLifecycle(ctx, record.Name, func(tx lifecycle.Transaction) error {
						return tx.Reserve(ctx, []prerequisites.HostReservation{using})
					})
				}
				refused := func(err error) {
					t.Helper()
					reported := diagnostics.Of(err)
					if len(reported) != 1 || reported[0].Code != "controller.conflict" || !strings.Contains(reported[0].Message, "context second "+scenario.named) ||
						reported[0].Remediation != "complete that removal with bootwright destroy --context second, then repeat this operation" {
						t.Fatalf("a claim during the removal = %#v", reported)
					}
				}
				if err := store.MutateLifecycle(ctx, "second", func(tx lifecycle.Transaction) error {
					return tx.Reserve(ctx, []prerequisites.HostReservation{scenario.removing})
				}); err != nil {
					t.Fatalf("the removing context's claim failed: %#v", diagnostics.Of(err))
				}
				if err := store.MutateLifecycle(ctx, "second", func(tx lifecycle.Transaction) error {
					if err := tx.ReproveReservations(ctx, func(held []prerequisites.HostReservation) error {
						return prerequisites.ReleaseFailure(held, "second", scenario.kind, scenario.object)
					}, func(held prerequisites.HostReservation) bool {
						return prerequisites.Releases(held, scenario.kind, scenario.object)
					}); err != nil {
						return err
					}
					refused(use())
					if interrupted {
						return nil
					}
					return tx.ReleaseReservations(ctx)
				}); err != nil {
					t.Fatalf("the removal failed: %#v", diagnostics.Of(err))
				}
				if interrupted {
					refused(use())
					if err := store.MutateLifecycle(ctx, "second", func(tx lifecycle.Transaction) error {
						return tx.ReleaseReservations(ctx)
					}); err != nil {
						t.Fatalf("the continued removal failed: %#v", diagnostics.Of(err))
					}
				}
				if err := use(); err != nil {
					t.Fatalf("a claim after the removal completed refused: %#v", diagnostics.Of(err))
				}
			})
		}
	}
}

// Two contexts that reach one SSH host by two spellings, an IP and a DNS name,
// qualify their claims by two addresses but by one pinned host key, so a
// socket the second claims there refuses beside the first's wildcard, by the
// host-key form, naming the host by that key; a host
// pinning another key is another host and admits it (B844, D188).
func TestTwoSpellingsOfOneSSHHostConflictByItsPinnedHostKey(t *testing.T) {
	ctx := context.Background()
	key := func(seed byte) string {
		blob := binary.BigEndian.AppendUint32(nil, uint32(len("ssh-ed25519")))
		blob = append(blob, "ssh-ed25519"...)
		blob = binary.BigEndian.AppendUint32(blob, 32)
		return base64.StdEncoding.EncodeToString(append(blob, bytes.Repeat([]byte{seed}, 32)...))
	}
	claim := func(t *testing.T, name, address, socket string, seed byte) prerequisites.HostReservation {
		t.Helper()
		pinned, err := machine.SSHHostKeyQualifier(address+" ssh-ed25519 "+key(seed)+"\n", address, 22)
		if err != nil {
			t.Fatal(err)
		}
		keys := []string{"socket:" + socket + ":3128", "unit:bootwright-" + name}
		return prerequisites.HostReservation{Context: name, Kind: "proxy", Service: "gamma",
			Keys: append(prerequisites.QualifiedKeys(machine.SSHReservationQualifier(address, 22), keys), prerequisites.QualifiedKeys(pinned, keys)...)}
	}
	for name, test := range map[string]struct {
		seed     byte
		conflict bool
	}{
		"one pinned host key": {seed: 1, conflict: true},
		"another host key":    {seed: 2},
	} {
		t.Run(name, func(t *testing.T) {
			store, record := lifecycleFixture(t)
			_, sources := fixture(t)
			publish(t, store, "second", sources)
			reserveFixture(t, store, record)
			reserve := func(reservation prerequisites.HostReservation) error {
				return store.MutateLifecycle(ctx, reservation.Context, func(tx lifecycle.Transaction) error {
					return tx.Reserve(ctx, []prerequisites.HostReservation{reservation})
				})
			}
			if err := reserve(claim(t, record.Name, "192.0.2.10", "0.0.0.0", 1)); err != nil {
				t.Fatalf("the first spelling's claim failed: %#v", diagnostics.Of(err))
			}
			err := reserve(claim(t, "second", "services.lab.example.test", "192.0.2.10", test.seed))
			reported := diagnostics.Of(err)
			if !test.conflict {
				if err != nil {
					t.Fatalf("a host pinning another key refused: %#v", reported)
				}
				return
			}
			if len(reported) == 0 || reported[0].Code != "controller.conflict" || !strings.Contains(reported[0].Message, "context "+record.Name) ||
				!strings.Contains(reported[0].Message, "on the SSH host whose pinned host key is SHA256:") {
				t.Fatalf("a second spelling of one SSH host = %#v", reported)
			}
		})
	}
}
