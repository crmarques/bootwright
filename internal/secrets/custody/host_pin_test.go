package custody

import (
	"context"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/secrets/secretstore"
)

type pinHolds struct{ holds Holds }

func (h *pinHolds) SecretHolds(context.Context, string) (Holds, error) { return h.holds, nil }

func TestHostPinPublicationRechecksCurrentVersionAndEffectHolds(t *testing.T) {
	svc, access, _, _ := serviceFixture(t, "\n---\napiVersion: bootwright.io/v1alpha1\nkind: Secret\nmetadata: {name: pin}\nspec: {type: opaque}\n")
	ctx := context.Background()
	reader := &pinHolds{}
	svc = svc.WithHolds(reader)
	read := func() HostPin {
		t.Helper()
		pin, err := svc.ReadHostPin(ctx, "fixture", func(api.Catalog) (string, error) { return "pin", nil })
		if err != nil {
			t.Fatal(err)
		}
		return pin
	}
	initial := read()
	id, err := svc.StoreHostPin(ctx, initial, "first")
	if err != nil || id == "" {
		t.Fatalf("initial publication = %q, %v", id, err)
	}
	if _, err := svc.StoreHostPin(ctx, initial, "stale overwrite"); !hostPinConflict(err) {
		t.Fatalf("stale publication = %v", err)
	}
	current := read()
	if current.Version != id || string(current.Value) != "first" {
		t.Fatalf("current = %q %q", current.Version, current.Value)
	}
	if err := svc.CheckHostPinWrite(ctx, current); err != nil {
		t.Fatal(err)
	}
	access.session.snapshot.Bindings = []secretstore.Binding{{ID: "bind", Versions: []string{id}}}
	reader.holds.Bindings = []Hold{{Binding: "bind", Operation: "apply", Secrets: []string{"pin"}}}
	if err := svc.CheckHostPinWrite(ctx, current); !hostPinConflict(err) {
		t.Fatalf("held preview = %v", err)
	}
	if _, err := svc.StoreHostPin(ctx, current, "held overwrite"); !hostPinConflict(err) {
		t.Fatalf("held publication = %v", err)
	}
	if access.session.writes != 1 {
		t.Fatalf("writes = %d", access.session.writes)
	}
}

func hostPinConflict(err error) bool {
	found := diagnostics.Of(err)
	return len(found) == 1 && found[0].Code == "secret.store.conflict"
}
