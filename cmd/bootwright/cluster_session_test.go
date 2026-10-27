//go:build linux && amd64

package main

import (
	"bytes"
	"context"
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/cli"
	"github.com/crmarques/bootwright/internal/machine"
	machineaccess "github.com/crmarques/bootwright/internal/machine/access"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/trust"
)

type cephSessionOwnership struct{}

func (cephSessionOwnership) Ownership(context.Context, string) (map[string]machine.OwnershipState, error) {
	done := machine.OwnershipState{Verb: machine.VerbApply, State: machine.BlockDone}
	return map[string]machine.OwnershipState{"Machine/rhel-01": done, "Machine/rhel-02": done, "Machine/rhel-03": done}, nil
}

type cephSessionProof struct {
	catalog api.Catalog
	key     trust.HostKey
}

func (p cephSessionProof) HostKey(_ context.Context, _ string, name string) (machine.HostKeyEvidence, bool, error) {
	object, found := p.catalog.Find(api.Machine, name)
	if !found {
		return machine.HostKeyEvidence{}, false, nil
	}
	target, _ := machine.SSH(object)
	return machine.HostKeyEvidence{Address: target.Address, HostKey: p.key}, true, nil
}

type cephSessionRig struct {
	catalog api.Catalog
	lender  *labSessionLender
	client  *labSessionClient
}

func newCephSessionRig(t *testing.T, edit func(api.Catalog) api.Catalog) cephSessionRig {
	t.Helper()
	state, _ := compileAcceptance(t, exampleDirectory(t, "libvirt-3-nodes-ceph"))
	catalog := state.Effective()
	if edit != nil {
		catalog = edit(catalog)
	}
	guest := requireObject(t, catalog, api.Machine, "rhel-01")
	target, ok := machine.SSH(guest)
	if !ok || target.PrivateKeyRef == "" {
		t.Fatalf("rhel-01 access = %+v", target)
	}
	return cephSessionRig{
		catalog: catalog,
		lender: &labSessionLender{held: map[string]secrets.Material{
			target.PrivateKeyRef: secrets.NewMaterial(map[secrets.Part][]byte{secrets.PrivateKeyPart: []byte("FLEET KEY")}),
		}},
		client: &labSessionClient{},
	}
}

func (r cephSessionRig) run(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	proved, err := trust.ParseAuthorizedKey("ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAICG+RadYRVwHqO7OQsOpyh8SDf1tbTQ8PUmgn7UEmou/")
	if err != nil {
		t.Fatal(err)
	}
	state := labSessionState{effective: r.catalog}
	sessions := machineaccess.New(state, nil, machineaccess.Options{
		Lender: r.lender, Ownership: cephSessionOwnership{},
		Evidence: cephSessionProof{catalog: r.catalog, key: proved}, Launcher: r.client,
	})
	var out, errOut bytes.Buffer
	services := cli.Services{EnvironmentAccess: clusterSessions(state, sessions, nil)}
	code := runServices(t.Context(), args, &out, &errOut, services)
	return code, out.String(), errOut.String()
}

func withCephNodes(edit func([]api.Value) []api.Value) func(api.Catalog) api.Catalog {
	return func(catalog api.Catalog) api.Catalog {
		objects := catalog.Objects()
		for i, object := range objects {
			if object.Kind() == api.CephCluster && object.Name() == "ceph-lab" {
				spec := object.Spec()
				nodes := edit(spec.Get("topology", "nodes").Items())
				objects[i] = object.WithSpec(spec.WithPath(api.ListValue(nodes...), "topology", "nodes"))
			}
		}
		return api.NewCatalog(objects)
	}
}

func withFQDN(name, fqdn string) func([]api.Value) []api.Value {
	return func(nodes []api.Value) []api.Value {
		for i, node := range nodes {
			if node.Get("name").Text() == name {
				nodes[i] = node.With("fqdn", api.StringValue(fqdn))
			}
		}
		return nodes
	}
}

func TestClusterSessionsReachTheSelectedCephNode(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		edit    func(api.Catalog) api.Catalog
		machine string
		command []string
	}{
		{name: "the default node is the first in name order", machine: "rhel-01", command: []string{"true"},
			args: []string{"cluster", "exec", "--context", "lab", "--name", "ceph-lab", "true"}},
		{name: "a node name selects its node", machine: "rhel-02", command: []string{"true"},
			args: []string{"cluster", "exec", "--context", "lab", "--name", "ceph-lab", "--node", "rhel-02", "true"}},
		{name: "a FQDN selects its node", machine: "rhel-03", command: []string{"true"},
			args: []string{"cluster", "exec", "--context", "lab", "--name", "ceph-lab", "--node", "rhel-03.lab.example.test", "true"}},
		{name: "a node name wins over a FQDN that spells it", machine: "rhel-02", command: []string{"true"},
			edit: withCephNodes(withFQDN("rhel-03", "rhel-02")),
			args: []string{"cluster", "exec", "--context", "lab", "--name", "ceph-lab", "--node", "rhel-02", "true"}},
		{name: "the payload tail never retargets the session", machine: "rhel-02",
			command: []string{"echo", "--node", "rhel-03", "--name", "other", "--context", "elsewhere"},
			args:    []string{"cluster", "exec", "--context", "lab", "--name", "ceph-lab", "--node", "rhel-02", "--", "echo", "--node", "rhel-03", "--name", "other", "--context", "elsewhere"}},
		{name: "rsh opens an interactive session", machine: "rhel-03",
			args: []string{"cluster", "rsh", "--context", "lab", "--name", "ceph-lab", "--node", "rhel-03"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rig := newCephSessionRig(t, tt.edit)
			code, out, errOut := rig.run(t, tt.args...)
			if code != 0 || out != "" || errOut != "" {
				t.Fatalf("exit %d, out %q, err %q", code, out, errOut)
			}
			if len(rig.client.sessions) != 1 {
				t.Fatalf("sessions = %+v", rig.client.sessions)
			}
			session := rig.client.sessions[0]
			target, _ := machine.SSH(requireObject(t, rig.catalog, api.Machine, tt.machine))
			if session.Machine != tt.machine || session.Address != target.Address || session.User != "bootwright" || session.Kind != machine.IdentityKey {
				t.Fatalf("session = %+v, want %s at %s", session, tt.machine, target.Address)
			}
			if !slices.Equal(session.Command, tt.command) || rig.client.keys[0] != "FLEET KEY" {
				t.Fatalf("command %q with key %q, want %q", session.Command, rig.client.keys[0], tt.command)
			}
		})
	}
}

