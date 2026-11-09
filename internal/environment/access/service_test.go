package access

import (
	"context"
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/machine"
)

type fixedGraph struct {
	catalog api.Catalog
	asked   []string
}

func (g *fixedGraph) Effective(_ context.Context, contextName string) (api.Catalog, error) {
	g.asked = append(g.asked, contextName)
	return g.catalog, nil
}

type openedSession struct {
	context, machine string
	machines         []string
	command          []string
	interactive      bool
}

type recordingSessions struct{ opened []openedSession }

func (r *recordingSessions) Rsh(_ context.Context, contextName, name string, _ machine.SSHOptions) (*machine.SessionResult, error) {
	r.opened = append(r.opened, openedSession{context: contextName, machine: name, interactive: true})
	return &machine.SessionResult{Context: contextName, Machine: name, ExitCode: 3}, nil
}

func (r *recordingSessions) Exec(_ context.Context, contextName string, names []string, _ machine.SSHOptions, command []string) (*machine.SessionResult, error) {
	r.opened = append(r.opened, openedSession{context: contextName, machines: names, command: command})
	return &machine.SessionResult{Context: contextName, Machine: strings.Join(names, ","), ExitCode: 4}, nil
}

func text(value string) api.Value { return api.StringValue(value) }

func field(name string, value api.Value) api.FieldValue {
	return api.FieldValue{Name: name, Value: value}
}

func node(name, fqdn, machineRef string) api.Value {
	return api.MapValue(field("name", text(name)), field("fqdn", text(fqdn)), field("machineRef", text(machineRef)))
}

func storageCluster(name, management string, nodes ...api.Value) api.Object {
	administrator := ""
	if len(nodes) > 0 {
		administrator = nodes[0].Get("name").Text()
	}
	return storageClusterAdministeredBy(name, management, administrator, nodes...)
}

func storageClusterAdministeredBy(name, management, administrator string, nodes ...api.Value) api.Object {
	return api.NewObject(api.CephCluster, name, api.Value{}, api.MapValue(
		field("management", text(management)),
		field("cephadm", api.MapValue(field("bootstrap", api.MapValue(field("nodeRef", text(administrator)))))),
		field("topology", api.MapValue(field("nodes", api.ListValue(nodes...)))),
	))
}

func administrator(cluster api.Object) string {
	return cluster.Spec().Get("cephadm", "bootstrap", "nodeRef").Text()
}

func sessionCatalog(nodes ...api.Value) api.Catalog {
	return sessionCatalogAdministeredBy("", nodes...)
}

func sessionCatalogAdministeredBy(administrator string, nodes ...api.Value) api.Catalog {
	ceph := storageCluster("ceph", "managed", nodes...)
	if administrator != "" {
		ceph = storageClusterAdministeredBy("ceph", "managed", administrator, nodes...)
	}
	return api.NewCatalog([]api.Object{
		ceph,
		storageCluster("outside", "external"),
		api.NewObject(api.OpenShiftCluster, "ocp", api.Value{}, api.MapValue(field("type", text("openshift")))),
	})
}

func labNodes() []api.Value {
	return []api.Value{
		node("rhel-03", "c.lab.test", "machine-c"),
		node("rhel-01", "a.lab.test", "machine-a"),
		node("rhel-02", "b.lab.test", "machine-b"),
	}
}

