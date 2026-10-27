package clusterpreflight

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/availability"
	containerpreflight "github.com/crmarques/bootwright/internal/containercluster/preflight"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/machine"
	storagepreflight "github.com/crmarques/bootwright/internal/storage/preflight"
	"github.com/crmarques/bootwright/internal/substrate/capacity"
)

type graph struct{ catalog api.Catalog }

func (g graph) Effective(context.Context, string) (api.Catalog, error) { return g.catalog, nil }

type containerFake struct {
	calls         int
	request       containerpreflight.PreflightRequest
	report        *Report
	err           error
	waitForCancel bool
	cancelled     bool
}

func (f *containerFake) Check(ctx context.Context, request containerpreflight.PreflightRequest) (*Report, error) {
	f.calls++
	f.request = request
	if f.waitForCancel {
		<-ctx.Done()
		f.cancelled = true
		if f.err == nil {
			return nil, ctx.Err()
		}
	}
	return f.report, f.err
}

type storageFake struct {
	calls         int
	request       storagepreflight.PreflightRequest
	report        *Report
	err           error
	waitForCancel bool
	cancelled     bool
}

func (f *storageFake) Check(ctx context.Context, request storagepreflight.PreflightRequest) (*Report, error) {
	f.calls++
	f.request = request
	if f.waitForCancel {
		<-ctx.Done()
		f.cancelled = true
		if f.err == nil {
			return nil, ctx.Err()
		}
	}
	return f.report, f.err
}

func clusterCatalog() api.Catalog {
	return api.NewCatalog([]api.Object{
		api.NewObject(api.OpenShiftCluster, "sno", api.Value{}, api.Value{}),
		api.NewObject(api.OpenShiftCluster, "edge", api.Value{}, api.Value{}),
		api.NewObject(api.CephCluster, "store", api.Value{}, api.Value{}),
	})
}

func report(target string, checks ...capacity.Check) *Report {
	return &Report{Context: "lab", Target: target, Checks: checks}
}

func check(id, target, status string) capacity.Check {
	return capacity.Check{ID: id, Target: target, Status: status, Reason: id + " " + status}
}

func fixture() (Service, *containerFake, *storageFake) {
	container := &containerFake{report: report("container-cluster", check("resolution.api", "api.sno.test", capacity.Passed))}
	storage := &storageFake{report: report("storage-cluster", check("capacity.memory", "host", capacity.Passed))}
	return Service{Graph: graph{clusterCatalog()}, Selection: func(context.Context) (string, error) { return "lab", nil }, Container: container, Storage: storage}, container, storage
}

func TestClustersRunsBothChecksAndReportsTheirUnion(t *testing.T) {
	service, container, storage := fixture()
	ssh := machine.SSHOptions{User: "operator"}
	result, err := service.Check(context.Background(), PreflightRequest{DryRun: true, TrustOnFirstUse: true, SSH: ssh})
	if err != nil || result == nil {
		t.Fatalf("result %+v, error %v", result, err)
	}
	if result.Context != "lab" || result.Target != "clusters" || len(result.Checks) != 2 || result.Summary.Passed != 2 {
		t.Fatalf("report %+v", result)
	}
	if got := result.Checks[0].Target + " " + result.Checks[1].Target; got != "api.sno.test host" {
		t.Fatalf("checks are not in target order: %s", got)
	}
	if container.calls != 1 || storage.calls != 1 {
		t.Fatalf("calls: container %d, storage %d", container.calls, storage.calls)
	}
	if want := (containerpreflight.PreflightRequest{ContextName: "lab", DryRun: true}); !reflect.DeepEqual(container.request, want) {
		t.Fatalf("the controller checks take no SSH or trust options: %+v", container.request)
	}
	if want := (storagepreflight.PreflightRequest{ContextName: "lab", DryRun: true, TrustOnFirstUse: true, SSH: ssh}); !reflect.DeepEqual(storage.request, want) {
		t.Fatalf("storage request %+v", storage.request)
	}
}

