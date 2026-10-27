package cephcluster

import api "github.com/crmarques/bootwright/api/v1alpha1"

type ConsumerFacts struct {
	Name, Cluster string
	Gateway, TLS  bool
}

func AddonExportFacts(catalog api.Catalog) []ConsumerFacts {
	facts := []ConsumerFacts{}
	for _, export := range catalog.OfKind(api.CephConsumer) {
		gateway, ref := attachedGateway(catalog, export)
		tls := false
		if gateway.Name() != "" {
			cluster, _ := catalog.Find(api.CephCluster, gateway.Spec().Get("clusterRef").Text())
			service, _ := NativeService(cluster, gateway.Spec().Get("serviceRef").Text())
			tls = service.Get("spec", "ssl").Bool()
			for _, front := range cluster.Spec().Get("services").Items() {
				if front.Get("service_type").Text() == "ingress" && front.Get("spec", "backend_service").Text() == NativeServiceName(service) {
					tls = front.Get("spec", "ssl").Bool()
				}
			}
		}
		facts = append(facts, ConsumerFacts{Name: export.Name(), Cluster: export.Spec().Get("clusterRef").Text(), Gateway: ref != "", TLS: tls})
	}
	return facts
}