func TestClusterSessionRefusalsExitTwoFiftyFiveBeforeAnyCredential(t *testing.T) {
	external := func(catalog api.Catalog) api.Catalog {
		spec := api.MapValue(api.FieldValue{Name: "management", Value: api.StringValue("external")})
		ocp := api.MapValue(api.FieldValue{Name: "type", Value: api.StringValue("openshift")})
		return api.NewCatalog(append(catalog.Objects(),
			api.NewObject(api.CephCluster, "ceph-outside", api.Value{}, spec),
			api.NewObject(api.OpenShiftCluster, "ocp", api.Value{}, ocp)))
	}
	shared := withCephNodes(func(nodes []api.Value) []api.Value {
		return withFQDN("rhel-03", "rhel-02.lab.example.test")(nodes)
	})
	tests := []struct {
		name, code string
		edit       func(api.Catalog) api.Catalog
		args       []string
	}{
		{name: "unknown cluster", code: "access.target", args: []string{"--name", "missing"}},
		{name: "unknown node", code: "access.target", args: []string{"--name", "ceph-lab", "--node", "rhel-09"}},
		{name: "duplicate FQDN", code: "access.target", edit: shared, args: []string{"--name", "ceph-lab", "--node", "rhel-02.lab.example.test"}},
		{name: "external storage", code: "cluster.not-applicable", edit: external, args: []string{"--name", "ceph-outside"}},
		{name: "OpenShift OpenShiftCluster", code: "access.unavailable", edit: external, args: []string{"--name", "ocp"}},
	}
	for _, tt := range tests {
		for _, verb := range []string{"rsh", "exec"} {
			t.Run(tt.name+"/"+verb, func(t *testing.T) {
				rig := newCephSessionRig(t, tt.edit)
				args := append([]string{"cluster", verb, "--context", "lab"}, tt.args...)
				if verb == "exec" {
					args = append(args, "--", "hostname", "--node", "rhel-01")
				}
				code, out, errOut := rig.run(t, args...)
				if code != 255 || out != "" || !strings.HasPrefix(errOut, "  error  "+tt.code+"\n") {
					t.Fatalf("exit %d, out %q, err %q; want 255 and %s", code, out, errOut, tt.code)
				}
				if rig.lender.requested != nil || len(rig.client.sessions) != 0 {
					t.Fatalf("a refusal opened %v and ran %+v", rig.lender.requested, rig.client.sessions)
				}
			})
		}
	}
}

func TestClusterExecOnSeveralNodesRunsEachMachineOnceTogether(t *testing.T) {
	second := func(catalog api.Catalog) api.Catalog {
		first := requireObject(t, catalog, api.CephCluster, "ceph-lab")
		nodes := first.Spec().Get("topology", "nodes").Items()
		spec := api.MapValue(api.FieldValue{Name: "management", Value: api.StringValue("managed")},
			api.FieldValue{Name: "topology", Value: api.MapValue(api.FieldValue{Name: "nodes", Value: api.ListValue(nodes[:1]...)})})
		return api.NewCatalog(append(catalog.Objects(), api.NewObject(api.CephCluster, "ceph-twin", api.Value{}, spec)))
	}
	tests := []struct {
		name string
		args []string
		want []string
	}{
		{name: "two nodes of one cluster", args: []string{"--name", "ceph-lab", "--node", "rhel-03,rhel-01"}, want: []string{"rhel-01", "rhel-03"}},
		{name: "two clusters share a Machine", args: []string{"--name", "ceph-twin,ceph-lab"}, want: []string{"rhel-01"}},
		{name: "two clusters and a node both declare", args: []string{"--name", "ceph-twin,ceph-lab", "--node", "rhel-01,rhel-02"}, want: []string{"rhel-01", "rhel-02"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rig := newCephSessionRig(t, second)
			code, out, errOut := rig.run(t, append(append([]string{"cluster", "exec", "--context", "lab"}, tt.args...), "--", "true")...)
			var ran []string
			for _, session := range rig.client.sessions {
				ran = append(ran, session.Machine)
			}
			slices.Sort(ran)
			if code != 0 || out != "" || errOut != "" || !slices.Equal(ran, tt.want) {
				t.Fatalf("exit %d, out %q, err %q, ran %v, want %v", code, out, errOut, ran, tt.want)
			}
		})
	}
	rig := newCephSessionRig(t, second)
	code, out, errOut := rig.run(t, "cluster", "exec", "--context", "lab", "--name", "ceph-twin,ceph-lab", "--node", "rhel-02", "--", "true")
	if code != 255 || out != "" || !strings.HasPrefix(errOut, "  error  access.target\n    ") || !strings.Contains(errOut, "CephCluster ceph-twin declares none of the nodes rhel-02") ||
		rig.lender.requested != nil || len(rig.client.sessions) != 0 {
		t.Fatalf("exit %d, out %q, err %q, lent %v", code, out, errOut, rig.lender.requested)
	}
}
