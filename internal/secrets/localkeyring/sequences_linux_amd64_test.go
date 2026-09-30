//go:build linux && amd64

package localkeyring

import (
	"context"
	"reflect"
	"slices"
	"testing"

	"github.com/crmarques/bootwright/internal/secrets/secretstore"
)

// Inspection reports every version as the version reader does, ordinal
// included, so secret list and secret check name a keyring's versions by
// v<sequence> instead of reporting none.
func TestInspectReportsEachVersionAsPublished(t *testing.T) {
	h := newIntegrationStore(t)
	credential, other := declaration("credential", "opaque", "contextStore"), declaration("other", "opaque", "contextStore")
	first, second := opaque("first"), opaque("second")
	defer first.Clear()
	defer second.Clear()
	published := map[string]secretstore.Version{}
	put := func(puts ...secretstore.Put) []secretstore.Version {
		t.Helper()
		var versions []secretstore.Version
		if err := h.mutate(func(session secretstore.StoreSession) error {
			var err error
			versions, err = session.PutBatch(context.Background(), puts)
			return err
		}); err != nil {
			t.Fatal(err)
		}
		for _, version := range versions {
			published[version.ID] = version
		}
		return versions
	}
	retained := put(secretstore.Put{Declaration: credential, Material: first})[0]
	if err := h.mutate(func(session secretstore.StoreSession) error {
		_, err := session.Bind(context.Background(), []secretstore.BoundInput{{Declaration: credential, Version: retained.ID, Material: first}})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	put(secretstore.Put{Declaration: credential, Material: second}, secretstore.Put{Declaration: other, Material: first})
	sequences := map[string][]int{}
	if err := h.view(func(session secretstore.StoreSession) error {
		snapshot, err := session.Inspect(context.Background())
		if err != nil {
			return err
		}
		if len(snapshot.Versions) != len(published) {
			t.Fatalf("inspection lists %d versions, %d were published", len(snapshot.Versions), len(published))
		}
		for _, version := range snapshot.Versions {
			if !reflect.DeepEqual(version, published[version.ID]) {
				t.Fatalf("inspection reports %#v, the reader published %#v", version, published[version.ID])
			}
			sequences[version.Declaration.Name] = append(sequences[version.Declaration.Name], version.Sequence)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	for _, series := range sequences {
		slices.Sort(series)
	}
	if !reflect.DeepEqual(sequences, map[string][]int{"credential": {1, 2}, "other": {1}}) {
		t.Fatalf("inspected ordinals: %v", sequences)
	}
}
