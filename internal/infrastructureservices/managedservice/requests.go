package managedservice

import (
	"bytes"
	"encoding/json"
	machineref "github.com/crmarques/bootwright/internal/machine"
	"slices"

	"github.com/crmarques/bootwright/internal/machine"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

// Record is one name this service answers with, and the addresses it answers.
// A subtree record answers for every name beneath it as well, which is what an
// ingress wildcard means; every other record answers its exact name alone.
type Record struct {
	Addresses []string `json:"addresses"`
	Name      string   `json:"name"`
	Subtree   bool     `json:"subtree,omitempty"`
}

// Request is the complete frozen intent for one managed network service. Its
// fields are declared in the canonical key order the plan digest requires, and
// it carries no secret value. The kind-specific arrays are omitted when the
// kind does not use them, so a request means exactly one thing.
type Request struct {
	BindAddress  string               `json:"bindAddress"`
	Clients      []string             `json:"clients,omitempty"`
	ContentRoot  string               `json:"contentRoot"`
	Egress       Egress               `json:"egress"`
	Endpoints    []Endpoint           `json:"endpoints"`
	Forwarders   []string             `json:"forwarders,omitempty"`
	Identity     Identity             `json:"identity"`
	Image        string               `json:"image"`
	IngressHosts []string             `json:"ingressHosts,omitempty"`
	Kind         string               `json:"kind"`
	Placement    machineref.Placement `json:"placement"`
	Port         int                  `json:"port"`
	Records      []Record             `json:"records,omitempty"`
	Sources      []string             `json:"sources,omitempty"`
	Unit         string               `json:"unit"`
	Version      string               `json:"version"`
}

// Canonical encodes the request exactly as the plan digest and the adapter
// both consume it. It refuses anything a reader could interpret differently.
func (r Request) Canonical() ([]byte, error) {
	data, err := json.Marshal(r)
	if err != nil {
		return nil, Refusal("lifecycle.state", "the managed service request cannot be encoded", "")
	}
	var probe map[string]any
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&probe); err != nil {
		return nil, Refusal("lifecycle.state", "the managed service request cannot be decoded", "")
	}
	canonical, err := json.Marshal(probe)
	if err != nil || !bytes.Equal(data, canonical) {
		return nil, Refusal("lifecycle.state", "the managed service request is not canonically ordered", "")
	}
	return data, nil
}

func DecodeRequest(data []byte, version string) (Request, error) {
	var request Request
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		return Request{}, Refusal("lifecycle.state", "the frozen managed service request is malformed", "")
	}
	if decoder.More() {
		return Request{}, Refusal("lifecycle.state", "the frozen managed service request contains trailing data", "")
	}
	if request.Version != version {
		return Request{}, Refusal("lifecycle.state", "the frozen managed service request has an unsupported version", "")
	}
	canonical, err := request.Canonical()
	if err != nil {
		return Request{}, err
	}
	if !bytes.Equal(canonical, data) {
		return Request{}, Refusal("lifecycle.state", "the frozen managed service request is not canonical", "")
	}
	return request, nil
}

// ReservationKeys are the exclusive host resources this request claims.
func (r Request) ReservationKeys() []string {
	return ReservationKeys(r.Unit, r.ContentRoot, SocketKeys(r.BindAddress, r.Port, r.Endpoints))
}

// ProbeTargets is every address readiness must answer on. A wildcard bind is
// proved through each declared endpoint, because that is what a consumer uses.
func (r Request) ProbeTargets() []string {
	if r.BindAddress != "0.0.0.0" && r.BindAddress != "::" {
		return []string{r.BindAddress}
	}
	addresses := make([]string, 0, len(r.Endpoints))
	for _, endpoint := range r.Endpoints {
		if !slices.Contains(addresses, endpoint.Address) {
			addresses = append(addresses, endpoint.Address)
		}
	}
	slices.Sort(addresses)
	return addresses
}

