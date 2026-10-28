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

func TestInfrastructureReportsTheControllerAndCapacityChecksAndNothingItDoesNotObserve(t *testing.T) {
	service, controller, budget, _ := fixture()
	controller.report = &prerequisites.Report{Next: "bootwright setup", Checks: []prerequisites.Check{
		ready("host-packages", "podman", "podman 5"),
		{ID: "controller-binding", Required: "this host", Observed: "another host", Status: "not-ready", Scope: prerequisites.ContextScope},
		{ID: "fips-mode", Required: "the host's FIPS mode", Observed: "unverified", Status: "unverified", Scope: prerequisites.HostScope},
		{ID: "listener-firewall", Required: "DNSServer/local-dns inbound socket", Observed: "closed", Status: "not-ready", Scope: prerequisites.ContextScope, Target: "DNSServer/local-dns 192.0.2.53:53/udp"},
		{ID: "listener-firewall", Required: "DNSServer/local-dns inbound socket", Observed: "open", Status: "ready", Scope: prerequisites.ContextScope, Target: "DNSServer/local-dns 192.0.2.53:53/tcp"},
	}}
	controller.err = diagnostics.NewFailure("preflight.failed", "required controller prerequisites are not ready", "")
	result, err := service.PreflightInfrastructure(context.Background(), InfrastructurePreflightRequest{})
	found := diagnostics.Of(err)
	if result == nil || len(found) != 1 || found[0].Code != "preflight.failed" || !strings.Contains(found[0].Remediation, "preflight infra") {
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
	if result.Context != "lab" || result.Target != "infra" || result.Summary.Passed != 3 || result.Summary.Failed != 2 || result.Summary.Unknown != 1 {
		t.Fatalf("report %+v", result)
	}
	if controller.request.ContextName != "lab" || budget.dryRun {
		t.Fatalf("controller %+v, dry-run %t", controller.request, budget.dryRun)
	}
	for _, check := range result.Checks {
		if check.Status != capacity.Passed && check.Next != "run bootwright setup" {
			t.Fatalf("an unready controller check does not name the command that settles it: %+v", check)
		}
	}
}

func TestInfrastructureKeepsTwoSocketsOfOneObjectAsTwoChecks(t *testing.T) {
	service, controller, _, _ := fixture()
	row := func(target string) prerequisites.Check {
		return prerequisites.Check{ID: "listener-firewall", Required: "DNSServer/dns inbound socket", Observed: "open", Status: "ready", Scope: prerequisites.ContextScope, Target: target}
	}
	controller.report = &prerequisites.Report{Checks: []prerequisites.Check{row("DNSServer/dns 192.0.2.53:53/tcp"), row("DNSServer/dns 192.0.2.53:53/udp")}}
	result, err := service.PreflightInfrastructure(context.Background(), InfrastructurePreflightRequest{})
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
	report, _ := clusterpreflight.Assemble("", "", "", "", &Report{Checks: checks})
	copy(checks, report.Checks)
}

func TestInfrastructureDryRunRunsNoControllerCheckAndKeepsEveryCheckPending(t *testing.T) {
	service, controller, budget, _ := fixture()
	result, err := service.PreflightInfrastructure(context.Background(), InfrastructurePreflightRequest{DryRun: true})
	found := diagnostics.Of(err)
	if result == nil || len(found) != 1 || found[0].Code != "preflight.unknown" || controller.calls != 0 || !budget.dryRun {
		t.Fatalf("result %+v, error %#v, controller calls %d", result, err, controller.calls)
	}
	for _, check := range result.Checks {
		if check.Status != capacity.Pending {
			t.Fatalf("dry-run check %+v", check)
		}
	}
	if result.Summary.Pending != 2 || len(result.Checks) != 2 {
		t.Fatalf("summary %+v", result.Summary)
	}
}

func TestInfrastructureIsReadyWhenEveryCheckPasses(t *testing.T) {
	service, _, _, _ := fixture()
	result, err := service.PreflightInfrastructure(context.Background(), InfrastructurePreflightRequest{})
	if err != nil || result.Summary.Passed != 2 || len(result.Checks) != 2 {
		t.Fatalf("result %+v, error %v", result, err)
	}
}

func TestInfrastructureReturnsAControllerErrorItsRowsDoNotExplain(t *testing.T) {
	boom := errors.New("listener inspection stopped")
	service, controller, _, _ := fixture()
	controller.err = boom
	result, err := service.PreflightInfrastructure(context.Background(), InfrastructurePreflightRequest{})
	if result != nil || !errors.Is(err, boom) {
		t.Fatalf("result %+v, error %v", result, err)
	}
	service, controller, _, _ = fixture()
	controller.report, controller.err = nil, boom
	if result, err = service.PreflightInfrastructure(context.Background(), InfrastructurePreflightRequest{}); result != nil || !errors.Is(err, boom) {
		t.Fatalf("result %+v, error %v", result, err)
	}
}

func TestInfrastructureStopsTheOtherChecksWhenOneErrorsWithoutAReport(t *testing.T) {
	boom := errors.New("capacity read stopped")
	service, controller, budget, _ := fixture()
	controller.waitForCancel = true
	budget.err = boom
	result, err := service.PreflightInfrastructure(context.Background(), InfrastructurePreflightRequest{})
	if result != nil || !errors.Is(err, boom) || !controller.cancelled {
		t.Fatalf("result %+v, error %v, controller cancelled %t", result, err, controller.cancelled)
	}
	stopped := errors.New("controller read stopped")
	service, controller, budget, _ = fixture()
	budget.waitForCancel = true
	controller.report, controller.err = nil, stopped
	result, err = service.PreflightInfrastructure(context.Background(), InfrastructurePreflightRequest{})
	if result != nil || !errors.Is(err, stopped) || !budget.cancelled {
		t.Fatalf("result %+v, error %v, capacity cancelled %t", result, err, budget.cancelled)
	}
	fromController, fromCapacity := errors.New("controller side"), errors.New("capacity side")
	service, controller, budget, _ = fixture()
	controller.report, controller.err = nil, fromController
	budget.err = fromCapacity
	if _, err = service.PreflightInfrastructure(context.Background(), InfrastructurePreflightRequest{}); !errors.Is(err, fromController) {
		t.Fatalf("the controller error must win when both fail: %v", err)
	}
}

func TestInfrastructureAndAllAreUnavailableWithoutTheirCollaboratorsAndStopOnACancelledContext(t *testing.T) {
	service, _, _, _ := fixture()
	service.Capacity = nil
	if _, err := service.PreflightInfrastructure(context.Background(), InfrastructurePreflightRequest{}); !errors.Is(err, availability.ErrNotImplemented) {
		t.Fatalf("error %v", err)
	}
	service, _, _, _ = fixture()
	service.Clusters = nil
	if _, err := service.PreflightAll(context.Background(), AllPreflightRequest{}); !errors.Is(err, availability.ErrNotImplemented) {
		t.Fatalf("error %v", err)
	}
	service, controller, budget, clusters := fixture()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, first := service.PreflightInfrastructure(ctx, InfrastructurePreflightRequest{})
	_, second := service.PreflightAll(ctx, AllPreflightRequest{})
	if !errors.Is(first, context.Canceled) || !errors.Is(second, context.Canceled) || controller.calls+budget.calls+clusters.calls != 0 {
		t.Fatalf("errors %v, %v", first, second)
	}
}

func TestAllMergesInfrastructureAndClusterChecks(t *testing.T) {
	service, controller, budget, clusters := fixture()
	ssh := machine.SSHOptions{User: "operator"}
	clusters.report.Checks = append(clusters.report.Checks, capacity.Check{ID: "capacity.memory", Target: "host", Status: capacity.Failed, Reason: "from clusters"})
	result, err := service.PreflightAll(context.Background(), AllPreflightRequest{TrustOnFirstUse: true, SSH: ssh})
	found := diagnostics.Of(err)
	if result == nil || len(found) != 1 || found[0].Code != "preflight.failed" || !strings.Contains(found[0].Remediation, "preflight all") {
		t.Fatalf("result %+v, error %#v", result, err)
	}
	if result.Context != "lab" || result.Target != "all" {
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

func TestAllKeepsTheInfrastructureCheckAtEqualSeverity(t *testing.T) {
	service, _, _, clusters := fixture()
	clusters.report.Checks = []capacity.Check{{ID: "capacity.memory", Target: "host", Status: capacity.Passed, Reason: "from clusters"}}
	result, _ := service.PreflightAll(context.Background(), AllPreflightRequest{})
	for _, check := range result.Checks {
		if check.ID == "capacity.memory" && check.Reason != "memory fits" {
			t.Fatalf("the cluster check displaced the infrastructure check at equal severity: %+v", check)
		}
	}
}

func TestAllReportsAnInfrastructureFailureBesideTheClusterChecks(t *testing.T) {
	service, controller, _, clusters := fixture()
	controller.report.Checks[0].Status = "not-ready"
	controller.err = diagnostics.NewFailure("preflight.failed", "not ready", "")
	result, err := service.PreflightAll(context.Background(), AllPreflightRequest{})
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

func TestAllStopsTheOtherChecksWhenOneErrorsWithoutAReport(t *testing.T) {
	boom := errors.New("cluster observation stopped")
	service, controller, budget, clusters := fixture()
	controller.waitForCancel, budget.waitForCancel = true, true
	clusters.report, clusters.err = nil, boom
	result, err := service.PreflightAll(context.Background(), AllPreflightRequest{})
	if result != nil || !errors.Is(err, boom) || !controller.cancelled || !budget.cancelled {
		t.Fatalf("result %+v, error %v, cancelled %t %t", result, err, controller.cancelled, budget.cancelled)
	}
	stopped := errors.New("capacity read stopped")
	service, _, budget, clusters = fixture()
	clusters.waitForCancel = true
	budget.err = stopped
	result, err = service.PreflightAll(context.Background(), AllPreflightRequest{})
	if result != nil || !errors.Is(err, stopped) || !clusters.cancelled {
		t.Fatalf("result %+v, error %v, clusters cancelled %t", result, err, clusters.cancelled)
	}
	fromInfrastructure, fromClusters := errors.New("infrastructure side"), errors.New("clusters side")
	service, _, budget, clusters = fixture()
	budget.err = fromInfrastructure
	clusters.report, clusters.err = nil, fromClusters
	if _, err = service.PreflightAll(context.Background(), AllPreflightRequest{}); !errors.Is(err, fromInfrastructure) {
		t.Fatalf("the infrastructure error must win when both fail: %v", err)
	}
}

func TestAllDryRunRunsNoControllerCheck(t *testing.T) {
	service, controller, budget, clusters := fixture()
	result, err := service.PreflightAll(context.Background(), AllPreflightRequest{DryRun: true})
	if result == nil || err == nil || controller.calls != 0 || !budget.dryRun || !clusters.request.DryRun {
		t.Fatalf("result %+v, error %v, controller calls %d", result, err, controller.calls)
	}
}

func TestAllRefusesSSHOverridesNoCephClusterReachesBeforeAnyCheckRuns(t *testing.T) {
	ssh := machine.SSHOptions{User: "operator"}
	service, controller, budget, clusters := fixture()
	service.Graph = graph{api.NewCatalog([]api.Object{object(api.DNSServer, "local-dns", field("management", "managed"))})}
	result, err := service.PreflightAll(context.Background(), AllPreflightRequest{SSH: ssh})
	found := diagnostics.Of(err)
	if result != nil || len(found) != 1 || found[0].Code != "cli.usage" || found[0].Remediation == "" || controller.calls+budget.calls+clusters.calls != 0 {
		t.Fatalf("result %+v, error %#v, calls %d %d %d", result, err, controller.calls, budget.calls, clusters.calls)
	}
	service, _, _, clusters = fixture()
	if _, err = service.PreflightAll(context.Background(), AllPreflightRequest{SSH: ssh}); err != nil || clusters.request.SSH != ssh {
		t.Fatalf("a declared CephCluster takes the overrides: %v", err)
	}
}
