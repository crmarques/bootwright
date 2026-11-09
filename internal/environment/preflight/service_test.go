package preflight

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/availability"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/environment/clusterpreflight"
	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/substrate/capacity"
)

type graph struct{ catalog api.Catalog }

func (g graph) Effective(context.Context, string) (api.Catalog, error) { return g.catalog, nil }

type controllerFake struct {
	calls         int
	request       prerequisites.CheckRequest
	report        *prerequisites.Report
	err           error
	waitForCancel bool
	cancelled     bool
}

func (f *controllerFake) Check(ctx context.Context, request prerequisites.CheckRequest) (*prerequisites.Report, error) {
	f.calls++
	f.request = request
	if f.waitForCancel {
		select {
		case <-ctx.Done():
		case <-time.After(5 * time.Second):
			return nil, errors.New("the check was never cancelled")
		}
		f.cancelled = true
		if f.err == nil {
			return nil, ctx.Err()
		}
	}
	return f.report, f.err
}

type capacityFake struct {
	calls         int
	dryRun        bool
	checks        []capacity.Check
	err           error
	waitForCancel bool
	cancelled     bool
}

func (f *capacityFake) CheckCapacity(ctx context.Context, _ api.Catalog, _ string, dryRun bool) ([]capacity.Check, error) {
	f.calls++
	f.dryRun = dryRun
	if f.waitForCancel {
		select {
		case <-ctx.Done():
		case <-time.After(5 * time.Second):
			return nil, errors.New("the check was never cancelled")
		}
		f.cancelled = true
		if f.err == nil {
			return nil, ctx.Err()
		}
	}
	return append([]capacity.Check(nil), f.checks...), f.err
}

type clustersFake struct {
	calls         int
	request       clusterpreflight.PreflightRequest
	report        *Report
	err           error
	waitForCancel bool
	cancelled     bool
}

func (f *clustersFake) Check(ctx context.Context, request clusterpreflight.PreflightRequest) (*Report, error) {
	f.calls++
	f.request = request
	if f.waitForCancel {
		select {
		case <-ctx.Done():
		case <-time.After(5 * time.Second):
			return nil, errors.New("the check was never cancelled")
		}
		f.cancelled = true
		if f.err == nil {
			return nil, ctx.Err()
		}
	}
	return f.report, f.err
}

func object(kind api.Kind, name string, fields ...api.FieldValue) api.Object {
	return api.NewObject(kind, name, api.Value{}, api.MapValue(fields...))
}

func field(name, value string) api.FieldValue {
	return api.FieldValue{Name: name, Value: api.StringValue(value)}
}

func infrastructureCatalog() api.Catalog {
	return api.NewCatalog([]api.Object{
		object(api.DNSServer, "local-dns", field("management", "managed"), field("machineRef", "controller")),
		object(api.NTPServer, "remote-ntp", field("management", "managed"), field("machineRef", "edge")),
		object(api.Proxy, "outside", field("management", "external")),
		object(api.Registry, "mirror", field("management", "managed"), field("machineRef", "controller")),
		object(api.CephCluster, "store"),
	})
}

func ready(id, required, observed string) prerequisites.Check {
	return prerequisites.Check{ID: id, Required: required, Observed: observed, Status: "ready", Scope: prerequisites.HostScope}
}

func fixture() (Service, *controllerFake, *capacityFake, *clustersFake) {
	controller := &controllerFake{report: &prerequisites.Report{Checks: []prerequisites.Check{ready("host-packages", "podman", "podman")}}}
	budget := &capacityFake{checks: []capacity.Check{{ID: "capacity.memory", Target: "host", Status: capacity.Passed, Reason: "memory fits"}}}
	clusters := &clustersFake{report: &Report{Context: "lab", Target: "clusters", Checks: []capacity.Check{{ID: "resolution.api", Target: "api.sno.test", Status: capacity.Passed, Reason: "resolves"}}}}
	service := Service{
		Graph: graph{infrastructureCatalog()}, Selection: func(context.Context) (string, error) { return "lab", nil },
		Controller: controller, Capacity: budget, Clusters: clusters,
	}
	return service, controller, budget, clusters
}

func targets(checks []capacity.Check) []string {
	found := make([]string, 0, len(checks))
	for _, check := range checks {
		found = append(found, check.Target+" "+check.ID+" "+check.Status)
	}
	return found
}

var infrastructure = []string{"substrates", "controller", "infrastructure-services"}

