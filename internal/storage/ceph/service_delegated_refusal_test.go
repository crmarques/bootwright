package ceph

import (
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

func TestDelegatedFamiliesStillRefuseAtPlan(t *testing.T) {
	for name, service := range map[string]api.Value{
		"rgw.objects":         requestMap("service_type", "rgw", "service_id", "objects", "placement", requestMap("label", "mon", "count", 2)),
		"ingress.rgw.objects": requestMap("service_type", "ingress", "service_id", "rgw.objects", "placement", requestMap("label", "mon", "count", 2), "spec", requestMap("backend_service", "rgw.objects", "virtual_ip", "192.0.2.50/24")),
		"nfs.files":           requestMap("service_type", "nfs", "service_id", "files", "placement", requestMap("label", "mon", "count", 2)),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := clusterRequestWith(t, func(services []api.Value) []api.Value { return append(services, service) })
			refusedWith(t, err, "lifecycle.unsupported", "a selected service has no core lifecycle", "use only MON, MGR, OSD and MDS services until its feature item is delivered")
		})
	}
}