func TestClustersSendsEachNameToTheChecksOfItsKind(t *testing.T) {
	service, container, storage := fixture()
	if _, err := service.Check(context.Background(), PreflightRequest{ContextName: "lab", Clusters: []string{"store", "sno", "sno"}}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(container.request.Clusters, []string{"sno"}) || !reflect.DeepEqual(storage.request.Clusters, []string{"store"}) {
		t.Fatalf("container %v, storage %v", container.request.Clusters, storage.request.Clusters)
	}
}

func TestClustersSkipsTheChecksOfAKindNoNameSelects(t *testing.T) {
	service, container, storage := fixture()
	result, err := service.Check(context.Background(), PreflightRequest{Clusters: []string{"store"}})
	if err != nil || container.calls != 0 || storage.calls != 1 || len(result.Checks) != 1 {
		t.Fatalf("result %+v, error %v, container %d, storage %d", result, err, container.calls, storage.calls)
	}
	service, container, storage = fixture()
	if _, err = service.Check(context.Background(), PreflightRequest{Clusters: []string{"edge"}}); err != nil || container.calls != 1 || storage.calls != 0 {
		t.Fatalf("error %v, container %d, storage %d", err, container.calls, storage.calls)
	}
}

func TestClustersRefusesANameOfNeitherKindBeforeAnyCheck(t *testing.T) {
	service, container, storage := fixture()
	_, err := service.Check(context.Background(), PreflightRequest{Clusters: []string{"sno", "missing"}})
	found := diagnostics.Of(err)
	if len(found) != 1 || found[0].Code != "access.target" || !strings.Contains(found[0].Message, "missing") || found[0].Remediation == "" || container.calls+storage.calls != 0 {
		t.Fatalf("error %#v, calls %d %d", err, container.calls, storage.calls)
	}
}

func TestClustersCombinesTheReadinessOfBothChecks(t *testing.T) {
	for _, row := range []struct {
		name               string
		container, storage string
		code               string
	}{
		{"pending", capacity.Pending, capacity.Passed, "preflight.unknown"},
		{"unknown", capacity.Passed, capacity.Unknown, "preflight.unknown"},
		{"failed", capacity.Failed, capacity.Unknown, "preflight.failed"},
		{"failed storage", capacity.Passed, capacity.Failed, "preflight.failed"},
	} {
		t.Run(row.name, func(t *testing.T) {
			service, container, storage := fixture()
			container.report.Checks[0].Status = row.container
			storage.report.Checks[0].Status = row.storage
			result, err := service.Check(context.Background(), PreflightRequest{})
			found := diagnostics.Of(err)
			if len(found) != 1 || found[0].Code != row.code || found[0].Remediation == "" || result == nil || len(result.Checks) != 2 {
				t.Fatalf("result %+v, error %#v", result, err)
			}
			total := result.Summary.Passed + result.Summary.Failed + result.Summary.Unknown + result.Summary.Pending
			if total != 2 || result.Summary.Passed == 2 {
				t.Fatalf("summary %+v", result.Summary)
			}
		})
	}
}

func TestClustersReportsACheckBothHalvesRunOnceAtItsWorseStatus(t *testing.T) {
	service, container, storage := fixture()
	container.report.Checks = append(container.report.Checks, check("capacity.memory", "host", capacity.Passed))
	storage.report.Checks[0].Status = capacity.Failed
	result, err := service.Check(context.Background(), PreflightRequest{})
	if err == nil || len(result.Checks) != 2 || result.Summary.Failed != 1 || result.Summary.Passed != 1 {
		t.Fatalf("result %+v, error %v", result, err)
	}
	for _, c := range result.Checks {
		if c.ID == "capacity.memory" && c.Status != capacity.Failed {
			t.Fatalf("the shared check keeps %s", c.Status)
		}
	}
}

func TestClustersReturnsAnOperationalErrorWithoutAPartialReport(t *testing.T) {
	boom := errors.New("observation stopped")
	service, container, storage := fixture()
	container.report, container.err = nil, boom
	storage.report.Checks[0].Status = capacity.Failed
	result, err := service.Check(context.Background(), PreflightRequest{})
	if result != nil || !errors.Is(err, boom) {
		t.Fatalf("result %+v, error %v", result, err)
	}
}

func TestClustersIsUnavailableWithoutItsCollaborators(t *testing.T) {
	service, _, _ := fixture()
	service.Storage = nil
	if _, err := service.Check(context.Background(), PreflightRequest{}); !errors.Is(err, availability.ErrNotImplemented) {
		t.Fatalf("error %v", err)
	}
}

func TestClustersStopsOnACancelledContext(t *testing.T) {
	service, container, storage := fixture()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := service.Check(ctx, PreflightRequest{}); !errors.Is(err, context.Canceled) || container.calls+storage.calls != 0 {
		t.Fatalf("error %v", err)
	}
}

func TestClustersRefusesSSHOverridesWhenNoStorageClusterIsSelected(t *testing.T) {
	ssh := machine.SSHOptions{User: "operator"}
	service, container, storage := fixture()
	_, err := service.Check(context.Background(), PreflightRequest{Clusters: []string{"sno"}, SSH: ssh, TrustOnFirstUse: true})
	found := diagnostics.Of(err)
	if len(found) != 1 || found[0].Code != "cli.usage" || found[0].Remediation == "" || container.calls+storage.calls != 0 {
		t.Fatalf("error %#v, calls %d %d", err, container.calls, storage.calls)
	}
	service, container, storage = fixture()
	service.Graph = graph{api.NewCatalog([]api.Object{api.NewObject(api.OpenShiftCluster, "sno", api.Value{}, api.Value{})})}
	if _, err = service.Check(context.Background(), PreflightRequest{SSH: ssh}); len(diagnostics.Of(err)) != 1 || container.calls+storage.calls != 0 {
		t.Fatalf("a context with no storage cluster accepted SSH overrides: %v", err)
	}
	service, container, storage = fixture()
	if _, err = service.Check(context.Background(), PreflightRequest{Clusters: []string{"sno"}, TrustOnFirstUse: true}); err != nil || container.calls != 1 {
		t.Fatalf("the default trust option alone must not refuse: %v", err)
	}
	service, _, storage = fixture()
	if _, err = service.Check(context.Background(), PreflightRequest{Clusters: []string{"sno", "store"}, SSH: ssh}); err != nil || storage.calls != 1 || storage.request.SSH != ssh {
		t.Fatalf("a selected storage cluster takes the overrides: %v", err)
	}
}

func TestClustersStopsTheOtherChecksWhenOneErrorsWithoutAReport(t *testing.T) {
	boom := errors.New("storage observation stopped")
	service, container, storage := fixture()
	container.waitForCancel = true
	storage.report, storage.err = nil, boom
	result, err := service.Check(context.Background(), PreflightRequest{})
	if result != nil || !errors.Is(err, boom) || !container.cancelled {
		t.Fatalf("result %+v, error %v, container cancelled %t", result, err, container.cancelled)
	}
	stopped := errors.New("container observation stopped")
	service, container, storage = fixture()
	storage.waitForCancel = true
	container.report, container.err = nil, stopped
	result, err = service.Check(context.Background(), PreflightRequest{})
	if result != nil || !errors.Is(err, stopped) || !storage.cancelled {
		t.Fatalf("result %+v, error %v, storage cancelled %t", result, err, storage.cancelled)
	}
}

func TestClustersReturnsTheContainerErrorWhenBothFail(t *testing.T) {
	fromContainer, fromStorage := errors.New("container side"), errors.New("storage side")
	service, container, storage := fixture()
	container.report, container.err = nil, fromContainer
	storage.report, storage.err = nil, fromStorage
	if _, err := service.Check(context.Background(), PreflightRequest{}); !errors.Is(err, fromContainer) {
		t.Fatalf("error %v", err)
	}
	service, container, storage = fixture()
	container.report, container.err, container.waitForCancel = nil, fromContainer, true
	storage.report, storage.err = nil, fromStorage
	if _, err := service.Check(context.Background(), PreflightRequest{}); !errors.Is(err, fromContainer) {
		t.Fatalf("the container error must win even when the storage side fails first: %v", err)
	}
}

func TestClustersKeepsTheContainerCheckAtEqualSeverityAndMergesOnTargetAndIDAlone(t *testing.T) {
	service, container, storage := fixture()
	container.report.Checks = []capacity.Check{
		{ID: "capacity.memory", Target: "host", Status: capacity.Unknown, Reason: "from container"},
		{ID: "capacity.cpu", Target: "host", Status: capacity.Passed, Reason: "cpu"},
	}
	storage.report.Checks = []capacity.Check{
		{ID: "capacity.memory", Target: "host", Status: capacity.Unknown, Reason: "from storage"},
		{ID: "capacity.memory", Target: "other", Status: capacity.Passed, Reason: "other host"},
		{ID: "capacity.disk", Target: "host", Status: capacity.Passed, Reason: "disk"},
	}
	result, _ := service.Check(context.Background(), PreflightRequest{})
	if len(result.Checks) != 4 {
		t.Fatalf("a check differing in target or id was merged: %+v", result.Checks)
	}
	for _, check := range result.Checks {
		if check.ID == "capacity.memory" && check.Target == "host" && check.Reason != "from container" {
			t.Fatalf("the storage check displaced the container check at equal severity: %+v", check)
		}
	}
	for _, order := range []struct{ container, storage string }{{capacity.Pending, capacity.Unknown}, {capacity.Unknown, capacity.Failed}, {capacity.Passed, capacity.Pending}} {
		service, container, storage = fixture()
		container.report.Checks = []capacity.Check{{ID: "x", Target: "t", Status: order.container, Reason: "container"}}
		storage.report.Checks = []capacity.Check{{ID: "x", Target: "t", Status: order.storage, Reason: "storage"}}
		result, _ = service.Check(context.Background(), PreflightRequest{})
		if len(result.Checks) != 1 || result.Checks[0].Reason != "storage" {
			t.Fatalf("%s then %s: %+v", order.container, order.storage, result.Checks)
		}
	}
}
