package access

import (
	"context"
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

func severalCatalog() api.Catalog {
	return api.NewCatalog([]api.Object{
		storageCluster("beta", "managed", node("b2", "b2.lab.test", "machine-b2"), node("b1", "b1.lab.test", "machine-b1"), node("shared", "s.beta.test", "machine-bs")),
		storageCluster("alpha", "managed", node("a2", "a2.lab.test", "machine-a2"), node("a1", "a1.lab.test", "machine-a1"), node("shared", "s.alpha.test", "machine-as")),
		storageCluster("gamma", "managed", node("g1", "g1.lab.test", "machine-a1")),
		storageCluster("empty", "managed"),
		storageCluster("dup", "managed", node("d1", "same.lab.test", "machine-d1"), node("d2", "same.lab.test", "machine-d2")),
		storageCluster("outside", "external"),
		storageCluster("outside2", "external"),
		api.NewObject(api.ContainerCluster, "ocp", api.Value{}, api.MapValue(field("type", text("openshift")))),
	})
}

func execSeveral(t *testing.T, names, nodes []string) (*recordingSessions, error) {
	t.Helper()
	sessions := &recordingSessions{}
	service := New(&fixedGraph{catalog: severalCatalog()}, sessions, nil).WithMembers(storageMembers)
	_, err := service.ClusterExec(context.Background(), ClusterExecRequest{ContextName: "lab", Names: names, Nodes: nodes, Command: []string{"uptime"}})
	return sessions, err
}

func TestClusterExecHandsEveryResolvedMachineToOneSessionCall(t *testing.T) {
	tests := []struct {
		name         string
		names, nodes []string
		want         []string
	}{
		{name: "several clusters run on their first nodes", names: []string{"beta", "alpha"}, want: []string{"machine-a1", "machine-b1"}},
		{name: "one cluster, several nodes by name and FQDN", names: []string{"alpha"}, nodes: []string{"a2.lab.test", "a1"}, want: []string{"machine-a1", "machine-a2"}},
		{name: "several clusters split the node members they declare", names: []string{"alpha", "beta"}, nodes: []string{"a1", "b1", "b2"}, want: []string{"machine-a1", "machine-b1", "machine-b2"}},
		{name: "a node name two clusters declare runs in both", names: []string{"alpha", "beta"}, nodes: []string{"shared"}, want: []string{"machine-as", "machine-bs"}},
		{name: "a Machine two nodes share runs once", names: []string{"gamma", "alpha"}, want: []string{"machine-a1"}},
		{name: "a Machine two selected nodes share runs once", names: []string{"gamma", "alpha"}, nodes: []string{"g1", "a1"}, want: []string{"machine-a1"}},
		{name: "names and nodes are trimmed and deduplicated", names: []string{" alpha ", "alpha", ""}, nodes: []string{"a2", " a2", ""}, want: []string{"machine-a2"}},
		{name: "one cluster and one node is today's session", names: []string{"beta"}, nodes: []string{"b2"}, want: []string{"machine-b2"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sessions, err := execSeveral(t, tt.names, tt.nodes)
			if err != nil {
				t.Fatal(err)
			}
			if len(sessions.opened) != 1 {
				t.Fatalf("Exec was called %d times, want once: %+v", len(sessions.opened), sessions.opened)
			}
			got := slices.Clone(sessions.opened[0].machines)
			slices.Sort(got)
			if !slices.Equal(got, tt.want) || sessions.opened[0].context != "lab" || !slices.Equal(sessions.opened[0].command, []string{"uptime"}) {
				t.Fatalf("Exec got %+v, want machines %v", sessions.opened[0], tt.want)
			}
		})
	}
}

func TestClusterExecRefusesOnceNamingEveryOffender(t *testing.T) {
	tests := []struct {
		name         string
		names, nodes []string
		code         string
		contains     []string
		excludes     []string
	}{
		{name: "every unknown cluster in one diagnostic", names: []string{"zed", "alpha", "missing"}, code: "access.target",
			contains: []string{"ContainerClusters or StorageClusters named missing, zed"}, excludes: []string{"alpha"}},
		{name: "unknown cluster precedes an external one", names: []string{"outside", "missing"}, code: "access.target", contains: []string{"named missing"}},
		{name: "first external cluster in canonical order", names: []string{"outside2", "alpha", "outside"}, code: "cluster.not-applicable",
			contains: []string{"does not apply to outside,"}},
		{name: "external precedes OpenShift", names: []string{"ocp", "outside"}, code: "cluster.not-applicable", contains: []string{"outside"}},
		{name: "OpenShift refuses a list", names: []string{"alpha", "ocp"}, code: "access.unavailable", contains: []string{"for ocp,"}},
		{name: "every unknown node in one diagnostic", names: []string{"alpha", "beta"}, nodes: []string{"nope", "a1", "zilch"}, code: "access.target",
			contains: []string{"none of StorageClusters alpha, beta declares a node named nope, zilch or one with that FQDN"}},
		{name: "unknown nodes of one cluster", names: []string{"alpha"}, nodes: []string{"x9", "x8"}, code: "access.target",
			contains: []string{"StorageCluster alpha declares no node named x8, x9 and none with that FQDN"}},
		{name: "a cluster declaring none of the nodes", names: []string{"alpha", "beta"}, nodes: []string{"a1", "a2"}, code: "access.target",
			contains: []string{"StorageCluster beta declares none of the nodes a1, a2"}, excludes: []string{"alpha declares"}},
		{name: "every idle cluster in one diagnostic", names: []string{"alpha", "beta", "empty"}, nodes: []string{"a1"}, code: "access.target",
			contains: []string{"StorageClusters beta, empty declare none of the nodes a1"}},
		{name: "an unknown node precedes an idle cluster", names: []string{"alpha", "beta"}, nodes: []string{"a1", "nope"}, code: "access.target",
			contains: []string{"declares a node named nope"}},
		{name: "a shared FQDN selects none", names: []string{"alpha", "dup"}, nodes: []string{"same.lab.test"}, code: "access.target",
			contains: []string{"FQDN same.lab.test names several nodes of StorageCluster dup"}},
		{name: "a cluster with no node and no selector", names: []string{"alpha", "empty"}, code: "access.target",
			contains: []string{"StorageCluster empty declares no node"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sessions, err := execSeveral(t, tt.names, tt.nodes)
			found := diagnostics.Of(err)
			if len(found) != 1 || found[0].Code != tt.code {
				t.Fatalf("refusal = %v, want one %s", err, tt.code)
			}
			for _, want := range tt.contains {
				if !strings.Contains(found[0].Message, want) {
					t.Errorf("message %q lacks %q", found[0].Message, want)
				}
			}
			for _, unwanted := range tt.excludes {
				if strings.Contains(found[0].Message, unwanted) {
					t.Errorf("message %q names %q", found[0].Message, unwanted)
				}
			}
			if len(sessions.opened) != 0 {
				t.Fatalf("a refusal opened %+v", sessions.opened)
			}
		})
	}
}

func TestClusterExecSingleRefusalsKeepTheirMessages(t *testing.T) {
	tests := []struct {
		name         string
		names, nodes []string
		message      string
	}{
		{name: "unknown cluster", names: []string{"missing"},
			message: "the context lab selects no ContainerCluster or StorageCluster named missing"},
		{name: "unknown node", names: []string{"alpha"}, nodes: []string{"rhel-09"},
			message: "StorageCluster alpha declares no node named rhel-09 and none with that FQDN"},
		{name: "no cluster name", message: "no cluster name was supplied"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := execSeveral(t, tt.names, tt.nodes)
			if found := diagnostics.Of(err); len(found) != 1 || found[0].Code != "access.target" || found[0].Message != tt.message {
				t.Fatalf("refusal = %v, want access.target %q", err, tt.message)
			}
		})
	}
}

func TestClusterRshStaysOneSessionOnTheSelectedNode(t *testing.T) {
	sessions := &recordingSessions{}
	service := New(&fixedGraph{catalog: severalCatalog()}, sessions, nil).WithMembers(storageMembers)
	if _, err := service.ClusterRsh(context.Background(), ClusterRshRequest{ContextName: "lab", Name: "beta", Node: "b2"}); err != nil {
		t.Fatal(err)
	}
	if want := (openedSession{context: "lab", machine: "machine-b2", interactive: true}); len(sessions.opened) != 1 || !equalSession(sessions.opened[0], want) {
		t.Fatalf("rsh opened %+v, want %+v", sessions.opened, want)
	}
	for _, request := range []ClusterRshRequest{{ContextName: "lab", Name: " beta ", Node: "b2"}, {ContextName: "lab", Name: "beta", Node: " "}} {
		_, err := service.ClusterRsh(context.Background(), request)
		if reported := diagnostics.Of(err); len(reported) != 1 || reported[0].Code != "access.target" || len(sessions.opened) != 1 {
			t.Fatalf("rsh %+v = %v and opened %d sessions; a single value is matched exactly, never trimmed", request, err, len(sessions.opened))
		}
	}
}
