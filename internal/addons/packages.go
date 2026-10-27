package addons

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/netip"
	"slices"
	"time"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/addons/clusterobjects"
	"github.com/crmarques/bootwright/internal/reconciliation"
)

type GroupShape string

const (
	NoGroup       GroupShape = "none"
	OwnNamespace  GroupShape = "ownNamespace"
	AllNamespaces GroupShape = "allNamespaces"
)

type Approval string

const (
	Exact           Approval = "exact"
	OperatorManaged Approval = "operatorManaged"
)

type Package struct {
	Name                string
	Provides, Requires  []string
	RequiresFor         func(Instance) []string
	Last                bool
	PlacementNamespaces []string
	Normalize           func(api.Value) api.Value
	Budget              time.Duration
	OLM                 *OLM
	Admit               func(Instance, ClusterFacts) []api.Issue
	Render              func(Instance) (Rendered, error)
}
type OLM struct {
	Namespace                                       string
	NamespaceLabels                                 map[string]string
	OperatorGroup                                   GroupShape
	GroupName, Subscription, Package, DefaultSource string
	Approval                                        Approval
}
type Instance struct {
	Cluster, Binding string
	Config           api.Value
	Facts            ClusterFacts
}
type ClusterFacts struct {
	Name, Release                                     string
	Nodes                                             []string
	Family                                            string
	MachineNetworks, ClusterNetworks, ServiceNetworks []netip.Prefix
	Endpoints                                         []AddressHolder
	NodeNetworks                                      []NodeNetwork
	Reserved                                          []AddressHolder
	Gateway                                           OVNGateway
	SecondaryNetworks                                 []SecondaryNetwork
	Exports                                           []ConsumerFacts
	Trust                                             TrustFacts
}

type ConsumerFacts struct {
	Name, Cluster string
	Gateway, TLS  bool
}

type TrustFacts struct {
	Policy  string
	Bundles int
}
type AddressHolder struct {
	Addr   netip.Addr
	Holder string
}
type NodeNetwork struct {
	Machine, DefaultRouteInterface string
	Interfaces                     []NodeInterface
	NextHops                       []netip.Addr
}
type NodeInterface struct {
	Name, Type string
	Addresses  []netip.Prefix
}
type OVNGateway struct {
	RoutingViaHost bool
	IPForwarding   string
}
type SecondaryNetwork struct {
	Name, UplinkInterface, UplinkBridge string
	Subnets                             []netip.Prefix
}
type Rendered struct {
	SubscriptionConfig *clusterobjects.SubscriptionConfig
	Before, After      []clusterobjects.Phase
	Checks             []clusterobjects.Check
	Requires           []reconciliation.ObjectRef
}

func Decode[T any](v api.Value) (T, error) {
	var result T
	data, err := json.Marshal(nativeValue(v))
	if err != nil {
		return result, err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	err = decoder.Decode(&result)
	return result, err
}
func nativeValue(v api.Value) any {
	switch v.Type() {
	case api.Mapping:
		result := map[string]any{}
		for _, field := range v.Fields() {
			result[field.Name] = nativeValue(field.Value)
		}
		return result
	case api.Sequence:
		result := make([]any, 0, v.Len())
		for _, item := range v.Items() {
			result = append(result, nativeValue(item))
		}
		return result
	case api.String:
		return v.Text()
	case api.Boolean:
		return v.Bool()
	case api.Integer, api.Number:
		return json.Number(v.Text())
	default:
		return nil
	}
}

type Registry struct{ entries []Package }

func NewRegistry(entries []Package) (Registry, error) { return NewRegistryWithExamples(entries, nil) }

func NewRegistryWithExamples(entries []Package, examples map[string][]api.Value) (Registry, error) {
	r := Registry{}
	names, namespaces, operators := map[string]bool{}, map[string]bool{}, map[string]bool{}
	readers := map[string]bool{"kubevirt": true, "dataFoundation": true, "metallb": true, "nmstate": true}
	for _, p := range entries {
		for _, token := range p.Requires {
			readers[token] = true
		}
		if p.RequiresFor != nil {
			for _, config := range examples[p.Name] {
				for _, token := range p.RequiresFor(Instance{Config: config}) {
					readers[token] = true
				}
			}
		}
	}
	for _, p := range entries {
		if len(p.Name) == 0 || len(p.Name) > 9 || p.Render == nil || p.Budget <= 0 || p.Budget > 5*time.Hour+50*time.Minute {
			return Registry{}, fmt.Errorf("invalid package %q", p.Name)
		}
		for i, c := range p.Name {
			if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' && i > 0 && i < len(p.Name)-1) {
				return Registry{}, fmt.Errorf("invalid package name %q", p.Name)
			}
		}
		if names[p.Name] {
			return Registry{}, fmt.Errorf("duplicate package %q", p.Name)
		}
		names[p.Name] = true
		if p.Last && len(p.Provides) > 0 {
			return Registry{}, fmt.Errorf("last package %q provides a capability", p.Name)
		}
		for _, token := range p.Provides {
			if !readers[token] {
				return Registry{}, fmt.Errorf("package %q provides unread capability %q", p.Name, token)
			}
		}
		if p.OLM != nil {
			if p.OLM.Approval != Exact && (p.OLM.Approval != OperatorManaged || p.Name != "fdf") {
				return Registry{}, fmt.Errorf("package %q has an unqualified OLM approval policy", p.Name)
			}
			if p.OLM.Namespace == "" || p.OLM.Namespace == "openshift-operators" || namespaces[p.OLM.Namespace] {
				return Registry{}, fmt.Errorf("package %q has an invalid or shared operator namespace", p.Name)
			}
			namespaces[p.OLM.Namespace] = true
			if p.OLM.Package == "" || operators[p.OLM.Package] {
				return Registry{}, fmt.Errorf("package %q has an empty or shared OLM package name", p.Name)
			}
			operators[p.OLM.Package] = true
		}
		r.entries = append(r.entries, clonePackage(p))
	}
	slices.SortFunc(r.entries, func(a, b Package) int {
		if a.Name < b.Name {
			return -1
		}
		if a.Name > b.Name {
			return 1
		}
		return 0
	})
	return r, nil
}
func clonePackage(p Package) Package {
	p.PlacementNamespaces = slices.Clone(p.PlacementNamespaces)
	p.Provides = slices.Clone(p.Provides)
	p.Requires = slices.Clone(p.Requires)
	if p.OLM != nil {
		o := *p.OLM
		o.NamespaceLabels = map[string]string{}
		for k, v := range p.OLM.NamespaceLabels {
			o.NamespaceLabels[k] = v
		}
		p.OLM = &o
	}
	return p
}
func (r Registry) Packages() []Package {
	result := make([]Package, 0, len(r.entries))
	for _, p := range r.entries {
		result = append(result, clonePackage(p))
	}
	return result
}
func (r Registry) Find(name string) (Package, bool) {
	for _, p := range r.entries {
		if p.Name == name {
			return clonePackage(p), true
		}
	}
	return Package{}, false
}
