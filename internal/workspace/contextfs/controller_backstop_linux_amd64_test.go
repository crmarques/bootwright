package contextfs

import (
	"context"
	"fmt"
	"strconv"
	"testing"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

func expectBackstop(t *testing.T, err error, message, remediation string) {
	t.Helper()
	reported := diagnostics.Of(err)
	if len(reported) != 1 || reported[0].Code != "controller.conflict" || reported[0].Message != message || reported[0].Remediation != remediation {
		t.Fatalf("want controller.conflict %q with the remedy %q, got %v %#v", message, remediation, err, reported)
	}
}

// expectSetupBound requires the store's refusal of a publication that finds the
// host's bound held: a conflict whose remedy is setup's purge.
func expectSetupBound(t *testing.T, err error) {
	t.Helper()
	reported := diagnostics.Of(err)
	if len(reported) != 1 || reported[0].Code != "controller.conflict" || reported[0].Remediation != setupBoundRemedy {
		t.Fatalf("want a controller.conflict with the remedy %q, got %v %#v", setupBoundRemedy, err, reported)
	}
}

// The store decides the host's bundle bound again when setup publishes. With
// client areas holding every other area and one resolution retained, the
// resolution has room and the bundle does not, so the publication refuses
// naming the bound and the remedy.
func TestSetupsStoreBackstopAtTheBundleBoundNamesItsRemedy(t *testing.T) {
	ctx := context.Background()
	store, record := lifecycleFixture(t)
	sealedBundleFixture(t, store, record)
	for index := 1; index < maxControllerBundles; index++ {
		if err := publishClients(store, fmt.Sprintf("%064x", index), true); err != nil {
			t.Fatalf("client area %d: %#v", index, diagnostics.Of(err))
		}
	}
	next := revisionReceipt(t, automationRevision(t, 1))
	err := store.MutateController(ctx, prerequisites.SetupContext{}, true, func(tx prerequisites.StorageTransaction) error {
		_, err := tx.Publish(ctx, next)
		return err
	})
	expectBackstop(t, err, "this host already holds the "+strconv.Itoa(maxControllerBundles)+" bundle areas it may hold, so setup's new execution bundle has no room", setupBoundRemedy)
}

// A new resolution or dependency source beyond what the host retains refuses
// with the remedy that fits: purging for a resolution, another host for a
// source this build never retires.
func TestSetupsStoreBackstopsAtTheResolutionAndDependencyLimitsNameTheirRemedy(t *testing.T) {
	t.Run("resolutions", func(t *testing.T) {
		var before, next prerequisites.HostState
		for revision := range maxControllerBundles {
			before.RetainedDefinitions = append(before.RetainedDefinitions, automationRevision(t, revision))
		}
		definition := automationRevision(t, maxControllerBundles)
		next.Receipt.Definition = &definition
		_, err := retainControllerSources(before, next)
		expectBackstop(t, err, "this host already retains the "+strconv.Itoa(maxControllerBundles)+" dependency resolutions it may hold, so setup's new resolution has no room", setupBoundRemedy)
	})
	t.Run("dependency sources", func(t *testing.T) {
		var before, next prerequisites.HostState
		for index := range maxControllerRetainedSources {
			before.RetainedSources = append(before.RetainedSources, prerequisites.DependencySource{ID: fmt.Sprintf("source-%04d", index), URL: "https://example.invalid/source", SHA256: "0", Bytes: 1})
		}
		next.RetainedSources = append(next.RetainedSources, before.RetainedSources...)
		next.RetainedSources = append(next.RetainedSources, prerequisites.DependencySource{ID: "source-new", URL: "https://example.invalid/source", SHA256: "0", Bytes: 1})
		_, err := retainControllerSources(before, next)
		expectBackstop(t, err, "this host already retains the "+strconv.Itoa(maxControllerRetainedSources)+" dependency source identities it may hold, so this resolution's sources have no room", retainedSourcesRemedy)
	})
	t.Run("a client resolution's dependency sources", func(t *testing.T) {
		var value prerequisites.HostState
		for index := range maxControllerRetainedSources {
			value.RetainedSources = append(value.RetainedSources, prerequisites.DependencySource{ID: fmt.Sprintf("source-%04d", index), URL: "https://example.invalid/source", SHA256: "0", Bytes: 1})
		}
		added := []prerequisites.DependencySource{{ID: "source-new", URL: "https://example.invalid/source", SHA256: "0", Bytes: 1}}
		_, err := retainDependencies(value, nil, nil, added, nil)
		expectBackstop(t, err, "this host already retains the "+strconv.Itoa(maxControllerRetainedSources)+" dependency source identities it may hold, so this resolution's sources have no room", retainedSourcesRemedy)
	})
}
