package sessionpolicy

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/diagnostics"
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

func TestSessionPolicyRejectsInvalidGrants(t *testing.T) {
	for _, test := range []struct {
		name, original, replacement, want string
	}{
		{"duplicated grant", "name: shell", "name: diagnostics", "distinct DNS-label names"},
		{"both subject types", "group: PLATFORM_SUPPORT", "group: PLATFORM_SUPPORT\n          user: analyst", "one valid user or group"},
		{"numeric subject", "group: PLATFORM_SUPPORT", "group: '2042'", "one valid user or group"},
		{"invalid target", "machineRefs: [gateway]", "machineRefs: [gateway.example.test]", "DNS-label target names"},
		{"missing command", `commands: [["/usr/bin/uptime"]]`, "commands: []", "at least one exec command"},
		{"relative command", "/usr/bin/uptime", "../uptime", "absolute executable path"},
		{"noncanonical command", "/usr/bin/uptime", "/usr/bin/../bin/uptime", "absolute executable path"},
		{"command with control character", `"/usr/bin/uptime"`, `"/usr/bin/uptime", "first\nsecond"`, "without control characters"},
	} {
		t.Run(test.name, func(t *testing.T) {
			data := strings.Replace(policyFixture, test.original, test.replacement, 1)
			if _, err := Decode([]byte(data), "lab"); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("decode error %v, want %q", err, test.want)
			}
		})
	}
}

type unreadablePolicy struct{}

func (unreadablePolicy) ReadSessionPolicy(context.Context, string) ([]byte, error) {
	return nil, errors.New("cannot read policy")
}

func TestPolicyPermissionsRefuseUnusableState(t *testing.T) {
	for _, test := range []struct {
		name       string
		store      Store
		lookup     func(context.Context, bool, string) (uint32, error)
		wantReason string
	}{
		{name: "unset policy", store: policyBytes("")},
		{name: "empty policy", store: policyBytes(strings.Split(policyFixture, "  grants:")[0] + "  grants: []\n")},
		{name: "unreadable policy", store: unreadablePolicy{}, wantReason: "active policy could not be read"},
		{name: "invalid policy", store: policyBytes("invalid"), wantReason: "active policy is invalid"},
		{
			name: "unresolved subject", store: policyBytes(policyFixture),
			lookup:     func(context.Context, bool, string) (uint32, error) { return 0, errors.New("directory is down") },
			wantReason: "a policy subject could not be verified",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			service := testPolicy()
			service.Store = test.store
			if test.lookup != nil {
				service.Lookup = test.lookup
			}
			grants, err := service.Permissions(t.Context(), Principal{UID: 3001}, "lab", policyCatalog("worker"))
			if test.wantReason == "" {
				if err != nil || grants == nil || len(grants) != 0 {
					t.Fatalf("permissions %+v, error %v, want an empty grant list", grants, err)
				}
				return
			}
			if got := diagnostics.Of(err); grants != nil || len(got) != 1 || got[0].Code != "access.denied" ||
				!strings.Contains(got[0].Message, test.wantReason) {
				t.Fatalf("permissions %+v, diagnostics %+v, want %q", grants, got, test.wantReason)
			}
		})
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
