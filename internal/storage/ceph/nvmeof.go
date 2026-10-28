package ceph

import (
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/netip"
	"slices"
	"strings"
	"time"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/storage/cephcluster"
)

type NVMeOFEndpoint struct {
	Address string `json:"address"`
	Host    string `json:"host"`
	Port    int64  `json:"port"`
}

type NVMeOFNamespace struct {
	ID    int64    `json:"id"`
	Image RBDImage `json:"image"`
}

type NVMeOFSubsystem struct {
	Gateways   []NVMeOFEndpoint  `json:"gateways"`
	Group      string            `json:"group"`
	Hosts      []string          `json:"hosts"`
	Listeners  []NVMeOFEndpoint  `json:"listeners"`
	MTLS       string            `json:"mtls"`
	Name       string            `json:"name"`
	Namespaces []NVMeOFNamespace `json:"namespaces"`
	NQN        string            `json:"nqn"`
	Pool       string            `json:"pool"`
	Service    string            `json:"service"`
}

type nvmeofServiceSpec struct {
	DiscoveryPort int64  `json:"discovery_port"`
	EnableAuth    bool   `json:"enable_auth,omitempty"`
	EnableMTLS    bool   `json:"enable_mtls,omitempty"`
	Group         string `json:"group"`
	Pool          string `json:"pool"`
	Port          int64  `json:"port"`
	Transports    string `json:"transports"`
}

func nvmeofSpecOf(spec api.Value) any {
	port, ok := spec.Get("port").Int64()
	if !ok {
		port = 5500
	}
	discoveryPort, ok := spec.Get("discovery_port").Int64()
	if !ok {
		discoveryPort = 8009
	}
	return nvmeofServiceSpec{DiscoveryPort: discoveryPort, Transports: "tcp", Pool: spec.Get("pool").Text(), Group: spec.Get("group").Text(), Port: port,
		EnableAuth: spec.Get("enable_auth").Bool(), EnableMTLS: !spec.Get("enable_auth").Bool()}
}

func nvmeofUnits(cluster api.Object) ([]ServiceUnit, error) {
	units := []ServiceUnit{}
	for _, service := range cluster.Spec().Get("services").Items() {
		if service.Get("service_type").Text() != "nvmeof" {
			continue
		}
		document, err := unitDocumentOf(cluster, service, nvmeofSpecOf)
		if err != nil {
			return nil, err
		}
		unit := ServiceUnit{ID: service.Get("service_id").Text(), Type: "nvmeof", Documents: []json.RawMessage{document}}
		for _, binding := range gatewayBindings(cluster, cephcluster.NativeServiceName(service)) {
			unit.Bindings = append(unit.Bindings, ServiceBinding(binding))
		}
		units = append(units, unit)
	}
	slices.SortFunc(units, func(a, b ServiceUnit) int { return strings.Compare(a.ID, b.ID) })
	return units, nil
}

func projectNVMeOFPools(catalog api.Catalog, cluster api.Object, requests []Request) error {
	for i := range requests {
		unit := requests[i].Service
		if unit == nil || unit.Type != "nvmeof" {
			continue
		}
		service, err := nvmeofService(cluster, unit.ID)
		if err != nil {
			return err
		}
		pool, err := nvmeofConfigPool(catalog, cluster, unit.ID, service)
		if err != nil {
			return err
		}
		service = service.WithPath(api.StringValue(cephcluster.PoolName(pool)), "spec", "pool")
		document, err := unitDocumentOf(cluster, service, nvmeofSpecOf)
		if err != nil {
			return err
		}
		unit.Documents = []json.RawMessage{document}
	}
	return nil
}

func nvmeofPoolName(cluster api.Object, id string, service api.Value) (string, error) {
	name := service.Get("spec", "pool").Text()
	if name == "" {
		return "", failure("api.required", "the nvmeof service "+id+" of CephCluster "+cluster.Name()+" declares no spec.pool, the CephPool that keeps its gateway state", "set spec.pool of that nvmeof service to a replicated CephPool with role rbd of this cluster")
	}
	return name, nil
}

func nvmeofConfigPool(catalog api.Catalog, cluster api.Object, id string, service api.Value) (api.Object, error) {
	name, err := nvmeofPoolName(cluster, id, service)
	if err != nil {
		return api.Object{}, err
	}
	pool, found := catalog.Find(api.CephPool, name)
	if !found {
		return api.Object{}, failure("api.reference", "the nvmeof service "+id+" of CephCluster "+cluster.Name()+" names spec.pool "+name+", which is not a declared CephPool", "declare a replicated CephPool "+name+" with role rbd for this cluster, or set spec.pool to the name of one")
	}
	return pool, nil
}

func nvmeofService(cluster api.Object, id string) (api.Value, error) {
	name := "nvmeof." + id
	service, found := cephcluster.NativeService(cluster, name)
	if !found {
		return api.Value{}, failure("api.invariant", "CephCluster "+cluster.Name()+" declares no single NVMe-oF service "+name+", the name the service_id of its nvmeof service in spec.services derives",
			"give each nvmeof service in spec.services of CephCluster "+cluster.Name()+" its own non-empty service_id")
	}
	return service, nil
}

func nvmeofDependencies(cluster api.Object, unit ServiceUnit) ([]string, error) {
	service, err := nvmeofService(cluster, unit.ID)
	if err != nil {
		return nil, err
	}
	name, err := nvmeofPoolName(cluster, unit.ID, service)
	if err != nil {
		return nil, err
	}
	return []string{reconciliation.BlockIdentity("ceph-pool-", name)}, nil
}