func TestTheControllerAndSubstrateStagesReportTheControllerAndCapacityChecksAndNothingTheyDoNotObserve(t *testing.T) {
	service, controller, budget, clusters := fixture()
	controller.report = &prerequisites.Report{Next: "bootwright controller install", Checks: []prerequisites.Check{
		ready("host-packages", "podman", "podman 5"),
		{ID: "controller-binding", Required: "this host", Observed: "another host", Status: "not-ready", Scope: prerequisites.ContextScope},
		{ID: "fips-mode", Required: "the host's FIPS mode", Observed: "unverified", Status: "unverified", Scope: prerequisites.HostScope},
		{ID: "listener-firewall", Required: "DNSServer/local-dns inbound socket", Observed: "closed", Status: "not-ready", Scope: prerequisites.ContextScope, Target: "DNSServer/local-dns 192.0.2.53:53/udp"},
		{ID: "listener-firewall", Required: "DNSServer/local-dns inbound socket", Observed: "open", Status: "ready", Scope: prerequisites.ContextScope, Target: "DNSServer/local-dns 192.0.2.53:53/tcp"},
	}}
	controller.err = diagnostics.NewFailure("preflight.failed", "required controller prerequisites are not ready", "")
	result, err := service.Preflight(context.Background(), PreflightRequest{Stages: infrastructure})
	found := diagnostics.Of(err)
	if result == nil || len(found) != 1 || found[0].Code != "preflight.failed" || found[0].Remediation != "resolve the reported prerequisites and repeat bootwright preflight --context lab --stage controller,infrastructure-services,substrates" {
		t.Fatalf("result %+v, error %#v", result, err)
	}
	want := []string{
		"DNSServer/local-dns 192.0.2.53:53/tcp controller.listener-firewall passed",
		"DNSServer/local-dns 192.0.2.53:53/udp controller.listener-firewall failed",
		"controller controller.controller-binding failed",
		"controller controller.fips-mode unknown",
		"controller controller.host-packages passed",
		"host capacity.memory passed",
	}
	if got := targets(result.Checks); !reflect.DeepEqual(got, sortedLike(want)) {
		t.Fatalf("checks\n%q\nwant\n%q", got, sortedLike(want))
	}
	if result.Context != "lab" || result.Target != "controller,infrastructure-services,substrates" || result.Summary.Passed != 3 || result.Summary.Failed != 2 || result.Summary.Unknown != 1 {
		t.Fatalf("report %+v", result)
	}
	if controller.request.ContextName != "lab" || budget.dryRun || clusters.calls != 0 {
		t.Fatalf("controller %+v, dry-run %t, cluster calls %d", controller.request, budget.dryRun, clusters.calls)
	}
	for _, check := range result.Checks {
		if check.Status != capacity.Passed && check.Next != "run bootwright controller install" {
			t.Fatalf("an unready controller check does not name the command that settles it: %+v", check)
		}
	}
}

func TestTheListenerRowsAreTheInfrastructureServicesStageAndTheOthersTheControllerStage(t *testing.T) {
	rows := []prerequisites.Check{
		ready("host-packages", "podman", "podman 5"),
		{ID: "listener-firewall", Required: "DNSServer/local-dns inbound socket", Observed: "open", Status: "ready", Scope: prerequisites.ContextScope, Target: "DNSServer/local-dns 192.0.2.53:53/udp"},
	}
	for _, tt := range []struct {
		stage string
		want  []string
	}{
		{"controller", []string{"controller controller.host-packages passed"}},
		{"infrastructure-services", []string{"DNSServer/local-dns 192.0.2.53:53/udp controller.listener-firewall passed"}},
	} {
		service, controller, budget, clusters := fixture()
		controller.report = &prerequisites.Report{Checks: rows}
		result, err := service.Preflight(context.Background(), PreflightRequest{Stages: []string{tt.stage}})
		if err != nil || !reflect.DeepEqual(targets(result.Checks), tt.want) || result.Target != tt.stage || budget.calls+clusters.calls != 0 {
			t.Fatalf("%s: checks %q, error %v, calls %d %d", tt.stage, targets(result.Checks), err, budget.calls, clusters.calls)
		}
	}
}

func TestAnUnknownStageRefusesBeforeAnyCheck(t *testing.T) {
	service, controller, budget, clusters := fixture()
	_, err := service.Preflight(context.Background(), PreflightRequest{Stages: []string{"network"}})
	if found := diagnostics.Of(err); len(found) != 1 || found[0].Code != "cli.usage" || controller.calls+budget.calls+clusters.calls != 0 {
		t.Fatalf("error %#v", err)
	}
}

func TestAStageWithoutChecksRunsNothingAndPasses(t *testing.T) {
	service, controller, budget, clusters := fixture()
	result, err := service.Preflight(context.Background(), PreflightRequest{Stages: []string{"machines"}})
	if err != nil || result == nil || len(result.Checks) != 0 || result.Target != "machines" || controller.calls+budget.calls+clusters.calls != 0 {
		t.Fatalf("result %+v, error %v", result, err)
	}
}

