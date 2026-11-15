package access

import (
	"context"
	"slices"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/environment"
)

func TestContainerSessionsResolveNamesFQDNsAndRoleOrdinals(t *testing.T) {
	nodes := []environment.Member{
		{Name: "z", FQDN: "z.example.test", Machine: "host-z", Role: "worker"},
		{Name: "b", FQDN: "b.example.test", Machine: "host-b", Role: "worker"},
		{Name: "a", FQDN: "a.example.test", Machine: "host-a", Role: "controlPlane"},
		{Name: "infra-0", FQDN: "i.example.test", Machine: "host-i", Role: "worker"},
		{Name: "c", FQDN: "c.example.test", Machine: "host-c", Role: "infra"},
	}
	cluster := api.NewObject(api.OpenShiftCluster, "example", api.Value{}, api.MapValue())
	for selector, want := range map[string]string{"": "host-a", "b": "host-b", "z.example.test": "host-z", "worker-0": "host-b", "worker-2": "host-z", "controlPlane-0": "host-a", "infra-0": "host-i"} {
		for _, reverse := range []bool{false, true} {
			roster := slices.Clone(nodes)
			if reverse {
				slices.Reverse(roster)
			}
			sessions := &recordingSessions{}
			service := New(&fixedGraph{catalog: api.NewCatalog([]api.Object{cluster})}, sessions, func(context.Context) (string, error) { return "example", nil }).
				WithMembers(func(api.Object) (environment.Membership, bool) { return environment.Membership{Nodes: roster}, true }).
				WithAdministrator(func(api.Object) string { return "a" })
			payload := []string{"printf", "%s", "", "--node", "z"}
			result, err := service.ClusterExec(t.Context(), ClusterExecRequest{Names: []string{"example"}, Nodes: single(selector), Command: payload})
			if err != nil || result.ExitCode != 4 || len(sessions.opened) != 1 || !slices.Equal(sessions.opened[0].machines, []string{want}) || !slices.Equal(sessions.opened[0].command, payload) {
				t.Fatalf("selector %q: result %+v, error %v, opened %+v", selector, result, err, sessions.opened)
			}
			result, err = service.ClusterRsh(t.Context(), ClusterRshRequest{ContextName: "explicit", Name: "example", Node: selector})
			if err != nil || result.ExitCode != 3 || len(sessions.opened) != 2 || sessions.opened[1].machine != want || sessions.opened[1].context != "explicit" {
				t.Fatalf("rsh %q: result %+v, error %v, opened %+v", selector, result, err, sessions.opened)
			}
		}
	}
}

func TestContainerSessionInvalidOrAmbiguousSelectorsOpenNothing(t *testing.T) {
	for _, selector := range []string{"missing", "worker-01", "worker-+1", "worker--1", "worker-9", "worker-999999999999999999999", "worker-", "infra-0", "shared.example.test", "duplicate"} {
		nodes := []environment.Member{
			{Name: "duplicate", FQDN: "shared.example.test", Machine: "host-a", Role: "worker"},
			{Name: "duplicate", FQDN: "shared.example.test", Machine: "host-b", Role: "worker"},
		}
		cluster := api.NewObject(api.OpenShiftCluster, "example", api.Value{}, api.MapValue())
		sessions := &recordingSessions{}
		service := New(&fixedGraph{catalog: api.NewCatalog([]api.Object{cluster})}, sessions, nil).
			WithMembers(func(api.Object) (environment.Membership, bool) { return environment.Membership{Nodes: nodes}, true })
		_, err := service.ClusterRsh(t.Context(), ClusterRshRequest{ContextName: "example", Name: "example", Node: selector})
		if found := diagnostics.Of(err); len(found) != 1 || found[0].Code != "access.target" || len(sessions.opened) != 0 {
			t.Fatalf("%q: error %v, opened %+v", selector, err, sessions.opened)
		}
	}
}
