package preflight

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/substrate/capacity"
)

func m(values ...any) api.Value {
	fields := []api.FieldValue{}
	for i := 0; i < len(values); i += 2 {
		v, ok := values[i+1].(api.Value)
		if !ok {
			v = api.StringValue(values[i+1].(string))
		}
		fields = append(fields, api.FieldValue{Name: values[i].(string), Value: v})
	}
	return api.MapValue(fields...)
}
func object(kind api.Kind, name string, spec api.Value) api.Object {
	return api.NewObject(kind, name, api.Value{}, spec)
}

type graph struct{ catalog api.Catalog }

func (g graph) Effective(context.Context, string) (api.Catalog, error) { return g.catalog, nil }

type checker func(context.Context, []capacity.Allocation, bool) ([]capacity.Check, error)

func (f checker) Check(ctx context.Context, a []capacity.Allocation, dry bool) ([]capacity.Check, error) {
	return f(ctx, a, dry)
}
func fixture(t *testing.T, managed bool) Service {
	t.Helper()
	node := object(api.Machine, "node", m("network", m("inline", m("dns", api.ListValue(m("serverRef", "dns", "endpointRef", "ip"))))))
	objects := []api.Object{object(api.Environment, "env", m("domains", m("containerClusters", "example.test"))), node,
		object(api.ContainerCluster, "sno", m("nodes", api.ListValue(m("machineRef", "node")), "install", m("endpoints", m("api", m("address", "192.0.2.10"), "api-int", m("address", "192.0.2.10"), "ingress", m("address", "192.0.2.10")))))}
	if managed {
		objects = append(objects, object(api.DNSServer, "dns", m("management", "managed", "bindAddress", "192.0.2.53", "endpoints", api.ListValue(m("name", "ip", "address", "192.0.2.53")))))
	}
	return Service{Graph: graph{api.NewCatalog(objects)}, Project: func(api.Catalog, string) ([]capacity.Allocation, error) { return nil, nil }, Capacity: checker(func(context.Context, []capacity.Allocation, bool) ([]capacity.Check, error) {
		t.Fatal("capacity called without libvirt guests")
		return nil, nil
	}), Blocks: func(context.Context, string) (map[string]string, error) {
		return map[string]string{"dns-dns": "done"}, nil
	}}
}
func TestResolutionUsesExactNSSCommandAndChecksEveryAnswer(t *testing.T) {
	for _, row := range []struct {
		name, answer, status string
		err                  error
	}{
		{"equal", "192.0.2.10 STREAM\n192.0.2.10 DGRAM\n", capacity.Passed, nil},
		{"different", "192.0.2.11 STREAM\n", capacity.Failed, nil},
		{"extra", "192.0.2.10 STREAM\n192.0.2.11 STREAM\n", capacity.Failed, nil},
		{"empty", "", capacity.Failed, nil},
		{"failed", "", capacity.Failed, errors.New("getent failed")},
	} {
		t.Run(row.name, func(t *testing.T) {
			s := fixture(t, true)
			calls := []string{}
			s.Resolver = ResolverFunc(func(_ context.Context, argv []string) ([]byte, error) {
				if len(argv) != 3 || argv[0] != "getent" || argv[1] != "ahosts" {
					t.Fatalf("argv %v", argv)
				}
				calls = append(calls, argv[2])
				return []byte(row.answer), row.err
			})
			report, err := s.Check(context.Background(), PreflightRequest{ContextName: "lab"})
			if (err == nil) != (row.status == capacity.Passed) || report == nil || len(report.Checks) != 4 || len(calls) != 4 {
				t.Fatalf("report %+v, error %v, calls %v", report, err, calls)
			}
			for _, check := range report.Checks {
				if check.Status != row.status {
					t.Fatalf("check %+v", check)
				}
				if check.Status != capacity.Passed && (!strings.Contains(check.Next, check.Target) || !strings.Contains(check.Next, "192.0.2.53")) {
					t.Fatalf("missing record/route %+v", check)
				}
			}
		})
	}
}
func TestResolutionChecksTheNamesTheInstallationProvesAndNeverTheApex(t *testing.T) {
	long := strings.Repeat("c", 51) + "-d" + strings.Repeat("e", 10)
	for _, row := range []struct {
		cluster string
		want    map[string]string
	}{
		{"sno", map[string]string{
			"resolution.api":              "api.sno.example.test",
			"resolution.api-int":          "api-int.sno.example.test",
			"resolution.ingress":          "console-openshift-console.apps.sno.example.test",
			"resolution.ingress-wildcard": "bootwright-sno.apps.sno.example.test",
		}},
		{long, map[string]string{
			"resolution.api":              "api." + long + ".example.test",
			"resolution.api-int":          "api-int." + long + ".example.test",
			"resolution.ingress":          "console-openshift-console.apps." + long + ".example.test",
			"resolution.ingress-wildcard": "bootwright-" + strings.Repeat("c", 51) + ".apps." + long + ".example.test",
		}},
	} {
		t.Run(row.cluster[:3], func(t *testing.T) {
			s := fixture(t, true)
			endpoints := m("api", m("address", "192.0.2.10"), "api-int", m("address", "192.0.2.10"), "ingress", m("address", "192.0.2.10"))
			s.Graph = graph{api.NewCatalog([]api.Object{object(api.Environment, "env", m("domains", m("containerClusters", "example.test"))),
				object(api.ContainerCluster, row.cluster, m("install", m("endpoints", endpoints)))})}
			resolved := []string{}
			s.Resolver = ResolverFunc(func(_ context.Context, argv []string) ([]byte, error) {
				resolved = append(resolved, argv[2])
				return []byte("192.0.2.10 STREAM\n"), nil
			})
			report, err := s.Check(context.Background(), PreflightRequest{ContextName: "lab"})
			if err != nil {
				t.Fatal(err)
			}
			got := map[string]string{}
			for _, check := range report.Checks {
				got[check.ID] = check.Target
			}
			if !reflect.DeepEqual(got, row.want) || len(resolved) != len(row.want) {
				t.Fatalf("checks %v, resolved %v, want %v", got, resolved, row.want)
			}
			for _, name := range resolved {
				if strings.HasPrefix(name, "apps.") {
					t.Fatalf("resolved the applications apex %s, which a wildcard does not answer", name)
				}
			}
		})
	}
}
func TestManagedDNSIsPendingBeforeItsBlockCompletes(t *testing.T) {
	s := fixture(t, true)
	s.Blocks = func(context.Context, string) (map[string]string, error) { return nil, nil }
	s.Resolver = ResolverFunc(func(context.Context, []string) ([]byte, error) {
		t.Fatal("resolved before managed DNS block was done")
		return nil, nil
	})
	report, err := s.Check(context.Background(), PreflightRequest{ContextName: "lab"})
	if err == nil || report.Summary.Pending != 4 {
		t.Fatalf("%+v %v", report, err)
	}
	for _, check := range report.Checks {
		if !strings.Contains(check.Next, "--stage infra-components") {
			t.Fatal(check)
		}
	}
}
func TestDryRunNeverResolvesOrObservesAndPreservesCapacityProjection(t *testing.T) {
	s := fixture(t, false)
	s.Blocks = func(context.Context, string) (map[string]string, error) {
		t.Fatal("dry-run read lifecycle blocks")
		return nil, nil
	}
	s.Resolver = ResolverFunc(func(context.Context, []string) ([]byte, error) { t.Fatal("dry-run resolved names"); return nil, nil })
	allocations := []capacity.Allocation{{Machine: "node", Host: "controller", Pool: "/pool", Quantities: capacity.Quantities{CPU: 4}}, {Machine: "other", Host: "controller", Pool: "/pool", Quantities: capacity.Quantities{CPU: 3}}, {Machine: "foreign", Host: "elsewhere", Pool: "/pool"}}
	s.Project = func(api.Catalog, string) ([]capacity.Allocation, error) {
		return append([]capacity.Allocation{}, allocations...), nil
	}
	s.Capacity = checker(func(_ context.Context, got []capacity.Allocation, dry bool) ([]capacity.Check, error) {
		if !dry || !reflect.DeepEqual(got, allocations[:2]) {
			t.Fatalf("projection %v dry %t", got, dry)
		}
		return []capacity.Check{{ID: "capacity.cpu", Target: "controller", Status: capacity.Unknown}}, nil
	})
	report, err := s.Check(context.Background(), PreflightRequest{ContextName: "lab", DryRun: true})
	if err == nil || report.Summary.Pending != 5 {
		t.Fatalf("%+v %v", report, err)
	}
}
func TestSelectionRejectsUnknownClusterBeforeResolution(t *testing.T) {
	s := fixture(t, false)
	if report, err := s.Check(context.Background(), PreflightRequest{ContextName: "lab", Clusters: []string{"absent"}}); err == nil || report != nil {
		t.Fatalf("%+v %v", report, err)
	}
}

func TestControllerPreflightRefusesUnusedRemoteOptions(t *testing.T) {
	for _, request := range []PreflightRequest{{ContextName: "lab", TrustOnFirstUse: true}, {ContextName: "lab", SSH: machine.SSHOptions{User: "operator"}}} {
		s := fixture(t, false)
		if report, err := s.Check(context.Background(), request); err == nil || report != nil {
			t.Fatalf("unused remote options accepted: %+v %v", report, err)
		}
	}
}