func nvmeofRequest(catalog api.Catalog, object api.Object, contextName string) (Request, error) {
	spec := object.Spec()
	cluster, _ := catalog.Find(api.CephCluster, spec.Get("clusterRef").Text())
	request, err := baseRequest(catalog, cluster, contextName)
	if err != nil {
		return Request{}, err
	}
	service, found := cephcluster.NativeService(cluster, spec.Get("serviceRef").Text())
	if !found {
		return Request{}, failure("api.invariant", "NVMe-oF requires a declared gateway service", "set spec.serviceRef to the qualified native service name")
	}
	native := service.Get("spec")
	configPool, err := nvmeofConfigPool(catalog, cluster, service.Get("service_id").Text(), service)
	if err != nil {
		return Request{}, err
	}
	frozen := &NVMeOFSubsystem{Name: object.Name(), Service: service.Get("service_id").Text(), Group: native.Get("group").Text(), Pool: cephcluster.PoolName(configPool),
		MTLS: "enable_auth", NQN: spec.Get("nqn").Text(), Hosts: []string{}, Gateways: []NVMeOFEndpoint{}, Listeners: []NVMeOFEndpoint{}, Namespaces: []NVMeOFNamespace{}}
	if !native.Get("enable_auth").Bool() {
		frozen.MTLS = "enable_mtls"
	}
	port, ok := native.Get("port").Int64()
	if !ok {
		port = 5500
	}
	names, err := serviceHosts(cluster, service)
	if err != nil {
		return Request{}, err
	}
	for _, name := range names {
		for _, host := range request.Hosts {
			if host.Name == name {
				frozen.Gateways = append(frozen.Gateways, NVMeOFEndpoint{Host: name, Address: host.Address, Port: port})
			}
		}
	}
	for _, listener := range spec.Get("listeners").Items() {
		host := listener.Get("host").Text()
		for _, node := range cluster.Spec().Get("topology", "nodes").Items() {
			if node.Get("name").Text() == host {
				host = cephcluster.NativeHostname(node)
				break
			}
		}
		port, ok := listener.Get("trsvcid").Int64()
		if !ok {
			port = 4420
		}
		addressText := ""
		for _, node := range request.Hosts {
			if node.Name == host {
				addressText = node.Address
				break
			}
		}
		address, parseErr := netip.ParseAddr(addressText)
		if parseErr != nil {
			return Request{}, failure("api.invariant", "NVMe-oF listener requires an IP address", "set spec.topology.nodes[].addr on the CephCluster to the listener host's registered IP address")
		}
		frozen.Listeners = append(frozen.Listeners, NVMeOFEndpoint{Host: host, Address: address.String(), Port: port})
	}
	for _, host := range spec.Get("hosts").Items() {
		frozen.Hosts = append(frozen.Hosts, host.Text())
	}
	slices.Sort(frozen.Hosts)
	for _, namespace := range spec.Get("namespaces").Items() {
		image, _ := catalog.Find(api.CephRBDImage, namespace.Get("imageRef").Text())
		backing, err := rbdImageRequest(catalog, image, contextName)
		if err != nil {
			return Request{}, err
		}
		id, _ := namespace.Get("nsid").Int64()
		frozen.Namespaces = append(frozen.Namespaces, NVMeOFNamespace{ID: id, Image: *backing.RBD})
	}
	slices.SortFunc(frozen.Namespaces, func(a, b NVMeOFNamespace) int { return cmp.Compare(a.ID, b.ID) })
	less := func(a, b NVMeOFEndpoint) int {
		return cmp.Or(strings.Compare(a.Host, b.Host), strings.Compare(a.Address, b.Address), cmp.Compare(a.Port, b.Port))
	}
	slices.SortFunc(frozen.Gateways, less)
	slices.SortFunc(frozen.Listeners, less)
	request.Action, request.NVMeOF = "nvmeof", frozen
	return request, nil
}

func nvmeofSerial(r Request) string {
	if r.NVMeOF == nil {
		return ""
	}
	data, _ := json.Marshal(map[string]string{"owner": r.Owner, "fsid": r.FSID, "name": r.NVMeOF.Name, "nqn": r.NVMeOF.NQN})
	sum := sha256.Sum256(data)
	return "bw" + hex.EncodeToString(sum[:])[:18]
}

func nvmeofPhaseBudget(r Request, apply bool) int {
	n := r.NVMeOF
	gateways, namespaces, listeners, hosts := len(n.Gateways), len(n.Namespaces), len(n.Listeners), len(n.Hosts)
	dataPools := 0
	for _, namespace := range n.Namespaces {
		if namespace.Image.DataPool != "" {
			dataPools++
		}
	}
	scope := 5 + gateways + 3*namespaces + dataPools
	if apply {
		return 60*(scope+10+namespaces+listeners+hosts) + 30*(2+namespaces+listeners+hosts) + 60
	}
	return 60*(scope+4+gateways) + 2*listeners + 60
}

func nvmeofDeadline(r Request, operation string) (time.Duration, error) {
	observation := 660 + nvmeofPhaseBudget(r, false) + 20
	apply := 660 + nvmeofPhaseBudget(r, true) + 20 + observation + 900 + 60
	if time.Duration(apply)*time.Second > lifecycle.MaxDeadline {
		return 0, failure("lifecycle.unsupported", fmt.Sprintf("CephNVMeoFSubsystem %s needs a bounded apply deadline of %s, past the 6h0m0s adapter limit", r.NVMeOF.Name, time.Duration(apply)*time.Second), "declare fewer namespaces or initiator hosts in this subsystem")
	}
	if operation == "apply" {
		return time.Duration(apply) * time.Second, nil
	}
	return time.Duration(observation+60) * time.Second, nil
}