func TestTheAddOnsStageIsUnavailableAndRunsNothing(t *testing.T) {
	service, controller, budget, clusters := fixture()
	_, err := service.Preflight(context.Background(), PreflightRequest{Stages: []string{"controller", "add-ons"}})
	if !errors.Is(err, availability.ErrNotImplemented) || controller.calls+budget.calls+clusters.calls != 0 {
		t.Fatalf("error %v", err)
	}
}

func TestAClusterSelectionWithoutTheClustersStageRefusesBeforeAnyCheck(t *testing.T) {
	service, controller, budget, clusters := fixture()
	_, err := service.Preflight(context.Background(), PreflightRequest{Stages: []string{"controller"}, Clusters: []string{"store"}})
	found := diagnostics.Of(err)
	if len(found) != 1 || found[0].Code != "cli.usage" || found[0].Remediation == "" || controller.calls+budget.calls+clusters.calls != 0 {
		t.Fatalf("error %#v", err)
	}
	if _, err = service.Preflight(context.Background(), PreflightRequest{Stages: []string{"clusters"}, Clusters: []string{"store"}}); err != nil || !reflect.DeepEqual(clusters.request.Clusters, []string{"store"}) {
		t.Fatalf("the clusters stage takes the selection: %v, %+v", err, clusters.request)
	}
}

func TestTheInfrastructureStagesKeepTwoSocketsOfOneObjectAsTwoChecks(t *testing.T) {
	service, controller, _, _ := fixture()
	row := func(target string) prerequisites.Check {
		return prerequisites.Check{ID: "listener-firewall", Required: "DNSServer/dns inbound socket", Observed: "open", Status: "ready", Scope: prerequisites.ContextScope, Target: target}
	}
	controller.report = &prerequisites.Report{Checks: []prerequisites.Check{row("DNSServer/dns 192.0.2.53:53/tcp"), row("DNSServer/dns 192.0.2.53:53/udp")}}
	result, err := service.Preflight(context.Background(), PreflightRequest{Stages: infrastructure})
	listeners := 0
	for _, check := range result.Checks {
		if check.ID == "controller.listener-firewall" {
			listeners++
		}
	}
	if err != nil || listeners != 2 {
		t.Fatalf("listener checks %d, error %v", listeners, err)
	}
}

func sortedLike(want []string) []string {
	checks := make([]capacity.Check, 0, len(want))
	for _, line := range want {
		parts := strings.Split(line, " ")
		last := len(parts) - 1
		checks = append(checks, capacity.Check{Target: strings.Join(parts[:last-1], " "), ID: parts[last-1], Status: parts[last]})
	}
	sortChecks(checks)
	return targets(checks)
}

func sortChecks(checks []capacity.Check) {
	report, _ := clusterpreflight.Assemble("", "", "", &Report{Checks: checks})
	copy(checks, report.Checks)
}

func TestADryRunOfTheInfrastructureStagesRunsNoControllerCheckAndKeepsEveryCheckPending(t *testing.T) {
	service, controller, budget, _ := fixture()
	result, err := service.Preflight(context.Background(), PreflightRequest{Stages: infrastructure, DryRun: true})
	found := diagnostics.Of(err)
	if result == nil || len(found) != 1 || found[0].Code != "preflight.unknown" || controller.calls != 0 || !budget.dryRun {
		t.Fatalf("result %+v, error %#v, controller calls %d", result, err, controller.calls)
	}
	for _, check := range result.Checks {
		if check.Status != capacity.Pending {
			t.Fatalf("dry-run check %+v", check)
		}
	}
	if result.Summary.Pending != 3 || len(result.Checks) != 3 {
		t.Fatalf("summary %+v", result.Summary)
	}
}

func TestTheInfrastructureStagesAreReadyWhenEveryCheckPasses(t *testing.T) {
	service, _, _, _ := fixture()
	result, err := service.Preflight(context.Background(), PreflightRequest{Stages: infrastructure})
	if err != nil || result.Summary.Passed != 2 || len(result.Checks) != 2 {
		t.Fatalf("result %+v, error %v", result, err)
	}
}

