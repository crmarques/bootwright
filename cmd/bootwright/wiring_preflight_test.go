//go:build linux && amd64

package main

import (
	"context"
	"errors"
	"slices"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/machine"
	machineaccess "github.com/crmarques/bootwright/internal/machine/access"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/substrate/capacity"
	"github.com/crmarques/bootwright/internal/substrate/libvirt"
)

func TestStorageCapacityAdmissionSkipsTransitionsThatAllocateNoGuest(t *testing.T) {
	t.Parallel()
	sources := editedExample(t, "libvirt-3-nodes-ceph", exampleEdit{
		file: "infra/providers/lab-libvirt.yaml",
		old:  "    capacityReserve:\n      cpu: 1\n      memoryMiB: 2048\n      diskGiB: 10\n",
		new:  "",
	})
	state, _ := compileAcceptance(t, sources)
	catalog := state.Effective()
	allocations, err := libvirt.CapacityAllocations(catalog, "lab")
	if err != nil || len(allocations) == 0 {
		t.Fatalf("allocations = %v, %v", allocations, err)
	}
	block := func(stage reconciliation.Stage, kind, object string) reconciliation.Block {
		return reconciliation.Block{BlockDefinition: reconciliation.BlockDefinition{ID: kind + "/" + object, Stage: stage, Kind: kind, Object: object}}
	}
	owned := map[string]lifecycle.ObjectOwnership{}
	cases := map[string][]reconciliation.Block{
		"a controller-stage block":         {block(reconciliation.StageController, "ArtifactServer", "mirror")},
		"Machines done, only Ceph remains": {block(reconciliation.StageClusters, "CephCluster", "lab-ceph")},
		"a Machine no allocation projects": {block(reconciliation.StageMachines, libvirt.MachineKind, "absent")},
		"nothing left to start":            {},
	}
	for name, starting := range cases {
		if err := libvirtCapacityAdmission(nil)(context.Background(), "lab", catalog, owned, starting); err != nil {
			t.Fatalf("%s: admission = %v", name, diagnostics.Of(err))
		}
	}
	starting := []reconciliation.Block{block(reconciliation.StageClusters, "CephCluster", "lab-ceph"), block(reconciliation.StageMachines, libvirt.MachineKind, allocations[0].Machine)}
	err = libvirtCapacityAdmission(nil)(context.Background(), "lab", catalog, owned, starting)
	var failure *diagnostics.Failure
	if !errors.As(err, &failure) || len(failure.Diagnostics) == 0 {
		t.Fatalf("a transition starting a libvirt Machine with unknown capacity = %v", err)
	}
	for _, diagnostic := range failure.Diagnostics {
		if diagnostic.Code != "preflight.unknown" {
			t.Fatalf("diagnostic = %+v, want preflight.unknown", diagnostic)
		}
	}
}

type capacityCheckFunc func(context.Context, []capacity.Allocation, bool) ([]capacity.Check, error)

func (f capacityCheckFunc) Check(ctx context.Context, a []capacity.Allocation, _ []capacity.Write, dry bool) ([]capacity.Check, error) {
	return f(ctx, a, dry)
}

func TestContainerOnlyApplyProvesAllDeclaredLibvirtGuestsBeforeAdmission(t *testing.T) {
	t.Parallel()
	state, _ := compileAcceptance(t, exampleDirectory(t, "lab-sno"))
	catalog := state.Effective()
	if len(catalog.OfKind(api.CephCluster)) != 0 {
		t.Fatal("test must have no storage cluster")
	}
	allocations, err := libvirt.CapacityAllocations(catalog, "lab")
	if err != nil || len(allocations) != 1 {
		t.Fatalf("projection: %v %v", allocations, err)
	}
	called := 0
	checker := capacityCheckFunc(func(_ context.Context, got []capacity.Allocation, dry bool) ([]capacity.Check, error) {
		called++
		if dry || len(got) != len(allocations) || !got[0].Reserve.Configured || got[0].Context != "lab" {
			t.Fatalf("capacity admission lost declared allocation, reserve or context: %+v dry %t", got, dry)
		}
		return []capacity.Check{{ID: "capacity.memory", Target: got[0].Host, Status: capacity.Failed, Reason: "declared guests exceed available memory"}}, nil
	})
	block := reconciliation.Block{BlockDefinition: reconciliation.BlockDefinition{Kind: libvirt.MachineKind, Object: allocations[0].Machine}}
	if err := libvirt.AdmitCapacity(context.Background(), "lab", catalog, nil, []reconciliation.Block{block}, checker, nil); err == nil {
		t.Fatal("insufficient container-only host capacity admitted")
	}
	if err := libvirt.AdmitCapacity(context.Background(), "lab", catalog, nil, nil, checker, nil); err != nil {
		t.Fatal(err)
	}
	if called != 1 {
		t.Fatalf("continuation with all machine blocks done observed capacity: %d", called)
	}
}