// Clients is the deterministic set of addresses a managed service answers.
// Loopback is always permitted; everything else comes from the selected
// machine networks and the addresses the retained Machines declare.
func Clients(catalog api.Catalog) []string {
	clients := []string{"127.0.0.1/32", "::1/128"}
	for _, config := range catalog.OfKind(api.NetworkConfig) {
		for _, network := range config.Spec().Get("machineNetwork").Items() {
			if cidr := network.Get("cidr").Text(); cidr != "" && !slices.Contains(clients, cidr) {
				clients = append(clients, cidr)
			}
		}
	}
	for _, machine := range catalog.OfKind(api.Machine) {
		for _, address := range machine.Spec().Get("network", "addresses").Items() {
			cidr := hostPrefix(address.Get("address").Text())
			if cidr != "" && !slices.Contains(clients, cidr) {
				clients = append(clients, cidr)
			}
		}
	}
	slices.Sort(clients)
	return slices.Compact(clients)
}

// MachineRecords names every retained Machine by its effective fully qualified
// name and the addresses it declares, so a managed resolver answers the graph
// it was planned from.
func MachineRecords(catalog api.Catalog) []Record {
	var records []Record
	for _, machine := range catalog.OfKind(api.Machine) {
		name, addresses := "", []string{}
		for _, address := range machine.Spec().Get("network", "addresses").Items() {
			value := address.Get("address").Text()
			if address.Get("name").Text() == "fqdn" {
				name = value
				continue
			}
			if host := hostAddress(value); host != "" && !slices.Contains(addresses, host) {
				addresses = append(addresses, host)
			}
		}
		if name == "" || len(addresses) == 0 {
			continue
		}
		slices.Sort(addresses)
		records = append(records, Record{Addresses: addresses, Name: name})
	}
	return sortRecords(records)
}

// ClusterRecords names every container cluster the graph selects: the two API
// names its own installer polls and its consumers reach it at, the
// applications name every route answers beneath, and each declared node. A
// cluster is named under the container-cluster zone, so the resolver a Machine
// uses and the installer that polls the cluster agree by construction. An
// endpoint that resolved no address contributes no record rather than one
// pointing nowhere.
func ClusterRecords(catalog api.Catalog) []Record {
	zone := containerClusterZone(catalog)
	if zone == "" {
		return nil
	}
	var records []Record
	for _, cluster := range catalog.OfKind(api.ContainerCluster) {
		suffix := "." + cluster.Name() + "." + zone
		endpoints := cluster.Spec().Get("install", "endpoints")
		for _, slot := range []string{"api", "api-int"} {
			if address := hostAddress(endpoints.Get(slot, "address").Text()); address != "" {
				records = append(records, Record{Addresses: []string{address}, Name: slot + suffix})
			}
		}
		if address := hostAddress(endpoints.Get("ingress", "address").Text()); address != "" {
			records = append(records, Record{Addresses: []string{address}, Name: "apps" + suffix, Subtree: true})
		}
		for _, node := range cluster.Spec().Get("nodes").Items() {
			name := node.Get("fqdn").Text()
			address := nodeAddress(catalog, node.Get("machineRef").Text())
			if name == "" || address == "" {
				continue
			}
			records = append(records, Record{Addresses: []string{address}, Name: name})
		}
	}
	return sortRecords(records)
}

// nodeAddress is the address a cluster node answers at, which is the same
// installation address its own installer configures.
func nodeAddress(catalog api.Catalog, reference string) string {
	bound, found := catalog.Find(api.Machine, reference)
	if !found {
		return ""
	}
	selected, issues := machine.InstallAddress(bound, catalog)
	if len(issues) != 0 {
		return ""
	}
	return hostAddress(selected.Get("address").Text())
}

// containerClusterZone is the zone container clusters are named under.
// Effective state materializes it, so this reads one field rather than
// repeating the Environment's own defaulting.
func containerClusterZone(catalog api.Catalog) string {
	environments := catalog.OfKind(api.Environment)
	if len(environments) != 1 {
		return ""
	}
	return environments[0].Spec().Get("domains", "containerClusters").Text()
}

func sortRecords(records []Record) []Record {
	slices.SortFunc(records, func(x, y Record) int {
		switch {
		case x.Name < y.Name:
			return -1
		case x.Name > y.Name:
			return 1
		}
		return 0
	})
	return records
}
