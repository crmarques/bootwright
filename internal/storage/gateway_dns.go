package storage

import (
	"fmt"
	"math/big"
	"net/netip"
	"slices"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/machine"
)

func GatewayDNSAddresses(catalog api.Catalog, gateway api.Object) ([]string, error) {
	cluster, found := catalog.Find(api.StorageCluster, gateway.Spec().Get("clusterRef").Text())
	if !found {
		return nil, fmt.Errorf("%s has no retained storage cluster", gateway.Identity())
	}
	ref := gateway.Spec().Get("serviceRef").Text()
	for _, service := range cluster.Spec().Get("ceph", "services").Items() {
		if service.Get("service_type").Text() == "ingress" && service.Get("spec", "backend_service").Text() == ref {
			vip, err := netip.ParsePrefix(service.Get("spec", "virtual_ip").Text())
			if err != nil {
				return nil, fmt.Errorf("%s has no valid ingress virtual IP", gateway.Identity())
			}
			return []string{vip.Addr().Unmap().String()}, nil
		}
	}
	service, found := NativeService(cluster, ref)
	if !found {
		return nil, fmt.Errorf("%s has no retained RGW service", gateway.Identity())
	}
	hosts, issues := ResolveServicePlacement(cluster, service)
	if len(issues) != 0 {
		return nil, fmt.Errorf("%s has unresolved RGW placement", gateway.Identity())
	}
	if count := service.Get("placement", "count"); count.Present() && numeric(count).Cmp(big.NewInt(int64(len(hosts)))) != 0 {
		return nil, fmt.Errorf("%s selects fewer RGW daemons than eligible hosts; use an ingress VIP or place a daemon on every selected host", gateway.Identity())
	}
	networks := cluster.Spec().Get("ceph", "networks", "publicCIDRs").Strings()
	if service.Get("spec", "only_bind_port_on_networks").Bool() && service.Get("networks").Len() > 0 {
		networks = service.Get("networks").Strings()
	}
	addresses := []string{}
	for _, node := range cluster.Spec().Get("ceph", "topology", "nodes").Items() {
		if !slices.Contains(hosts, node.Get("name").Text()) {
			continue
		}
		host, ok := catalog.Find(api.Machine, node.Get("machineRef").Text())
		if !ok {
			return nil, fmt.Errorf("%s has no retained RGW machine", gateway.Identity())
		}
		matched := false
		for _, raw := range machine.IPAddresses(host) {
			ip, err := netip.ParseAddr(raw)
			if err != nil {
				continue
			}
			for _, cidr := range networks {
				prefix, err := netip.ParsePrefix(cidr)
				if err == nil && prefix.Contains(ip) {
					addresses = append(addresses, ip.Unmap().String())
					matched = true
					break
				}
			}
		}
		if !matched {
			return nil, fmt.Errorf("%s has an RGW host without a declared public address", gateway.Identity())
		}
	}
	if len(addresses) == 0 {
		return nil, fmt.Errorf("%s has no declared gateway addresses", gateway.Identity())
	}
	slices.Sort(addresses)
	return slices.Compact(addresses), nil
}
