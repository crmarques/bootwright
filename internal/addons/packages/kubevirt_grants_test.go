package packages

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/crmarques/bootwright/internal/addons"
)

const hostGrantsGolden = "../../../ansible/collections/ansible_collections/bootwright/core/tests/unit/goldens/kubevirt_host_grants.json"

type grantedRule struct {
	APIGroups     []string `json:"apiGroups"`
	ResourceNames []string `json:"resourceNames,omitempty"`
	Resources     []string `json:"resources"`
	Verbs         []string `json:"verbs"`
}

type grantedRole struct {
	Kind      string        `json:"kind"`
	Name      string        `json:"name"`
	Namespace string        `json:"namespace,omitempty"`
	Rules     []grantedRule `json:"rules"`
}

type grantBinding struct {
	Kind      string `json:"kind"`
	Namespace string `json:"namespace,omitempty"`
	Role      string `json:"role"`
	RoleKind  string `json:"roleKind"`
}

// The collection's tests hold every read the provider proof issues against the
// roles read from this table, so a read added without its grant fails there.
func TestTheHostedAccountsRolesAreTheTableTheCollectionHoldsItsReadsAgainst(t *testing.T) {
	rendered, err := kubevirt().Render(addons.Instance{Cluster: "host", Config: packageTestConfig("kubevirt"),
		Facts: addons.ClusterFacts{Name: "host", HostedNamespaces: []addons.HostedNamespace{{Name: "guests"}}}})
	if err != nil {
		t.Fatal(err)
	}
	table := struct {
		Bindings []grantBinding `json:"bindings"`
		Roles    []grantedRole  `json:"roles"`
	}{Bindings: []grantBinding{}, Roles: []grantedRole{}}
	for _, object := range rendered.After[1].Objects {
		switch object.Kind {
		case "ClusterRole", "Role":
			role := grantedRole{Kind: object.Kind, Name: object.Name, Namespace: object.Namespace, Rules: []grantedRule{}}
			for _, entry := range object.Body.Get("rules").Items() {
				role.Rules = append(role.Rules, grantedRule{APIGroups: entry.Get("apiGroups").Strings(), ResourceNames: entry.Get("resourceNames").Strings(),
					Resources: entry.Get("resources").Strings(), Verbs: entry.Get("verbs").Strings()})
			}
			table.Roles = append(table.Roles, role)
		case "ClusterRoleBinding", "RoleBinding":
			table.Bindings = append(table.Bindings, grantBinding{Kind: object.Kind, Namespace: object.Namespace,
				Role: object.Body.Get("roleRef", "name").Text(), RoleKind: object.Body.Get("roleRef", "kind").Text()})
		}
	}
	data, err := json.MarshalIndent(table, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, '\n')
	if *update {
		if err := os.MkdirAll(filepath.Dir(hostGrantsGolden), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(hostGrantsGolden, data, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(hostGrantsGolden)
	if err != nil {
		t.Fatalf("%v; run with -update to write it", err)
	}
	if !bytes.Equal(want, data) {
		t.Errorf("%s differs; rerun with -update if the change is intended, then run the collection's KubeVirt tests:\ngolden %s\ngot    %s", hostGrantsGolden, want, data)
	}
}
