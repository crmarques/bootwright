package prerequisites

import (
	"context"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/controller"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

type rebindStore struct {
	recorded controller.InstalledHostIdentity
	rebound  int
}

func (s *rebindStore) ReadController(_ context.Context, _ string, read func(StorageView) error) error {
	return read(StorageView{Exists: true, Initialized: true, State: HostState{Host: s.recorded}})
}

func (s *rebindStore) RebindController(_ context.Context, recorded, next controller.InstalledHostIdentity) (RebindResult, error) {
	if !recorded.Equal(s.recorded) {
		return RebindResult{}, failure("controller.identity", "changed", "")
	}
	s.recorded, s.rebound = next, s.rebound+1
	return RebindResult{Outcome: "rebound", Bindings: 1}, nil
}

type identityHost struct {
	identity controller.InstalledHostIdentity
}

func (h identityHost) Platform(context.Context) (Platform, error) { return Platform{}, nil }

func (h identityHost) Identity(context.Context) (controller.InstalledHostIdentity, error) {
	return h.identity, nil
}

type askedRebind struct {
	action, name string
	refuse       error
}

func (a *askedRebind) Confirm(_ context.Context, action, name string) error {
	a.action, a.name = action, name
	return a.refuse
}

func installedHost(t *testing.T, machine string) controller.InstalledHostIdentity {
	t.Helper()
	host, err := controller.NewInstalledHostIdentity(controller.LinuxInstalledIdentityV1, strings.Repeat(machine, 32), "12345678-1234-5678-9abc-def012345678", "fedcba98-7654-3210-fedc-ba9876543210")
	if err != nil {
		t.Fatal(err)
	}
	return host
}

// A rebind asks naming both identities and rewrites the record only once it
// is confirmed; --yes rebinds only with --recorded-host-retired and this
// host's three values named; and
// an identity that did not change refuses before anything is asked.
func TestARebindAsksNamingBothIdentitiesOrTakesThemNamed(t *testing.T) {
	ctx := context.Background()
	old, current := installedHost(t, "1"), installedHost(t, "2")
	service := func(store *rebindStore, asked *askedRebind) Service {
		return New(store, nil, identityHost{current}, Ports{}, Options{Confirmer: asked})
	}
	store, asked := &rebindStore{recorded: old}, &askedRebind{refuse: failure("controller.identity", "rebind confirmation was declined", "")}
	if _, err := service(store, asked).Rebind(ctx, RebindRequest{}); err == nil || store.rebound != 0 {
		t.Fatalf("a declined rebind = %v, rebound %d", err, store.rebound)
	}
	if asked.action != "controller rebind" || !strings.Contains(asked.name, "machine-id "+old.MachineID()) || !strings.Contains(asked.name, "to machine-id "+current.MachineID()) ||
		!strings.Contains(asked.name, "DMI product UUID") || !strings.Contains(asked.name, "root filesystem UUID") {
		t.Fatalf("the confirmation asked %q %q", asked.action, asked.name)
	}
	for _, request := range []RebindRequest{
		{SkipConfirmation: true, RecordedRetired: true},
		{SkipConfirmation: true, RecordedRetired: true, MachineID: current.MachineID(), ProductUUID: current.ProductUUID()},
		{SkipConfirmation: true, RecordedRetired: true, MachineID: old.MachineID(), ProductUUID: current.ProductUUID(), FilesystemUUID: current.FilesystemUUID()},
	} {
		_, err := service(store, &askedRebind{}).Rebind(ctx, request)
		if reported := diagnostics.Of(err); len(reported) != 1 || !strings.Contains(reported[0].Message, "--yes rebinds only to the host identity it names") || store.rebound != 0 {
			t.Fatalf("--yes with %+v reported %#v", request, reported)
		}
	}
	named := RebindRequest{SkipConfirmation: true, MachineID: current.MachineID(), ProductUUID: current.ProductUUID(), FilesystemUUID: current.FilesystemUUID()}
	_, err := service(store, nil).Rebind(ctx, named)
	if reported := diagnostics.Of(err); len(reported) != 1 || !strings.Contains(reported[0].Message, "--recorded-host-retired") || store.rebound != 0 {
		t.Fatalf("--yes without --recorded-host-retired reported %#v", reported)
	}
	named.RecordedRetired = true
	if result, err := service(store, nil).Rebind(ctx, named); err != nil || result.Outcome != "rebound" || store.rebound != 1 {
		t.Fatalf("a named --yes rebind = %+v (%v)", result, err)
	}
	unasked := &askedRebind{}
	_, err = service(store, unasked).Rebind(ctx, RebindRequest{})
	if reported := diagnostics.Of(err); len(reported) != 1 || !strings.Contains(reported[0].Message, "already names this host") || unasked.action != "" || store.rebound != 1 {
		t.Fatalf("an unchanged identity reported %#v and asked %q", reported, unasked.action)
	}
}
