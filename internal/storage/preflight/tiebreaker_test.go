package preflight

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/substrate/capacity"
)

func fields(kv ...any) api.Value {
	out := []api.FieldValue{}
	for i := 0; i < len(kv); i += 2 {
		value, ok := kv[i+1].(api.Value)
		switch typed := kv[i+1].(type) {
		case string:
			value, ok = api.StringValue(typed), true
		case bool:
			value, ok = api.BoolValue(typed), true
		case []api.Value:
			value, ok = api.ListValue(typed...), true
		}
		if !ok {
			panic(kv[i])
		}
		out = append(out, api.FieldValue{Name: kv[i].(string), Value: value})
	}
	return api.MapValue(out...)
}

func stretchMachine(name, address string, provided bool, access api.Value) api.Object {
	spec := fields("os", fields("provided", provided),
		"network", fields("addresses", []api.Value{fields("name", "fqdn", "address", name+".example.test"), fields("name", "ip", "address", address)}))
	if access.Present() {
		spec = spec.With("access", fields("ssh", access))
	}
	return api.NewObject(api.Machine, name, api.Value{}, spec)
}

func stretchCatalog(witnessAccess api.Value, services ...api.Value) api.Catalog {
	nodes := []api.Value{}
	objects := []api.Object{
		api.NewObject(api.Environment, "env", api.Value{}, fields("remoteMachinesAccessKey", fields("keyRef", "fleet"))),
		api.NewObject(api.Secret, "fleet", api.Value{}, fields("type", "sshKeyPair")),
		stretchMachine("witness", "192.0.2.20", true, witnessAccess),
	}
	for _, name := range []string{"ceph-01", "ceph-02"} {
		objects = append(objects, stretchMachine(name, "192.0.2.1"+name[len(name)-1:], false, api.Value{}))
		nodes = append(nodes, fields("name", name, "fqdn", name+".example.test", "machineRef", name))
	}
	nodes = append(nodes, fields("name", "witness", "fqdn", "witness.example.test", "machineRef", "witness"))
	objects = append(objects, api.NewObject(api.CephCluster, "storage", api.Value{}, fields(
		"cephadm", fields("clusterSSH", fields("user", "bootwright")),
		"topology", fields("nodes", nodes, "stretch", fields("tiebreaker", fields("node", "witness"))),
		"services", services)))
	return api.NewCatalog(objects)
}

func pinnedWitness() api.Value {
	return fields("addressRef", "ip", "user", "bootwright", "knownHostsRef", "witness-known-hosts", "auth", fields("operatorIdentity", fields()))
}

func stretchService(catalog api.Catalog, commands CommandFunc) Service {
	return Service{Graph: graphStub{catalog}, Ownership: ownedStub{"Machine/ceph-01": {Verb: "apply", State: "done"}, "Machine/ceph-02": {Verb: "apply", State: "done"}},
		Project: func(api.Catalog, string) ([]Allocation, error) { return nil, nil }, Capacity: fixtureBudget(), Guests: GuestChecks{Commands: commands}}
}

func answer(request CommandRequest) []byte {
	argv := request.Argv
	if argv[0] == sudoCmd {
		argv = argv[2:]
	}
	switch argv[0] {
	case "/usr/bin/hostname":
		return []byte(request.Machine + ".example.test\n")
	case "/usr/bin/timedatectl":
		return []byte("yes\n")
	case "/usr/bin/id":
		return []byte("0\n")
	case "/usr/bin/cat":
		return []byte("ID=rhel\n")
	case "/usr/bin/getent":
		return []byte("192.0.2.1 STREAM " + argv[len(argv)-1] + "\n")
	}
	return []byte("answer\n")
}

func rowsOf(report *Report, target string) []Check {
	out := []Check{}
	for _, check := range report.Checks {
		if check.Target == target {
			out = append(out, check)
		}
	}
	return out
}

