package preflight

import (
	"context"
	"errors"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
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