func TestClusterSessionsOpenTheSelectedNodesMachine(t *testing.T) {
	tests := []struct {
		name, node, want string
		nodes            []api.Value
	}{
		{name: "default is the administrator node, not the first in name order", want: "machine-c", nodes: labNodes()},
		{name: "exact node name", node: "rhel-02", want: "machine-b", nodes: labNodes()},
		{name: "FQDN", node: "c.lab.test", want: "machine-c", nodes: labNodes()},
		{name: "a node name wins over a FQDN that spells it", node: "rhel-02", want: "machine-b",
			nodes: []api.Value{node("rhel-01", "rhel-02", "machine-a"), node("rhel-02", "b.lab.test", "machine-b")}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			graph, sessions := &fixedGraph{catalog: sessionCatalog(tt.nodes...)}, &recordingSessions{}
			service := New(graph, sessions, func(context.Context) (string, error) { return "lab", nil }).WithMembers(storageMembers).WithAdministrator(administrator)
			result, err := service.ClusterExec(context.Background(), ClusterExecRequest{Names: []string{"ceph"}, Nodes: []string{tt.node}, Command: []string{"hostname", "--node", "rhel-03"}})
			if err != nil {
				t.Fatal(err)
			}
			want := openedSession{context: "lab", machines: []string{tt.want}, command: []string{"hostname", "--node", "rhel-03"}}
			if len(sessions.opened) != 1 || !equalSession(sessions.opened[0], want) || result.ExitCode != 4 {
				t.Fatalf("opened %+v (result %+v), want %+v", sessions.opened, result, want)
			}
			if !slices.Equal(graph.asked, []string{"lab"}) {
				t.Fatalf("rendered %v, want the current context once", graph.asked)
			}
			result, err = service.ClusterRsh(context.Background(), ClusterRshRequest{ContextName: "other", Name: "ceph", Node: tt.node})
			if err != nil || len(sessions.opened) != 2 || !equalSession(sessions.opened[1], openedSession{context: "other", machine: tt.want, interactive: true}) || result.ExitCode != 3 {
				t.Fatalf("rsh opened %+v (%v)", sessions.opened, err)
			}
		})
	}
}

func equalSession(got, want openedSession) bool {
	return got.context == want.context && got.machine == want.machine && slices.Equal(got.machines, want.machines) && got.interactive == want.interactive && slices.Equal(got.command, want.command)
}

func TestClusterSessionRefusalsOpenNothing(t *testing.T) {
	shared := []api.Value{node("rhel-01", "a.lab.test", "machine-a"), node("rhel-02", "s.lab.test", "machine-b"), node("rhel-03", "s.lab.test", "machine-c")}
	tests := []struct {
		name, cluster, node, code string
		nodes                     []api.Value
	}{
		{name: "unknown cluster", cluster: "missing", code: "access.target", nodes: labNodes()},
		{name: "no cluster name", code: "access.target", nodes: labNodes()},
		{name: "external storage", cluster: "outside", code: "cluster.not-applicable", nodes: labNodes()},
		{name: "OpenShift OpenShiftCluster", cluster: "ocp", code: "access.unavailable", nodes: labNodes()},
		{name: "unknown node", cluster: "ceph", node: "rhel-09", code: "access.target", nodes: labNodes()},
		{name: "role ordinal is no selector", cluster: "ceph", node: "mon-0", code: "access.target", nodes: labNodes()},
		{name: "duplicate FQDN", cluster: "ceph", node: "s.lab.test", code: "access.target", nodes: shared},
		{name: "no declared node", cluster: "ceph", code: "access.target"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sessions := &recordingSessions{}
			service := New(&fixedGraph{catalog: sessionCatalog(tt.nodes...)}, sessions, nil).WithMembers(storageMembers).WithAdministrator(administrator)
			_, err := service.ClusterExec(context.Background(), ClusterExecRequest{ContextName: "lab", Names: []string{tt.cluster}, Nodes: []string{tt.node}, Command: []string{"true"}})
			if found := diagnostics.Of(err); len(found) != 1 || found[0].Code != tt.code {
				t.Fatalf("exec refusal = %v, want %s", err, tt.code)
			}
			_, err = service.ClusterRsh(context.Background(), ClusterRshRequest{ContextName: "lab", Name: tt.cluster, Node: tt.node})
			if found := diagnostics.Of(err); len(found) != 1 || found[0].Code != tt.code {
				t.Fatalf("rsh refusal = %v, want %s", err, tt.code)
			}
			if len(sessions.opened) != 0 {
				t.Fatalf("a refusal opened %+v", sessions.opened)
			}
		})
	}
}

