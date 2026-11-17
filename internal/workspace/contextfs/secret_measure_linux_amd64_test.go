//go:build linux && amd64

package contextfs

import (
	"context"
	"fmt"
	"testing"

	"github.com/crmarques/bootwright/internal/secrets/secretstore"
)

// A secret mutation is its area's only writer, so the area scans its storage
// once when it opens and then counts each write it makes itself: an exclusive
// write and a replacement. Writing many artifacts in one callback
// therefore scans once rather than once per write, and what it counted is
// what a fresh scan finds.
func TestTheSecretAreaScansItsStorageOncePerMutation(t *testing.T) {
	t.Parallel()
	store, sources := fixture(t)
	token := secretToken(publish(t, store, "example", sources))
	ctx := context.Background()
	if err := store.MutateSecrets(ctx, token, func(area secretstore.Area) error { return area.EnsureDirectory(ctx, "parts") }); err != nil {
		t.Fatal(err)
	}
	scans := 0
	store.fail = func(point string) error {
		if point == string(checkpointMeasureSecretStorage) {
			scans++
		}
		return nil
	}
	var counted, found secretUsage
	err := store.MutateSecrets(ctx, token, func(area secretstore.Area) error {
		for index := range 16 {
			if err := area.WriteExclusive(ctx, fmt.Sprintf("parts/artifact-%02d.enc", index), []byte("artifact")); err != nil {
				return err
			}
		}
		if err := area.WriteExclusive(ctx, "intent.json", []byte("intent")); err != nil {
			return err
		}
		if _, _, err := area.ReadMutable(ctx, "intent.json", 4096); err != nil {
			return err
		}
		if _, err := area.Replace(ctx, "intent.json", []byte("the next intent"), []byte("intent")); err != nil {
			return err
		}
		held := area.(*secretArea)
		if held.usage != nil {
			counted = *held.usage
		}
		entries, bytes, err := held.scan(ctx)
		found = secretUsage{entries: entries, bytes: bytes}
		return err
	})
	store.fail = nil
	if err != nil {
		t.Fatal(err)
	}
	if scans != 2 {
		t.Fatalf("a mutation of 18 writes scanned the secret storage %d times, want once when it opened and once to check", scans)
	}
	if counted != found {
		t.Fatalf("the area counted %+v, a fresh scan finds %+v", counted, found)
	}
}