func TestProvidedTiebreakerPreflightReadsOverTheFleetConnection(t *testing.T) {
	service := stretchService(stretchCatalog(pinnedWitness()), func(_ context.Context, request CommandRequest) ([]byte, error) {
		return answer(request), nil
	})
	report, err := service.Check(context.Background(), PreflightRequest{ContextName: "lab"})
	if err != nil {
		t.Fatalf("%+v %v", report, err)
	}
	if rows := rowsOf(report, "witness"); len(rows) != 6 {
		t.Fatalf("the tiebreaker reported %+v", rows)
	}

	unreached := func(_ context.Context, request CommandRequest) ([]byte, error) {
		if request.Machine == "witness" {
			return nil, ErrUnreached
		}
		return answer(request), nil
	}
	report, err = stretchService(stretchCatalog(pinnedWitness()), unreached).Check(context.Background(), PreflightRequest{ContextName: "lab"})
	rows := rowsOf(report, "witness")
	if err == nil || len(rows) != 1 || rows[0].ID != "host.access" || rows[0].Status != capacity.Unknown {
		t.Fatalf("an unreached tiebreaker reported %+v %v", rows, err)
	}
	for _, want := range []string{"Secret fleet", "bootwright secret show --name fleet --part public-key", "user bootwright on Machine/witness", "Secret witness-known-hosts"} {
		if !strings.Contains(rows[0].Next, want) {
			t.Errorf("the remedy %q does not name %q", rows[0].Next, want)
		}
	}
	if !strings.Contains(rows[0].Reason, "192.0.2.20:22") {
		t.Errorf("the reason %q names no endpoint", rows[0].Reason)
	}
}

func TestProvidedTiebreakerWithoutAPinIsNeverRead(t *testing.T) {
	access := fields("addressRef", "ip", "user", "bootwright", "auth", fields("operatorIdentity", fields()))
	service := stretchService(stretchCatalog(access), func(_ context.Context, request CommandRequest) ([]byte, error) {
		if request.Machine == "witness" {
			t.Fatal("an unpinned tiebreaker was read")
		}
		return answer(request), nil
	})
	report, _ := service.Check(context.Background(), PreflightRequest{ContextName: "lab"})
	rows := rowsOf(report, "witness")
	if len(rows) != 1 || rows[0].ID != "host.access" || !strings.Contains(rows[0].Next, "knownHostsRef") {
		t.Fatalf("%+v", rows)
	}
}

func TestAPinnedHostKeyRefusalNamesItsSecret(t *testing.T) {
	service := stretchService(stretchCatalog(pinnedWitness()), func(_ context.Context, request CommandRequest) ([]byte, error) {
		if request.Machine == "witness" {
			return nil, UntrustedKey{}
		}
		return answer(request), nil
	})
	report, _ := service.Check(context.Background(), PreflightRequest{ContextName: "lab"})
	rows := rowsOf(report, "witness")
	if len(rows) != 1 || rows[0].ID != "host.trust" || !strings.Contains(rows[0].Next, "Secret witness-known-hosts") || strings.Contains(rows[0].Next, "machine trust") {
		t.Fatalf("%+v", rows)
	}
}

func TestADataHostUnreachedKeepsItsUnknownRows(t *testing.T) {
	service := stretchService(stretchCatalog(pinnedWitness()), func(_ context.Context, request CommandRequest) ([]byte, error) {
		if request.Machine == "ceph-01" {
			return nil, errors.Join(ErrUnreached)
		}
		return answer(request), nil
	})
	report, _ := service.Check(context.Background(), PreflightRequest{ContextName: "lab"})
	for _, row := range rowsOf(report, "ceph-01") {
		if row.ID == "host.access" || row.Status != capacity.Unknown {
			t.Fatalf("%+v", row)
		}
	}
}

func placedOn(serviceType string, hosts ...string) api.Value {
	values := []api.Value{}
	for _, host := range hosts {
		values = append(values, api.StringValue(host+".example.test"))
	}
	return fields("service_type", serviceType, "placement", fields("hosts", values))
}

func resolverOf(known map[string][]string) CommandFunc {
	return func(_ context.Context, request CommandRequest) ([]byte, error) {
		if request.Argv[0] != "/usr/bin/getent" || len(request.Argv) == 3 && request.Argv[2] == request.Machine+".example.test" {
			return answer(request), nil
		}
		for _, name := range request.Argv[2:] {
			if !slices.Contains(known[request.Machine], name) {
				return nil, ExitStatus{Code: 2}
			}
		}
		return answer(request), nil
	}
}

func peerRows(report *Report) map[string]Check {
	out := map[string]Check{}
	for _, check := range report.Checks {
		if check.ID == "host.peer-names" {
			out[check.Target] = check
		}
	}
	return out
}