func TestPreflightReturnsAControllerErrorItsRowsDoNotExplain(t *testing.T) {
	boom := errors.New("listener inspection stopped")
	service, controller, _, _ := fixture()
	controller.err = boom
	result, err := service.Preflight(context.Background(), PreflightRequest{Stages: infrastructure})
	if result != nil || !errors.Is(err, boom) {
		t.Fatalf("result %+v, error %v", result, err)
	}
	service, controller, _, _ = fixture()
	controller.report, controller.err = nil, boom
	if result, err = service.Preflight(context.Background(), PreflightRequest{Stages: infrastructure}); result != nil || !errors.Is(err, boom) {
		t.Fatalf("result %+v, error %v", result, err)
	}
}

func TestTheInfrastructureStagesStopTheOtherChecksWhenOneErrorsWithoutAReport(t *testing.T) {
	boom := errors.New("capacity read stopped")
	service, controller, budget, _ := fixture()
	controller.waitForCancel = true
	budget.err = boom
	result, err := service.Preflight(context.Background(), PreflightRequest{Stages: infrastructure})
	if result != nil || !errors.Is(err, boom) || !controller.cancelled {
		t.Fatalf("result %+v, error %v, controller cancelled %t", result, err, controller.cancelled)
	}
	stopped := errors.New("controller read stopped")
	service, controller, budget, _ = fixture()
	budget.waitForCancel = true
	controller.report, controller.err = nil, stopped
	result, err = service.Preflight(context.Background(), PreflightRequest{Stages: infrastructure})
	if result != nil || !errors.Is(err, stopped) || !budget.cancelled {
		t.Fatalf("result %+v, error %v, capacity cancelled %t", result, err, budget.cancelled)
	}
	fromController, fromCapacity := errors.New("controller side"), errors.New("capacity side")
	service, controller, budget, _ = fixture()
	controller.report, controller.err = nil, fromController
	budget.err = fromCapacity
	if _, err = service.Preflight(context.Background(), PreflightRequest{Stages: infrastructure}); !errors.Is(err, fromController) {
		t.Fatalf("the controller error must win when both fail: %v", err)
	}
}

func TestPreflightIsUnavailableWithoutItsCollaboratorsAndStopsOnACancelledContext(t *testing.T) {
	for _, remove := range []func(*Service){
		func(s *Service) { s.Capacity = nil },
		func(s *Service) { s.Clusters = nil },
	} {
		service, _, _, _ := fixture()
		remove(&service)
		if _, err := service.Preflight(context.Background(), PreflightRequest{}); !errors.Is(err, availability.ErrNotImplemented) {
			t.Fatalf("error %v", err)
		}
	}
	service, controller, budget, clusters := fixture()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := service.Preflight(ctx, PreflightRequest{}); !errors.Is(err, context.Canceled) || controller.calls+budget.calls+clusters.calls != 0 {
		t.Fatalf("error %v", err)
	}
}

func TestEveryStageMergesInfrastructureAndClusterChecks(t *testing.T) {
	service, controller, budget, clusters := fixture()
	ssh := machine.SSHOptions{User: "operator"}
	clusters.report.Checks = append(clusters.report.Checks, capacity.Check{ID: "capacity.memory", Target: "host", Status: capacity.Failed, Reason: "from clusters"})
	result, err := service.Preflight(context.Background(), PreflightRequest{TrustOnFirstUse: true, SSH: ssh})
	found := diagnostics.Of(err)
	if result == nil || len(found) != 1 || found[0].Code != "preflight.failed" || found[0].Remediation != "resolve the reported prerequisites and repeat bootwright preflight --context lab" {
		t.Fatalf("result %+v, error %#v", result, err)
	}
	if result.Context != "lab" || result.Target != "controller,infrastructure-services,substrates,machines,clusters" {
		t.Fatalf("report %+v", result)
	}
	if want := (clusterpreflight.PreflightRequest{ContextName: "lab", TrustOnFirstUse: true, SSH: ssh}); !reflect.DeepEqual(clusters.request, want) {
		t.Fatalf("clusters request %+v", clusters.request)
	}
	if controller.request.ContextName != "lab" || controller.calls != 1 || budget.calls != 1 || clusters.calls != 1 {
		t.Fatalf("calls: controller %d, capacity %d, clusters %d", controller.calls, budget.calls, clusters.calls)
	}
	var memory []capacity.Check
	for _, check := range result.Checks {
		if check.ID == "capacity.memory" {
			memory = append(memory, check)
		}
	}
	if len(memory) != 1 || memory[0].Status != capacity.Failed || result.Summary.Failed != 1 || result.Summary.Passed != 2 || result.Summary.Unknown != 0 {
		t.Fatalf("the shared check must appear once at its worse status: %+v, summary %+v", memory, result.Summary)
	}
}

