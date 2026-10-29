package cephcluster

import (
	"math/big"
	"slices"
	"strconv"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

var stretchHAServices = []string{"mgr", "mds", "rgw"}

func stretchedChildRefusal(kind api.Kind) (message, remedy string, refused bool) {
	if kind != api.CephNFSExport && kind != api.CephSMBResource {
		return "", "", false
	}
	return "NFS and SMB on a stretch cluster have no qualified profile: cephadm creates their pools on the default rule and their per-site front doors are unqualified",
		"remove this " + string(kind) + " or declare it on a cluster without spec.topology.stretch; B936 delivers NFS and SMB stretch profiles", true
}

func (a *admission) stretchServices(ceph api.Value, sites []string) {
	siteOf := map[string]string{}
	for _, node := range ceph.Get("topology", "nodes").Items() {
		siteOf[textOf(node, "name")] = textOf(node, "site")
	}
	services := ceph.Get("services").Items()
	fronts := map[string][]int{}
	placedAt := map[int]map[string]int{}
	for i, service := range services {
		field, kind := indexed("$.spec.services", i), textOf(service, "service_type")
		if kind == "nfs" {
			a.invalid(field+".service_type", "an NFS service on a stretch cluster has no qualified profile: cephadm creates .nfs on the default rule and its per-site front door is unqualified", "remove this nfs service, or declare it on a cluster without spec.topology.stretch; B936 delivers NFS and SMB stretch profiles")
			continue
		}
		names, issues := ResolveServicePlacement(a.object, service)
		if len(issues) != 0 {
			continue
		}
		perSite := map[string]int{}
		for _, name := range names {
			perSite[siteOf[name]]++
		}
		placedAt[i] = perSite
		switch {
		case slices.Contains(stretchHAServices, kind):
			a.stretchHAService(service, field, kind, perSite, sites)
		case kind == "ingress":
			backend, ok := NativeService(a.object, textOf(service, "spec", "backend_service"))
			if !ok || textOf(backend, "service_type") != "rgw" {
				continue
			}
			reached := 0
			for _, site := range sites {
				if perSite[site] > 0 {
					reached++
				}
			}
			if reached > 1 {
				a.invalid(field+".placement", "an ingress whose hosts span both data sites floats one virtual IP across sites, which does not survive the loss of a site", "place this ingress only on hosts of one data site, by hosts or a per-site label, and front an rgw service with one ingress per data site")
			}
			fronts[NativeServiceName(backend)] = append(fronts[NativeServiceName(backend)], i)
		}
	}
	for i, service := range services {
		if textOf(service, "service_type") != "rgw" {
			continue
		}
		field, name := indexed("$.spec.services", i), NativeServiceName(service)
		ingresses := fronts[name]
		switch {
		case len(ingresses) == 0:
			names, issues := ResolveServicePlacement(a.object, service)
			if count := service.Get("placement", "count"); len(issues) == 0 && count.Present() && numeric(count).Cmp(big.NewInt(int64(len(names)))) != 0 {
				a.invalid(field+".placement.count", "an rgw service of a stretch cluster without ingresses is reached at every host's address, so every placed host runs a daemon", "omit placement.count or set it to the number of placed hosts, or front this service with one ingress per data site")
			}
		case len(sites) == 2 && len(ingresses) == 2:
			a.stretchIngressPair(services, ingresses, placedAt, sites)
		default:
			a.invalid(indexed("$.spec.services", ingresses[0])+".spec.backend_service", "rgw service "+name+" of a stretch cluster has "+strconv.Itoa(len(ingresses))+" ingresses, not one per data site; a single front door is lost with its site", "front "+name+" with exactly one ingress per data site ("+strings.Join(sites, ", ")+"), or with none behind an operator-owned load balancer or DNS")
		}
	}
}

func (a *admission) stretchHAService(service api.Value, field, kind string, perSite map[string]int, sites []string) {
	widest := 0
	for _, site := range sites {
		if perSite[site] == 0 {
			a.invalid(field+".placement", "the "+kind+" service of a stretch cluster places no host at data site "+site+", so it does not survive the loss of the other site", "include hosts of data site "+site+" in this service's placement, by hosts, label or host_pattern")
		}
		widest = max(widest, perSite[site])
	}
	if count := service.Get("placement", "count"); count.Present() && !service.Has("placement", "count_per_host") && numeric(count).Cmp(big.NewInt(int64(widest))) <= 0 {
		a.invalid(field+".placement.count", "the "+kind+" service's count "+numeric(count).String()+" lets cephadm place every daemon at one data site, which holds "+strconv.Itoa(widest)+" of its hosts", "raise placement.count above "+strconv.Itoa(widest)+", or omit it so every placed host runs a daemon")
	}
}

func (a *admission) stretchIngressPair(services []api.Value, ingresses []int, placedAt map[int]map[string]int, sites []string) {
	held := map[string]bool{}
	for _, at := range ingresses {
		for _, site := range sites {
			if placedAt[at][site] > 0 {
				held[site] = true
			}
		}
	}
	first, field := services[ingresses[0]], indexed("$.spec.services", ingresses[1])
	if len(held) != len(sites) {
		a.invalid(field+".placement", "the ingresses of an rgw service of a stretch cluster are placed one per data site", "place each of these ingresses on hosts of a different data site ("+strings.Join(sites, ", ")+")")
	}
	for _, at := range ingresses[1:] {
		other := services[at]
		for _, member := range []string{"frontend_port", "ssl"} {
			if !first.Get("spec", member).Equal(other.Get("spec", member)) {
				a.invalid(indexed("$.spec.services", at)+".spec."+member, "the per-site ingresses of one rgw service serve one endpoint FQDN, so they share spec."+member, "set spec."+member+" equal on every ingress of this rgw service")
			}
		}
		if !slices.Equal(boundSecrets(a.object, NativeServiceName(first)), boundSecrets(a.object, NativeServiceName(other))) {
			a.invalid(indexed("$.spec.services", at)+".spec.ssl", "the per-site ingresses of one rgw service present one certificate for its endpoint FQDN, so they bind the same Secret", "bind /spec/ssl_cert and /spec/ssl_key of every ingress of this rgw service to the same Secret")
		}
	}
}

func boundSecrets(cluster api.Object, service string) []string {
	out := []string{}
	for _, binding := range cluster.Spec().Get("serviceBindings").Items() {
		if textOf(binding, "serviceRef") != service {
			continue
		}
		for _, secret := range binding.Get("secrets").Items() {
			out = append(out, textOf(secret, "path")+"="+textOf(secret, "secretRef"))
		}
	}
	slices.Sort(out)
	return out
}
