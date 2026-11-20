package cephcluster

import (
	"math/big"
	"net/netip"
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

func (a *admission) stretchServices(ceph api.Value, sites []string, tiebreaker string) {
	siteOf, nodes := map[string]string{}, map[string]api.Value{}
	for _, node := range ceph.Get("topology", "nodes").Items() {
		siteOf[textOf(node, "name")], nodes[textOf(node, "name")] = textOf(node, "site"), node
	}
	services := ceph.Get("services").Items()
	fronts := map[string][]int{}
	placedAt := map[int]map[string]int{}
	reachedBy := map[int]int{}
	placedHosts := map[int][]string{}
	for i, service := range services {
		field, kind := indexed("$.spec.services", i), textOf(service, "service_type")
		if kind == "nfs" {
			a.invalid(field+".service_type", "an NFS service on a stretch cluster has no qualified profile: cephadm creates .nfs on the default rule and its per-site front door is unqualified", "remove this nfs service, or declare it on a cluster without spec.topology.stretch; B936 delivers NFS and SMB stretch profiles")
			continue
		}
		names, issues := a.placements().resolve(service)
		if len(issues) != 0 {
			continue
		}
		perSite := map[string]int{}
		for _, name := range names {
			perSite[siteOf[name]]++
		}
		placedAt[i], placedHosts[i] = perSite, names
		switch {
		case kind == "mon":
			a.stretchMonitors(service, field, len(names), perSite, sites, slices.Contains(names, tiebreaker))
		case slices.Contains(stretchHAServices, kind):
			a.stretchHAService(service, field, kind, perSite, sites)
		case kind == "ingress":
			backend, ok := NativeService(a.object, textOf(service, "spec", "backend_service"))
			if !ok || textOf(backend, "service_type") != "rgw" {
				continue
			}
			for _, site := range sites {
				if perSite[site] > 0 {
					reachedBy[i]++
				}
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
			names, issues := a.placements().resolve(service)
			if count := service.Get("placement", "count"); len(issues) == 0 && count.Present() && numeric(count).Cmp(big.NewInt(int64(len(names)))) != 0 {
				a.invalid(field+".placement.count", "an rgw service of a stretch cluster without ingresses is reached at every host's address, so every placed host runs a daemon", "omit placement.count or set it to the number of placed hosts, or front this service with one ingress over every data site or one per data site")
			}
		case len(ingresses) == 1:
			if reachedBy[ingresses[0]] != len(sites) {
				a.invalid(indexed("$.spec.services", ingresses[0])+".placement", "rgw service "+name+" of a stretch cluster has one ingress, and its hosts stand at one data site, so its virtual IP is lost with that site", "place this ingress on hosts of every data site ("+strings.Join(sites, ", ")+") on a public network stretched across them, front "+name+" with one ingress per data site, or with none behind an operator-owned load balancer or DNS")
				continue
			}
			a.stretchedIngress(services[ingresses[0]], indexed("$.spec.services", ingresses[0]), placedHosts[ingresses[0]], nodes)
		case len(sites) == 2 && len(ingresses) == 2:
			for _, at := range ingresses {
				if reachedBy[at] > 1 {
					a.invalid(indexed("$.spec.services", at)+".placement", "an ingress whose hosts span both data sites fronts rgw service "+name+" beside another ingress; per-site ingresses each stand at one data site", "place each ingress of "+name+" only on hosts of one data site, or front it with this one ingress alone over both")
				}
			}
			a.stretchIngressPair(services, ingresses, placedAt, sites)
		default:
			a.invalid(indexed("$.spec.services", ingresses[0])+".spec.backend_service", "rgw service "+name+" of a stretch cluster has "+strconv.Itoa(len(ingresses))+" ingresses, neither one over every data site nor one per data site", "front "+name+" with one ingress over every data site on a stretched public network, with exactly one ingress per data site ("+strings.Join(sites, ", ")+"), or with none behind an operator-owned load balancer or DNS")
		}
	}
}

func (a *admission) stretchMonitors(service api.Value, field string, total int, perSite map[string]int, sites []string, arbiter bool) {
	if count := service.Get("placement", "count"); count.Present() && numeric(count).Cmp(big.NewInt(int64(total))) != 0 {
		a.invalid(field+".placement.count", "stretch MON placement must deploy on every selected host to guarantee the tiebreaker and site quorum", "omit placement.count or set it to the number of selected hosts")
	}
	if len(sites) != 2 || !arbiter || perSite[sites[0]] == 0 || perSite[sites[1]] == 0 {
		return
	}
	for _, site := range sites {
		if total-perSite[site] <= total/2 {
			a.invalid(field+".placement", "the stretch MON service loses its majority when data site "+site+" is unavailable", "select an equal number of MON hosts at both data sites and the one tiebreaker")
			return
		}
	}
}

func (a *admission) stretchedIngress(service api.Value, field string, hosts []string, nodes map[string]api.Value) {
	if count := service.Get("placement", "count"); count.Present() && numeric(count).Cmp(big.NewInt(int64(len(hosts)))) != 0 {
		a.invalid(field+".placement.count", "one ingress over both data sites runs keepalived on every placed host, so its virtual IP has a holder at each site; a count lets cephadm place every one at one site", "omit placement.count on this ingress or set it to the number of placed hosts ("+strconv.Itoa(len(hosts))+")")
	}
	vip, ok := ingressAddress(service)
	if !ok {
		return
	}
	interfaceNetworks := service.Get("spec").Has("virtual_interface_networks")
	for _, name := range hosts {
		node := nodes[name]
		m, found := a.catalog.Find(api.Machine, textOf(node, "machineRef"))
		if !found {
			continue
		}
		prefixes := machinePrefixes(m, a.catalog)
		if slices.ContainsFunc(prefixes, func(prefix netip.Prefix) bool { return prefix.Masked().Contains(vip) }) || len(prefixes) != 0 && !interfaceNetworks {
			continue
		}
		a.invalid(field+".spec.virtual_ip", "ingress host "+NativeHostname(node)+" (Machine "+m.Name()+") of one ingress over both data sites holds no address in the virtual IP's subnet, so the virtual IP cannot move to it on a layer 2 stretched across the sites",
			"declare an address in the virtual IP's subnet on Machine "+m.Name()+", or front the rgw service with one ingress per data site")
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
