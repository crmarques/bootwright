package agentinstall

import (
	"context"
	"reflect"
	"strconv"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets/material"
)

func TestAdmittedInertFieldsDoNotChangePlans(t *testing.T) {
	base := singleNodeCatalog()
	cluster, _ := base.Find(api.OpenShiftCluster, "sno")
	changes := map[string]api.Value{
		"release channel": cluster.Spec().WithPath(api.StringValue("fast-4.21"), "distribution", "release", "channel"),
	}
	for _, native := range nativeEndpoints {
		for field, value := range map[string]api.Value{
			"dnsName": api.StringValue(native.slot + ".custom.example.test"),
			"port":    api.IntegerValue(strconv.FormatInt(native.port, 10)), "scheme": api.StringValue("https"),
			"interfaceNetworks": api.StringList(),
		} {
			changes[native.slot+" "+field] = cluster.Spec().WithPath(value, "install", "endpoints", native.slot, field)
		}
	}
	for field, value := range map[string]api.Value{
		"labels": api.MapValue(text("example.test/rack", "rack-a")),
		"taints": api.ListValue(api.MapValue(text("key", "example.test/reserved"), text("effect", "NoSchedule"))),
	} {
		nodes := cluster.Spec().Get("nodes").Items()
		nodes[0] = nodes[0].With(field, value)
		changes[field] = cluster.Spec().With("nodes", api.ListValue(nodes...))
	}
	for name, spec := range changes {
		t.Run(name, func(t *testing.T) {
			objects := base.Objects()
			for i, object := range objects {
				if object.Kind() == api.OpenShiftCluster {
					objects[i] = object.WithSpec(spec)
				}
			}
			changed := api.NewCatalog(objects)
			for _, capability := range []interface {
				Plan(context.Context, lifecycle.PlanInput) (lifecycle.CapabilityPlan, error)
			}{NewMedia(nil, material.AuthorizedKey), NewInstall(nil)} {
				plans := []lifecycle.CapabilityPlan{}
				for _, catalog := range []api.Catalog{base, changed} {
					plan, err := capability.Plan(context.Background(), lifecycle.PlanInput{
						Verb: reconciliation.Apply, State: compilation.NewState(catalog, catalog, nil),
						Controller: "controller", Context: lifecycle.ContextIdentity{Name: testContext},
					})
					if err != nil {
						t.Fatal(err)
					}
					plans = append(plans, plan)
				}
				if !reflect.DeepEqual(plans[0], plans[1]) {
					t.Fatal("inert declaration changed the capability plan")
				}
			}
		})
	}
}

// An endpoint field the agent installer would not follow refuses derivation
// at its own path, so plan and apply name the field to change.
func TestUnusedEndpointFieldsRefuseAtTheirPath(t *testing.T) {
	base := singleNodeCatalog()
	cluster, _ := base.Find(api.OpenShiftCluster, "sno")
	for _, test := range []struct {
		slot, field string
		value       api.Value
	}{
		{"api", "port", api.IntegerValue("8443")},
		{"api-int", "port", api.IntegerValue("443")},
		{"ingress", "port", api.IntegerValue("6443")},
		{"ingress", "scheme", api.StringValue("http")},
		{"api", "prefixLength", api.IntegerValue("24")},
		{"ingress", "interfaceNetworks", api.StringList("198.51.100.0/24")},
	} {
		path := "$.spec.install.endpoints." + test.slot + "." + test.field
		t.Run(path, func(t *testing.T) {
			objects := base.Objects()
			for i, object := range objects {
				if object.Kind() == api.OpenShiftCluster {
					objects[i] = cluster.WithSpec(cluster.Spec().WithPath(test.value, "install", "endpoints", test.slot, test.field))
				}
			}
			_, _, _, err := Requests(api.NewCatalog(objects), "controller", testContext)
			reported := diagnostics.Of(err)
			if len(reported) != 1 || reported[0].Code != "lifecycle.unsupported" || reported[0].Field != path ||
				reported[0].Object == nil || reported[0].Object.Name != "sno" {
				t.Fatalf("an authored %s refuses with %+v", path, reported)
			}
		})
	}
}

func TestClusterNameAdmissionBoundFitsEveryBlock(t *testing.T) {
	for _, blockID := range []func(string) string{MediaBlockID, InstallBlockID} {
		if block := blockID(strings.Repeat("a", 63)); !api.ValidLexical("name", block) || block == blockID(strings.Repeat("a", 62)+"b") {
			t.Fatalf("a 63-byte cluster name yields %q, not a distinct block identity", block)
		}
	}
	if got, want := InstallBlockID(strings.Repeat("a", 47)), "cluster-install-"+strings.Repeat("a", 47); got != want {
		t.Fatalf("a 63-byte identity moved to %q", got)
	}
}
