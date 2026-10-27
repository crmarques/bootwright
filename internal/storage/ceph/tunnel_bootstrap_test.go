package ceph

import (
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

func TestTunnelBootstrapEligibilityIsAdmittedBeforeClusterAndChildRequests(t *testing.T) {
	for _, test := range []struct {
		transport, name string
		hosts           []string
		refused         bool
	}{
		{"verifiedTunnel", "ineligible bootstrap", []string{"node-2", "node-3"}, true},
		{"verifiedTunnel", "eligible bootstrap", []string{"node-1", "node-2"}, false},
		{"native", "native placement retains its contract", []string{"node-2", "node-3"}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			catalog := poolRequestCatalog(3, 3)
			cluster, _ := catalog.Find(api.CephCluster, "example")
			services := cluster.Spec().Get("services").Items()
			for i, service := range services {
				if service.Get("service_type").Text() == "mgr" {
					services[i] = service.With("placement", requestMap("hosts", api.StringList(test.hosts...), "count", 2))
				}
			}
			spec := cluster.Spec().WithPath(api.ListValue(services...), "services")
			spec = spec.WithPath(api.StringValue(test.transport), "cephadm", "clusterSSH", "transport")
			cluster = cluster.WithSpec(spec)
			objects := catalog.Objects()
			for i, object := range objects {
				if object.Identity() == cluster.Identity() {
					objects[i] = cluster
				}
			}
			pool := api.NewObject(api.CephPool, "volumes", api.Value{}, requestMap("clusterRef", "example", "role", "rbd", "replicated", requestMap("size", 2, "minSize", 2)))
			objects = append(objects, pool)
			catalog = api.NewCatalog(objects)
			client := api.NewObject(api.CephClient, "consumer", api.Value{}, requestMap("clusterRef", "example", "poolRef", "volumes", "access", "readWrite"))
			for _, object := range []api.Object{cluster, pool, client} {
				request, err := New(object.Kind(), nil).request(catalog, object, "synthetic")
				if !test.refused {
					if err != nil || request.ClusterSSHTransport != test.transport {
						t.Fatalf("%s request=%+v err=%v", object.Identity(), request, err)
					}
					continue
				}
				issues := diagnostics.Of(err)
				if len(issues) != 1 || issues[0].Code != "lifecycle.unsupported" || !strings.Contains(issues[0].Message, "eligible MGR host") || issues[0].Remediation == "" {
					t.Fatalf("%s lacks actionable bootstrap placement refusal: %v %+v", object.Identity(), err, issues)
				}
				if request.Version != "" || len(request.Services) != 0 {
					t.Fatalf("%s published a usable request after placement refusal: %+v", object.Identity(), request)
				}
			}
		})
	}
}