func TestPeerNameThatDoesNotResolveFailsBeforeTheServiceBlock(t *testing.T) {
	catalog := stretchCatalog(pinnedWitness(), placedOn("mgr", "ceph-01", "ceph-02"), placedOn("mgmt-gateway", "ceph-01", "ceph-02"))
	everyName := []string{"ceph-01.example.test", "ceph-02.example.test", "witness.example.test"}
	commands := resolverOf(map[string][]string{"ceph-01": {"ceph-01.example.test"}, "ceph-02": everyName})
	report, err := stretchService(catalog, commands).Check(context.Background(), PreflightRequest{ContextName: "lab"})
	rows := peerRows(report)
	if !preflightFailed(err) || len(rows) != 2 || rows["ceph-02"].Status != capacity.Passed {
		t.Fatalf("%+v %v", rows, err)
	}
	failed := rows["ceph-01"]
	want := "Machine/ceph-01 runs mgmt-gateway, mgr and does not resolve ceph-02.example.test, witness.example.test, which cephadm writes into its configuration by name"
	if failed.Status != capacity.Failed || failed.Reason != want || failed.Next != "make ceph-01's resolver answer every Ceph node name" {
		t.Fatalf("%+v", failed)
	}
}

func TestPeerNamesCheckedOnlyOnNameAddressingPlacements(t *testing.T) {
	commands := resolverOf(map[string][]string{})
	report, _ := stretchService(stretchCatalog(pinnedWitness(), placedOn("mgr", "ceph-01"), placedOn("mon", "ceph-01", "ceph-02", "witness")), commands).Check(context.Background(), PreflightRequest{ContextName: "lab"})
	if rows := peerRows(report); len(rows) != 0 {
		t.Fatalf("a cluster with no name-addressing service checked %+v", rows)
	}
	report, _ = stretchService(stretchCatalog(pinnedWitness(), placedOn("mgr", "ceph-01"), placedOn("grafana", "ceph-02")), commands).Check(context.Background(), PreflightRequest{ContextName: "lab"})
	rows := peerRows(report)
	if len(rows) != 2 || rows["ceph-01"].Status != capacity.Failed || rows["ceph-02"].Status != capacity.Failed || !strings.Contains(rows["ceph-02"].Reason, "runs grafana and") {
		t.Fatalf("%+v", rows)
	}
	if _, checked := rows["witness"]; checked {
		t.Fatal("the tiebreaker, which runs no name-addressing service, was checked")
	}
}

func TestPeerNamesUnknownWhenTheReadDoesNotComplete(t *testing.T) {
	commands := func(_ context.Context, request CommandRequest) ([]byte, error) {
		if request.Argv[0] == "/usr/bin/getent" && len(request.Argv) > 3 {
			return nil, errors.New("verified bounded prerequisite command did not complete")
		}
		return answer(request), nil
	}
	report, _ := stretchService(stretchCatalog(pinnedWitness(), placedOn("alertmanager", "ceph-01")), commands).Check(context.Background(), PreflightRequest{ContextName: "lab"})
	if row := peerRows(report)["ceph-01"]; row.Status != capacity.Unknown {
		t.Fatalf("%+v", row)
	}
}

func TestHostDNSNotFoundIsFailedNotUnknown(t *testing.T) {
	host := stretchMachine("ceph-01", "192.0.2.11", false, api.Value{})
	for _, test := range []struct {
		err  error
		want string
	}{{ExitStatus{Code: 2}, capacity.Failed}, {errors.New("verified bounded prerequisite command did not complete"), capacity.Unknown}, {ErrUnreached, capacity.Unknown}} {
		checker := GuestChecks{Commands: CommandFunc(func(_ context.Context, request CommandRequest) ([]byte, error) {
			if request.Argv[0] == "/usr/bin/getent" {
				return nil, test.err
			}
			return answer(request), nil
		})}
		rows, err := checker.CheckGuest(context.Background(), "lab", host, HostRequirements{}, PreflightRequest{})
		if err != nil {
			t.Fatal(err)
		}
		for _, row := range rows {
			if row.ID == "host.dns" && row.Status != test.want {
				t.Fatalf("%v: %+v", test.err, row)
			}
		}
	}
}

func preflightFailed(err error) bool {
	for _, d := range diagnostics.Of(err) {
		if d.Code == "preflight.failed" {
			return true
		}
	}
	return false
}
