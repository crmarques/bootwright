package access

import (
	"context"
	"slices"
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
	command          []string
	interactive      bool
}

type recordingSessions struct{ opened []openedSession }

func (r *recordingSessions) Rsh(_ context.Context, contextName, name string, _ machine.SSHOptions) (*machine.SessionResult, error) {
	r.opened = append(r.opened, openedSession{context: contextName, machine: name, interactive: true})
	return &machine.SessionResult{Context: contextName, Machine: name, ExitCode: 3}, nil
}

func (r *recordingSessions) Exec(_ context.Context, contextName, name string, _ machine.SSHOptions, command []string) (*machine.SessionResult, error) {
	r.opened = append(r.opened, openedSession{context: contextName, machine: name, command: command})
	return &machine.SessionResult{Context: contextName, Machine: name, ExitCode: 4}, nil
}

func text(value string) api.Value { return api.StringValue(value) }

func field(name string, value api.Value) api.FieldValue {
	return api.FieldValue{Name: name, Value: value}
}

func node(name, fqdn, machineRef string) api.Value {
	return api.MapValue(field("name", text(name)), field("fqdn", text(fqdn)), field("machineRef", text(machineRef)))
}

func storageCluster(name, management string, nodes ...api.Value) api.Object {
	return api.NewObject(api.StorageCluster, name, api.Value{}, api.MapValue(
		field("type", text("ceph")), field("management", text(management)),
		field("ceph", api.MapValue(field("topology", api.MapValue(field("nodes", api.ListValue(nodes...)))))),
	))
}

func sessionCatalog(nodes ...api.Value) api.Catalog {
	return api.NewCatalog([]api.Object{
		storageCluster("ceph", "managed", nodes...),
		storageCluster("outside", "external"),
		api.NewObject(api.ContainerCluster, "ocp", api.Value{}, api.MapValue(field("type", text("openshift")))),
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
		{name: "default is the first node in name order", want: "machine-a", nodes: labNodes()},
		{name: "exact node name", node: "rhel-02", want: "machine-b", nodes: labNodes()},
		{name: "FQDN", node: "c.lab.test", want: "machine-c", nodes: labNodes()},
		{name: "a node name wins over a FQDN that spells it", node: "rhel-02", want: "machine-b",
			nodes: []api.Value{node("rhel-01", "rhel-02", "machine-a"), node("rhel-02", "b.lab.test", "machine-b")}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			graph, sessions := &fixedGraph{catalog: sessionCatalog(tt.nodes...)}, &recordingSessions{}
			service := New(graph, sessions, func(context.Context) (string, error) { return "lab", nil })
			result, err := service.ClusterExec(context.Background(), ClusterExecRequest{Name: "ceph", Node: tt.node, Command: []string{"hostname", "--node", "rhel-03"}})
			if err != nil {
				t.Fatal(err)
			}
			want := openedSession{context: "lab", machine: tt.want, command: []string{"hostname", "--node", "rhel-03"}}
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
	return got.context == want.context && got.machine == want.machine && got.interactive == want.interactive && slices.Equal(got.command, want.command)
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
		{name: "OpenShift ContainerCluster", cluster: "ocp", code: "access.unavailable", nodes: labNodes()},
		{name: "unknown node", cluster: "ceph", node: "rhel-09", code: "access.target", nodes: labNodes()},
		{name: "role ordinal is no selector", cluster: "ceph", node: "mon-0", code: "access.target", nodes: labNodes()},
		{name: "duplicate FQDN", cluster: "ceph", node: "s.lab.test", code: "access.target", nodes: shared},
		{name: "no declared node", cluster: "ceph", code: "access.target"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sessions := &recordingSessions{}
			service := New(&fixedGraph{catalog: sessionCatalog(tt.nodes...)}, sessions, nil)
			_, err := service.ClusterExec(context.Background(), ClusterExecRequest{ContextName: "lab", Name: tt.cluster, Node: tt.node, Command: []string{"true"}})
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
