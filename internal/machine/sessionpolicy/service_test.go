package sessionpolicy

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/machine"
)

const policyFixture = `apiVersion: bootwright.io/v1alpha1
kind: SessionAccessPolicy
metadata:
  name: lab
spec:
  grants:
    - name: diagnostics
      subjects:
        - group: PLATFORM_SUPPORT
        - user: 'analyst@example.test'
      targets:
        clusterRefs: [apps]
        machineRefs: [gateway]
      remoteUser: core
      exec:
        commands: [["/usr/bin/uptime"]]
    - name: shell
      subjects:
        - group: 'platform-oncall@example.test'
        - user: maintenance
      targets:
        machineRefs: [worker]
      remoteUser: core
      rsh: {}
`

type policyBytes string

func (p policyBytes) ReadSessionPolicy(context.Context, string) ([]byte, error) {
	return []byte(p), nil
}

func testPolicy() Service {
	return Service{
		Store: policyBytes(policyFixture),
		Lookup: func(_ context.Context, _ bool, name string) (uint32, error) {
			switch name {
			case "PLATFORM_SUPPORT":
				return 1001, nil
			case "analyst@example.test":
				return 3001, nil
			case "platform-oncall@example.test":
				return 1002, nil
			case "maintenance":
				return 3002, nil
			default:
				return 0, errors.New("unresolved subject")
			}
		},
		Members: func(cluster api.Object) ([]string, bool) {
			var names []string
			for _, node := range cluster.Spec().Get("nodes").Items() {
				names = append(names, node.Get("machineRef").Text())
			}
			return names, true
		},
	}
}

func TestSessionPolicyUsesCurrentRosterAndRequiresCompleteGrants(t *testing.T) {
	catalog := policyCatalog("worker", "worker-b")
	s := testPolicy()
	if err := s.Validate(t.Context(), "lab", catalog, []byte(policyFixture)); err != nil {
		t.Fatal(err)
	}
	diagnostics := Principal{UID: 3000, Groups: []uint32{1001}}
	argv := []string{"/usr/bin/uptime"}
	target := machine.SessionTarget{Machine: "worker", RemoteUser: "core"}
	grants, err := s.Permissions(t.Context(), diagnostics, "lab", catalog)
	if err != nil || len(grants) != 1 || !slices.Equal(grants[0].Machines, []string{"gateway", "worker", "worker-b"}) {
		t.Fatalf("unexpected grants %+v, %v", grants, err)
	}
	if err := s.Authorize(t.Context(), diagnostics, "lab", catalog, []machine.SessionTarget{target, {Machine: "worker-b", RemoteUser: "core"}}, argv); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		principal Principal
		catalog   api.Catalog
		targets   []machine.SessionTarget
		argv      []string
	}{
		{diagnostics, catalog, []machine.SessionTarget{target}, nil},
		{diagnostics, catalog, []machine.SessionTarget{target}, []string{"/usr/bin/uptime", "--help"}},
		{diagnostics, catalog, []machine.SessionTarget{target, {Machine: "worker-b", RemoteUser: "root"}}, argv},
		{diagnostics, policyCatalog("worker-b"), []machine.SessionTarget{target}, argv},
		{Principal{UID: 3002}, catalog, []machine.SessionTarget{target}, argv},
	} {
		if err := s.Authorize(t.Context(), tc.principal, "lab", tc.catalog, tc.targets, tc.argv); err == nil {
			t.Fatalf("authorization admitted %+v", tc)
		}
	}
	for _, principal := range []Principal{{UID: 3002}, {UID: 3000, Groups: []uint32{1002}}} {
		if err := s.Authorize(t.Context(), principal, "lab", catalog, []machine.SessionTarget{target}, nil); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Authorize(t.Context(), Principal{UID: 3001}, "lab", catalog, []machine.SessionTarget{target}, argv); err != nil {
		t.Fatal(err)
	}
}

func TestSessionPolicyRejectsInvalidDocuments(t *testing.T) {
	for _, replacement := range []string{"rsh: null", "rsh: {unknown: word}", "rsh: {}\n      exec: {}", "rsH: {}"} {
		if _, err := Decode([]byte(strings.Replace(policyFixture, "rsh: {}", replacement, 1)), "lab"); err == nil {
			t.Fatalf("accepted %q", replacement)
		}
	}
	if _, err := Decode([]byte(policyFixture), "another-context"); err == nil {
		t.Fatal("accepted different context")
	}
}

func policyCatalog(roster ...string) api.Catalog {
	var nodes []api.Value
	for _, name := range roster {
		nodes = append(nodes, api.MapValue(api.FieldValue{Name: "machineRef", Value: api.StringValue(name)}))
	}
	objects := []api.Object{
		api.NewObject(api.OpenShiftCluster, "apps", api.MapValue(), api.MapValue(api.FieldValue{Name: "nodes", Value: api.ListValue(nodes...)})),
	}
	for _, name := range []string{"worker", "worker-b", "gateway"} {
		objects = append(objects, api.NewObject(api.Machine, name, api.MapValue(), api.MapValue()))
	}
	return api.NewCatalog(objects)
}
