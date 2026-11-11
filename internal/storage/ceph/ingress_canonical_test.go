package ceph

import (
	"context"
	"encoding/json"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/canonicaljson"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

func TestIngressNetworkSelectionsFreezeThroughCapabilityPlanning(t *testing.T) {
	networks := api.StringList("192.0.2.0/24")
	rgwIngress := ingressService("rgw.objects").WithPath(networks, "spec", "virtual_interface_networks")
	nfsIngress := requestMap("service_type", "ingress", "service_id", "nfs.files", "placement", requestMap("label", "mon", "count", 2),
		"spec", requestMap("backend_service", "nfs.files", "virtual_ip", "192.0.2.50/24", "virtual_interface_networks", networks))
	for _, test := range []struct {
		name    string
		kind    api.Kind
		catalog api.Catalog
	}{
		{"rgw", api.CephObjectGateway, delegatedCatalog([]api.Value{rgwService("objects"), rgwIngress}, nil, objectGateway("objects"))},
		{"nfs", api.CephCluster, delegatedCatalog([]api.Value{nfsService("files"), nfsIngress}, nil)},
		{"management", api.CephCluster, delegatedCatalog([]api.Value{mgmtGateway(requestMap("port", 9443, "virtual_ip", "192.0.2.60")), mgmtIngress("192.0.2.60", "virtual_interface_networks", networks)}, nil)},
	} {
		t.Run(test.name, func(t *testing.T) {
			objects := test.catalog.Objects()
			for i, object := range objects {
				if object.Kind() == api.CephCluster {
					objects[i] = object.WithSpec(withCorePair(object.Spec()))
				}
			}
			catalog := api.NewCatalog(objects)
			state := compilation.NewState(catalog, catalog, nil)
			plan, err := New(test.kind, nil).Plan(context.Background(), lifecycle.PlanInput{
				Verb: reconciliation.Apply, State: state, Context: lifecycle.ContextIdentity{Name: "synthetic"}, Controller: "controller",
			})
			if err != nil {
				t.Fatal(diagnostics.Of(err))
			}
			found := false
			for _, block := range plan.Definitions {
				request, err := DecodeRequest(block.Request)
				if err != nil {
					t.Fatal(diagnostics.Of(err))
				}
				var documents []json.RawMessage
				if request.Gateway != nil {
					documents = request.Gateway.Documents
				}
				if request.Service != nil {
					documents = request.Service.Documents
				}
				for _, document := range documents {
					if err := canonicaljson.ProveObject(document); err != nil {
						t.Fatal(err)
					}
					var service struct {
						Type string `json:"service_type"`
						Spec struct {
							Networks []string `json:"virtual_interface_networks"`
							VIP      string   `json:"virtual_ip"`
						} `json:"spec"`
					}
					if err := json.Unmarshal(document, &service); err != nil {
						t.Fatal(err)
					}
					if service.Type == "ingress" {
						if len(service.Spec.Networks) != 1 || service.Spec.Networks[0] != "192.0.2.0/24" || service.Spec.VIP == "" {
							t.Fatalf("ingress lost its network selection or VIP: %+v", service.Spec)
						}
						found = true
					}
				}
			}
			if !found {
				t.Fatal("the plan contains no ingress network selection")
			}
		})
	}
}
