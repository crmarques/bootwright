package power

import (
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

func nestedField(name string, value api.Value) api.FieldValue {
	return api.FieldValue{Name: name, Value: value}
}

func nestedText(name, value string) api.FieldValue { return nestedField(name, api.StringValue(value)) }

// A KubeVirt Machine a cluster of this context hosts has no controller access
// a power command can be lent outside an operation's custody, so the command
// refuses naming the host rather than lending a Secret that does not exist.
func TestPowerOnAHostedKubeVirtMachineRefusesNamingItsHost(t *testing.T) {
	f, s := nestedField, nestedText
	catalog := api.NewCatalog([]api.Object{
		api.NewObject(api.Environment, "lab", api.Value{}, api.MapValue(f("controller", api.MapValue(s("machineRef", "controller"))))),
		api.NewObject(api.Machine, "controller", api.Value{}, api.MapValue(f("os", api.MapValue(f("provided", api.BoolValue(true)))), f("access", api.MapValue(f("local", api.BoolValue(true)))))),
		api.NewObject(api.NetworkConfig, "guests", api.Value{}, api.MapValue(f("nmstate", api.MapValue(f("interfaces", api.ListValue(api.MapValue(s("name", "enp1s0"), s("type", "ethernet"), s("state", "up")))))))),
		api.NewObject(api.InfraProvider, "kv", api.Value{}, api.MapValue(f("kubevirt", api.MapValue(s("hostClusterRef", "host"), s("namespace", "guests"))))),
		api.NewObject(api.Machine, "node-0", api.Value{}, api.MapValue(f("substrate", api.MapValue(s("providerRef", "kv"))),
			f("os", api.MapValue(f("provided", api.BoolValue(false)))), f("network", api.MapValue(s("configRef", "guests"))))),
	})
	object, _ := catalog.Find(api.Machine, "node-0")
	_, _, _, _, err := controllerFor(catalog, object, "lab", "controller", nil)
	found := diagnostics.Of(err)
	if len(found) != 1 || found[0].Code != "access.unavailable" || !strings.Contains(found[0].Message, "OpenShiftCluster/host") {
		t.Fatalf("power refused %+v (%v)", found, err)
	}
	surveys, err := readSurveysFor(catalog, "lab", []string{"node-0"}, nil)
	if err != nil || len(surveys) != 0 {
		t.Fatalf("the power reading surveyed %+v, %v", surveys, err)
	}
}