// A stretch cluster's provided tiebreaker sorts first, but it holds no
// administrator keyring: without --node the session opens the administrator
// node, and a node named by --node, the tiebreaker included, opens its own Machine.
func TestClusterSessionsOfAStretchClusterWithAProvidedTiebreaker(t *testing.T) {
	nodes := []api.Value{
		node("ceph-02", "ceph-02.lab.test", "ceph-02"),
		node("arbiter-01", "arbiter-01.lab.test", "arbiter-01"),
		node("ceph-01", "ceph-01.lab.test", "ceph-01"),
	}
	for name, want := range map[string]string{"": "ceph-01", "ceph-02.lab.test": "ceph-02", "arbiter-01": "arbiter-01", "arbiter-01.lab.test": "arbiter-01"} {
		graph, sessions := &fixedGraph{catalog: sessionCatalogAdministeredBy("ceph-01", nodes...)}, &recordingSessions{}
		service := New(graph, sessions, func(context.Context) (string, error) { return "lab", nil }).WithMembers(storageMembers).WithAdministrator(administrator)
		if _, err := service.ClusterExec(context.Background(), ClusterExecRequest{Names: []string{"ceph"}, Nodes: []string{name}, Command: []string{"ceph", "status"}}); err != nil {
			t.Fatalf("exec node %q: %v", name, err)
		}
		if _, err := service.ClusterRsh(context.Background(), ClusterRshRequest{Name: "ceph", Node: name}); err != nil {
			t.Fatalf("rsh node %q: %v", name, err)
		}
		if len(sessions.opened) != 2 || !slices.Equal(sessions.opened[0].machines, []string{want}) || sessions.opened[1].machine != want {
			t.Fatalf("node %q opened %+v, want %s", name, sessions.opened, want)
		}
	}
}

func TestClusterSessionWithoutNodeRefusesWhenNoNodeCanBeTheAdministrator(t *testing.T) {
	tests := map[string]string{"no bootstrap node": "", "a bootstrap node the roster does not declare": "ceph-09"}
	for name, bootstrap := range tests {
		t.Run(name, func(t *testing.T) {
			catalog := api.NewCatalog([]api.Object{storageClusterAdministeredBy("ceph", "managed", bootstrap,
				node("arbiter-01", "arbiter-01.lab.test", "arbiter-01"), node("ceph-01", "ceph-01.lab.test", "ceph-01"))})
			sessions := &recordingSessions{}
			service := New(&fixedGraph{catalog: catalog}, sessions, nil).WithMembers(storageMembers).WithAdministrator(administrator)
			_, err := service.ClusterExec(context.Background(), ClusterExecRequest{ContextName: "lab", Names: []string{"ceph"}, Command: []string{"true"}})
			found := diagnostics.Of(err)
			if len(found) != 1 || found[0].Code != "access.target" || !strings.Contains(found[0].Message, "has no administrator node") || !strings.Contains(found[0].Remediation, "arbiter-01, ceph-01") {
				t.Fatalf("exec refusal = %v", err)
			}
			if _, err = service.ClusterRsh(context.Background(), ClusterRshRequest{ContextName: "lab", Name: "ceph"}); len(diagnostics.Of(err)) != 1 {
				t.Fatalf("rsh refusal = %v", err)
			}
			if _, err = service.ClusterRsh(context.Background(), ClusterRshRequest{ContextName: "lab", Name: "ceph", Node: "arbiter-01"}); err != nil {
				t.Fatalf("an explicit node opens regardless: %v", err)
			}
			if len(sessions.opened) != 1 || sessions.opened[0].machine != "arbiter-01" {
				t.Fatalf("opened %+v, want only the explicit node's Machine", sessions.opened)
			}
		})
	}
}
