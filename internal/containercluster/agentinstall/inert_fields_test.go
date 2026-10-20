package agentinstall

import (
	"context"
	"reflect"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets/material"
)

func TestAdmittedInertFieldsDoNotChangePlans(t *testing.T) {
	base := singleNodeCatalog()
	cluster, _ := base.Find(api.ContainerCluster, "sno")
	changes := map[string]api.Value{
		"release channel": cluster.Spec().WithPath(api.StringValue("fast-4.21"), "distribution", "release", "channel"),
	}
	for _, slot := range []string{"api", "api-int", "ingress"} {
		for field, value := range map[string]api.Value{
			"port": api.IntegerValue("8443"), "scheme": api.StringValue("http"),
			"prefixLength": api.IntegerValue("24"), "interfaceNetworks": api.StringList("198.51.100.0/24"),
		} {
			changes[slot+" "+field] = cluster.Spec().WithPath(value, "install", "endpoints", slot, field)
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
				if object.Kind() == api.ContainerCluster {
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

func TestClusterNameAdmissionBoundFitsEveryBlock(t *testing.T) {
	for _, blockID := range []func(string) string{MediaBlockID, InstallBlockID} {
		if !api.ValidLexical("name", blockID(strings.Repeat("a", 47))) {
			t.Fatal("admitted cluster name exceeds a block identity bound")
		}
	}
	if api.ValidLexical("name", InstallBlockID(strings.Repeat("a", 48))) {
		t.Fatal("installation block no longer establishes the 47-byte admission bound")
	}
}
