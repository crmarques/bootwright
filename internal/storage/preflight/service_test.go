package preflight

import (
	"context"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/machine"
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
		return []Allocation{{Machine: "guest", Host: "controller", Pool: "/pool", Quantities: Quantities{1, 1, 1}}}, nil
	}, Capacity: fixtureBudget()}
}
func TestUninstalledGuestIsPendingWithoutProbe(t *testing.T) {
	s := testService()
	s.Guests = guestFunc(func(context.Context, string, api.Object, PreflightRequest) ([]Check, error) {
		t.Fatal("probed a future host")
		return nil, nil
	})
	report, err := s.Check(context.Background(), PreflightRequest{ContextName: "synthetic"})
	if err == nil || report.Summary.Pending != 1 || report.Summary.Passed != 3 {
		t.Fatalf("report=%+v err=%v", report, err)
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
