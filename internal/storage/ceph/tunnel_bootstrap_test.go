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
		{"verified-tunnel", "ineligible bootstrap", []string{"node-2", "node-3"}, true},
		{"verified-tunnel", "eligible bootstrap", []string{"node-1", "node-2"}, false},
		{"native", "native placement retains its contract", []string{"node-2", "node-3"}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			catalog := poolRequestCatalog(3, 3)
			cluster, _ := catalog.Find(api.StorageCluster, "example")
			services := cluster.Spec().Get("ceph", "services").Items()
			for i, service := range services {
				if service.Get("service_type").Text() == "mgr" {
					services[i] = service.With("placement", requestMap("hosts", api.StringList(test.hosts...), "count", 2))
				}
			}
			spec := cluster.Spec().WithPath(api.ListValue(services...), "ceph", "services")
			spec = spec.WithPath(api.StringValue(test.transport), "ceph", "cephadm", "clusterSSH", "transport")
			cluster = cluster.WithSpec(spec)
			objects := catalog.Objects()
			for i, object := range objects {
				if object.Identity() == cluster.Identity() {
					objects[i] = cluster
				}
			}
			pool := api.NewObject(api.StoragePool, "volumes", api.Value{}, requestMap("clusterRef", "example", "type", "replicated", "role", "rbd", "replicated", requestMap("size", 2, "minSize", 2)))
			objects = append(objects, pool)
			catalog = api.NewCatalog(objects)
			client := api.NewObject(api.StorageClient, "consumer", api.Value{}, requestMap("clusterRef", "example", "poolRef", "volumes", "access", "read-write"))
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