func TestEveryStageKeepsTheInfrastructureCheckAtEqualSeverity(t *testing.T) {
	service, _, _, clusters := fixture()
	clusters.report.Checks = []capacity.Check{{ID: "capacity.memory", Target: "host", Status: capacity.Passed, Reason: "from clusters"}}
	result, _ := service.Preflight(context.Background(), PreflightRequest{})
	for _, check := range result.Checks {
		if check.ID == "capacity.memory" && check.Reason != "memory fits" {
			t.Fatalf("the cluster check displaced the infrastructure check at equal severity: %+v", check)
		}
	}
}

func TestEveryStageReportsAnInfrastructureFailureBesideTheClusterChecks(t *testing.T) {
	service, controller, _, clusters := fixture()
	controller.report.Checks[0].Status = "not-ready"
	controller.err = diagnostics.NewFailure("preflight.failed", "not ready", "")
	result, err := service.Preflight(context.Background(), PreflightRequest{})
	found := diagnostics.Of(err)
	if result == nil || len(found) != 1 || found[0].Code != "preflight.failed" || clusters.calls != 1 {
		t.Fatalf("result %+v, error %#v", result, err)
	}
	seen := map[string]bool{}
	for _, check := range result.Checks {
		seen[check.ID] = true
	}
	if !seen["controller.host-packages"] || !seen["resolution.api"] || !seen["capacity.memory"] {
		t.Fatalf("checks %q", targets(result.Checks))
	}
}

func TestEveryStageStopsTheOtherChecksWhenOneErrorsWithoutAReport(t *testing.T) {
	boom := errors.New("cluster observation stopped")
	service, controller, budget, clusters := fixture()
	controller.waitForCancel, budget.waitForCancel = true, true
	clusters.report, clusters.err = nil, boom
	result, err := service.Preflight(context.Background(), PreflightRequest{})
	if result != nil || !errors.Is(err, boom) || !controller.cancelled || !budget.cancelled {
		t.Fatalf("result %+v, error %v, cancelled %t %t", result, err, controller.cancelled, budget.cancelled)
	}
	stopped := errors.New("capacity read stopped")
	service, _, budget, clusters = fixture()
	clusters.waitForCancel = true
	budget.err = stopped
	result, err = service.Preflight(context.Background(), PreflightRequest{})
	if result != nil || !errors.Is(err, stopped) || !clusters.cancelled {
		t.Fatalf("result %+v, error %v, clusters cancelled %t", result, err, clusters.cancelled)
	}
	fromInfrastructure, fromClusters := errors.New("infrastructure side"), errors.New("clusters side")
	service, _, budget, clusters = fixture()
	budget.err = fromInfrastructure
	clusters.report, clusters.err = nil, fromClusters
	if _, err = service.Preflight(context.Background(), PreflightRequest{}); !errors.Is(err, fromInfrastructure) {
		t.Fatalf("the infrastructure error must win when both fail: %v", err)
	}
}

func TestADryRunOfEveryStageRunsNoControllerCheck(t *testing.T) {
	service, controller, budget, clusters := fixture()
	result, err := service.Preflight(context.Background(), PreflightRequest{DryRun: true})
	if result == nil || err == nil || controller.calls != 0 || !budget.dryRun || !clusters.request.DryRun {
		t.Fatalf("result %+v, error %v, controller calls %d", result, err, controller.calls)
	}
}

func TestPreflightRefusesSSHOverridesNoCephClusterReachesBeforeAnyCheckRuns(t *testing.T) {
	ssh := machine.SSHOptions{User: "operator"}
	service, controller, budget, clusters := fixture()
	service.Graph = graph{api.NewCatalog([]api.Object{object(api.DNSServer, "local-dns", field("management", "managed"))})}
	result, err := service.Preflight(context.Background(), PreflightRequest{SSH: ssh})
	found := diagnostics.Of(err)
	if result != nil || len(found) != 1 || found[0].Code != "cli.usage" || found[0].Remediation == "" || controller.calls+budget.calls+clusters.calls != 0 {
		t.Fatalf("result %+v, error %#v, calls %d %d %d", result, err, controller.calls, budget.calls, clusters.calls)
	}
	service, controller, budget, clusters = fixture()
	if _, err = service.Preflight(context.Background(), PreflightRequest{SSH: ssh, Stages: infrastructure}); len(diagnostics.Of(err)) != 1 || controller.calls+budget.calls+clusters.calls != 0 {
		t.Fatalf("without the clusters stage no CephCluster takes the overrides: %v", err)
	}
	service, _, _, clusters = fixture()
	if _, err = service.Preflight(context.Background(), PreflightRequest{SSH: ssh}); err != nil || clusters.request.SSH != ssh {
		t.Fatalf("a declared CephCluster takes the overrides: %v", err)
	}
}
