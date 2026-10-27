package preflight

import (
	"context"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/substrate/capacity"
)

type graphStub struct{ catalog api.Catalog }

func (g graphStub) Effective(context.Context, string) (api.Catalog, error) { return g.catalog, nil }

type ownedStub map[string]machine.OwnershipState

func (o ownedStub) Ownership(context.Context, string) (map[string]machine.OwnershipState, error) {
	return o, nil
}

type guestFunc func(context.Context, string, api.Object, PreflightRequest) ([]Check, error)

func (f guestFunc) CheckGuest(ctx context.Context, name string, object api.Object, request PreflightRequest) ([]Check, error) {
	return f(ctx, name, object, request)
}
func testService() Service {
	node := api.MapValue(api.FieldValue{Name: "machineRef", Value: api.StringValue("guest")})
	cluster := api.NewObject(api.StorageCluster, "storage", api.Value{}, api.MapValue(api.FieldValue{Name: "ceph", Value: api.MapValue(api.FieldValue{Name: "topology", Value: api.MapValue(api.FieldValue{Name: "nodes", Value: api.ListValue(node)})})}))
	host := api.NewObject(api.Machine, "guest", api.Value{}, api.MapValue())
	return Service{Graph: graphStub{api.NewCatalog([]api.Object{cluster, host})}, Ownership: ownedStub{}, Project: func(api.Catalog, string) ([]Allocation, error) {
		return []Allocation{{Machine: "guest", Host: "controller", Pool: "/pool", Quantities: Quantities{CPU: 1, Memory: 1, Disk: 1}}}, nil
	}, Capacity: fixtureBudget()}
}
func TestUninstalledGuestIsPendingWithoutProbe(t *testing.T) {
	for _, test := range []struct {
		name      string
		ownership ownedStub
	}{
		{"unrecorded", ownedStub{}},
		{"apply pending", ownedStub{"Machine/guest": {Verb: machine.VerbApply, State: machine.BlockPending}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			s := testService()
			s.Ownership = test.ownership
			s.Guests = guestFunc(func(context.Context, string, api.Object, PreflightRequest) ([]Check, error) {
				t.Fatal("probed a future host")
				return nil, nil
			})
			report, err := s.Check(context.Background(), PreflightRequest{ContextName: "synthetic"})
			if err == nil || report == nil || report.Summary != (Summary{Pending: 1, Passed: 3}) {
				t.Fatalf("report=%+v err=%v", report, err)
			}
		})
	}
}
func TestUnprovedGuestRemainsUnknownWithoutProbe(t *testing.T) {
	for _, test := range []struct {
		name      string
		ownership Ownership
	}{
		{"ownership unavailable", nil},
		{"empty record", ownedStub{"Machine/guest": {}}},
		{"apply running", ownedStub{"Machine/guest": {Verb: machine.VerbApply, State: machine.BlockRunning}}},
		{"apply failed", ownedStub{"Machine/guest": {Verb: machine.VerbApply, State: machine.BlockFailed}}},
		{"apply unknown", ownedStub{"Machine/guest": {Verb: machine.VerbApply, State: machine.BlockUnknown}}},
		{"apply partial", ownedStub{"Machine/guest": {Verb: machine.VerbApply, State: "partial"}}},
		{"apply unrecognized", ownedStub{"Machine/guest": {Verb: machine.VerbApply, State: "unrecognized"}}},
		{"unrecognized verb", ownedStub{"Machine/guest": {Verb: "unrecognized", State: machine.BlockPending}}},
		{"destroy pending", ownedStub{"Machine/guest": {Verb: machine.VerbDestroy, State: machine.BlockPending}}},
		{"destroy running", ownedStub{"Machine/guest": {Verb: machine.VerbDestroy, State: machine.BlockRunning}}},
		{"destroy failed", ownedStub{"Machine/guest": {Verb: machine.VerbDestroy, State: machine.BlockFailed}}},
		{"destroy unknown", ownedStub{"Machine/guest": {Verb: machine.VerbDestroy, State: machine.BlockUnknown}}},
		{"destroy done", ownedStub{"Machine/guest": {Verb: machine.VerbDestroy, State: machine.BlockDone}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			s := testService()
			s.Ownership = test.ownership
			s.Guests = guestFunc(func(context.Context, string, api.Object, PreflightRequest) ([]Check, error) {
				t.Fatal("probed a Machine without proved installation")
				return nil, nil
			})
			report, err := s.Check(context.Background(), PreflightRequest{ContextName: "synthetic"})
			if err == nil || report == nil || report.Summary != (Summary{Unknown: 1, Passed: 3}) {
				t.Fatalf("report=%+v err=%v", report, err)
			}
		})
	}
}
func TestProvidedGuestWithPendingOwnershipUsesActualChecks(t *testing.T) {
	s := testService()
	s.Ownership = ownedStub{"Machine/guest": {Verb: machine.VerbApply, State: machine.BlockPending}}
	cluster, _ := s.Graph.(graphStub).catalog.Find(api.StorageCluster, "storage")
	host := api.NewObject(api.Machine, "guest", api.Value{}, api.MapValue(api.FieldValue{Name: "os", Value: api.MapValue(api.FieldValue{Name: "provided", Value: api.BoolValue(true)})}))
	s.Graph = graphStub{api.NewCatalog([]api.Object{cluster, host})}
	called := false
	s.Guests = guestFunc(func(context.Context, string, api.Object, PreflightRequest) ([]Check, error) {
		called = true
		return []Check{{ID: "host.prerequisites", Target: "guest", Status: capacity.Failed, Reason: "a native prerequisite is missing"}}, nil
	})
	report, err := s.Check(context.Background(), PreflightRequest{ContextName: "synthetic"})
	if !called || err == nil || report == nil || report.Summary != (Summary{Failed: 1, Passed: 3}) {
		t.Fatalf("called=%t report=%+v err=%v", called, report, err)
	}
}
func TestDryRunNeverObservesProviderOrInstalledGuest(t *testing.T) {
	s := testService()
	s.Ownership = ownedStub{"Machine/guest": {Verb: machine.VerbApply, State: machine.BlockDone}}
	budget := fixtureBudget()
	budget.Observer = observeFunc(func(context.Context, Target) (Facts, error) {
		t.Fatal("dry-run observed provider")
		return Facts{}, nil
	})
	s.Capacity = budget
	s.Guests = guestFunc(func(context.Context, string, api.Object, PreflightRequest) ([]Check, error) {
		t.Fatal("dry-run observed guest")
		return nil, nil
	})
	report, err := s.Check(context.Background(), PreflightRequest{ContextName: "synthetic", DryRun: true})
	if err == nil || report.Summary.Unknown != 4 {
		t.Fatalf("report=%+v err=%v", report, err)
	}
}
func TestUnknownClusterIsRefusedBeforeObservations(t *testing.T) {
	s := testService()
	s.Project = func(api.Catalog, string) ([]Allocation, error) { t.Fatal("selected unknown cluster"); return nil, nil }
	if report, err := s.Check(context.Background(), PreflightRequest{ContextName: "synthetic", Clusters: []string{"absent"}}); err == nil || report != nil {
		t.Fatalf("%+v %v", report, err)
	}
}
