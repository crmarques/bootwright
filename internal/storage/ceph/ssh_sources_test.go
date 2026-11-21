package ceph

import (
	"encoding/json"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

func TestSSHPolicyAdmitsAllEligibleCephManagers(t *testing.T) {
	request := Request{
		Bootstrap: "seed",
		Hosts: []Host{
			{Name: "seed", Machine: "seed", Address: "192.0.2.10"},
			{Name: "standby", Machine: "standby", Address: "192.0.2.11"},
			{Name: "data", Machine: "data", Address: "192.0.2.12"},
		},
		Services: []json.RawMessage{json.RawMessage(`{"service_type":"mgr","placement":{"hosts":["seed","standby"],"count":1}}`)},
	}
	catalog := func(sources ...string) api.Catalog {
		entries := []api.Value{}
		for _, source := range sources {
			entries = append(entries, api.MapValue(api.FieldValue{Name: "ip", Value: api.StringValue(source)}))
		}
		spec := api.Value{}.WithPath(api.ListValue(entries...), "firewall", "ssh", "allowedSources")
		return api.NewCatalog([]api.Object{api.NewObject(api.Machine, "data", api.Value{}, spec)})
	}
	err := validateSSHSourceAdmission(catalog("192.0.2.10"), request)
	if issues := diagnostics.Of(err); len(issues) != 1 || !strings.Contains(issues[0].Message, "Machine/standby") {
		t.Fatalf("eligible standby exclusion was not refused: %+v", issues)
	}
	if err := validateSSHSourceAdmission(catalog("192.0.2.10", "192.0.2.11"), request); err != nil {
		t.Fatal(err)
	}
}