func TestRemoteCapacityCommandsRefuseWithoutALifecycleOrAVerifiedRun(t *testing.T) {
	t.Parallel()
	for name, remote := range map[string]remoteCommands{
		"no lifecycle":          {},
		"an unavailable access": {reconciler: &lifecycle.Service{}},
	} {
		if out, _, err := remote.Run(context.Background(), "lab", "host", []string{"/usr/bin/nproc"}, 1<<10); err == nil || out != nil {
			t.Fatalf("%s: output %q, error %v", name, out, err)
		}
	}
}

func TestCapacitySelectsTheDedicatedIdentityWithoutChangingTheSession(t *testing.T) {
	const name = "host"
	spec := api.MapValue().
		WithPath(text("endpoint"), "access", "ssh", "addressRef").
		WithPath(text("operator"), "access", "ssh", "user").
		WithPath(text("operator-password"), "access", "ssh", "auth", "passwordRef").
		WithPath(text("automation"), "access", "lifecycle", "user").
		WithPath(text("automation-key"), "access", "lifecycle", "privateKeyRef").
		WithPath(text("lifecycle-pin"), "access", "lifecycle", "knownHostsRef").
		WithPath(text("sudo"), "access", "lifecycle", "privilege")
	endpoint := api.MapValue().With("name", text("endpoint")).With("address", text("192.0.2.10"))
	spec = spec.WithPath(api.ListValue(endpoint), "network", "addresses")
	host := api.NewObject(api.Machine, name, api.MapValue(), spec)
	effective := &compilation.EffectiveResult{Effective: api.NewCatalog([]api.Object{host})}
	projected, sudo := lifecycleAccess(effective, name)
	target, _ := projected.Effective.Find(api.Machine, name)
	login, ok := machine.SSH(target)
	if !ok || !sudo || login.User != "automation" || login.PrivateKeyRef != "automation-key" ||
		login.KnownHostsRef != "lifecycle-pin" || login.PasswordRef != "" || login.Address != "192.0.2.10" {
		t.Fatalf("capacity login = %+v, sudo = %t", login, sudo)
	}
	original, _ := effective.Effective.Find(api.Machine, name)
	session, _ := machine.SSH(original)
	if session.User != "operator" || session.PasswordRef != "operator-password" {
		t.Fatalf("the capacity projection changed operator access: %+v", session)
	}
	// No lifecycle runtime is injected, so the probe can resolve the account
	// but must refuse at the material boundary without reaching a host.
	remote := remoteCommands{reconciler: &lifecycle.Service{}, state: lifecycleAccessState{effective: effective}}
	if out, _, err := remote.Run(context.Background(), "lab", name, []string{"/usr/bin/nproc"}, 1024); err == nil || out != nil {
		t.Fatalf("unbound capacity probe returned %q and %v", out, err)
	}
}

func TestSSHLauncherResolvesTheInvokingAccountThroughComposition(t *testing.T) {
	client := sessionClient(nil, nil, nil, func(context.Context) (machineaccess.CopyAccount, error) {
		return machineaccess.CopyAccount{UID: 1234, GID: 2345, Groups: []uint32{4567}}, nil
	})
	account, err := client.Account(context.Background())
	if err != nil || account.UID != 1234 || account.GID != 2345 || len(account.Groups) != 1 || account.Groups[0] != 4567 {
		t.Fatalf("invoking SSH account = %+v, %v", account, err)
	}
}

func TestTheEscalatedCapacityProbeKeepsSudoRefusalApartFromAnAbsentPath(t *testing.T) {
	t.Parallel()
	probe := []string{"/usr/bin/stat", "--file-system", "--format=%a %S %i", "--", "/var/lib/$(touch x);`id`"}
	want := append([]string{"/usr/bin/sudo", "-H", "-n", "-k", "-u", "root", "--", "/bin/sh", "-c",
		`"$@"; status=$?; if [ "$status" -eq 1 ]; then exit 41; fi; exit "$status"`, "bootwright-capacity"}, probe...)
	if got := escalatedCapacityProbe(probe); !slices.Equal(got, want) {
		t.Fatalf("escalated probe = %q", got)
	}
	for _, test := range []struct {
		sudo         bool
		exit, status int
		refused      bool
	}{
		{false, 0, 0, false}, {false, 1, 1, false}, {false, 41, 41, false}, {false, 2, 2, false},
		{true, 0, 0, false}, {true, 1, 0, true}, {true, 41, 1, false}, {true, 2, 2, false},
	} {
		status, err := capacityProbeStatus(test.sudo, test.exit)
		if status != test.status || (err != nil) != test.refused {
			t.Fatalf("sudo %t exit %d = %d, %v", test.sudo, test.exit, status, err)
		}
	}
}
