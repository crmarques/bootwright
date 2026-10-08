package v1alpha1

import (
	"maps"
	"slices"
)

const APIVersion = "bootwright.io/v1alpha1"

type Kind string

const (
	Environment            Kind = "Environment"
	Entitlement            Kind = "Entitlement"
	Machine                Kind = "Machine"
	MachineImage           Kind = "MachineImage"
	MachineInstallProfile  Kind = "MachineInstallProfile"
	NetworkConfig          Kind = "NetworkConfig"
	InfraProvider          Kind = "InfraProvider"
	Proxy                  Kind = "Proxy"
	DNSServer              Kind = "DNSServer"
	NTPServer              Kind = "NTPServer"
	ArtifactServer         Kind = "ArtifactServer"
	Registry               Kind = "Registry"
	LoadBalancer           Kind = "LoadBalancer"
	ContainerCluster       Kind = "ContainerCluster"
	StorageCluster         Kind = "StorageCluster"
	StoragePlacementPolicy Kind = "StoragePlacementPolicy"
	StoragePool            Kind = "StoragePool"
	StorageFilesystem      Kind = "StorageFilesystem"
	StorageObjectGateway   Kind = "StorageObjectGateway"
	StorageNFSExport       Kind = "StorageNFSExport"
	StorageExport          Kind = "StorageExport"
	ClusterAddon           Kind = "ClusterAddon"
	ClusterAddonProfile    Kind = "ClusterAddonProfile"
	ClusterAddonBinding    Kind = "ClusterAddonBinding"
	CustomPlaybook         Kind = "CustomPlaybook"
	Secret                 Kind = "Secret"
)

var kindOrder = []Kind{Environment, Entitlement, Machine, MachineImage, MachineInstallProfile, NetworkConfig, InfraProvider, Proxy, DNSServer, NTPServer, ArtifactServer, Registry, LoadBalancer, ContainerCluster, StorageCluster, StoragePlacementPolicy, StoragePool, StorageFilesystem, StorageObjectGateway, StorageNFSExport, StorageExport, ClusterAddon, ClusterAddonProfile, ClusterAddonBinding, CustomPlaybook, Secret}

func Kinds() []Kind           { return slices.Clone(kindOrder) }
func KindIndex(kind Kind) int { return slices.Index(kindOrder, kind) }

type Object struct {
	kind   Kind
	name   string
	labels Value
	spec   Value
}

func NewObject(kind Kind, name string, labels, spec Value) Object {
	return Object{kind: kind, name: name, labels: labels, spec: spec}
}
func (o Object) Kind() Kind                 { return o.kind }
func (o Object) Name() string               { return o.name }
func (o Object) Labels() Value              { return o.labels }
func (o Object) Spec() Value                { return o.spec }
func (o Object) WithSpec(spec Value) Object { o.spec = spec; return o }
func (o Object) Identity() string           { return string(o.kind) + "/" + o.name }

// Value returns the presence-preserving API envelope for this object.
func (o Object) Value() Value {
	metadata := MapValue(FieldValue{Name: "name", Value: StringValue(o.name)})
	if o.labels.Present() {
		metadata = metadata.With("labels", o.labels)
	}
	return MapValue(
		FieldValue{Name: "apiVersion", Value: StringValue(APIVersion)},
		FieldValue{Name: "kind", Value: StringValue(string(o.kind))},
		FieldValue{Name: "metadata", Value: metadata},
		FieldValue{Name: "spec", Value: o.spec},
	)
}

type Catalog struct {
	objects     []Object
	byKind      map[Kind][]Object
	byIdentity  map[string][]Object
	undecodable map[string]bool
}

func NewCatalog(objects []Object) Catalog {
	c := Catalog{objects: slices.Clone(objects), byKind: map[Kind][]Object{}, byIdentity: map[string][]Object{}}
	for _, o := range objects {
		c.byKind[o.Kind()] = append(c.byKind[o.Kind()], o)
		c.byIdentity[o.Identity()] = append(c.byIdentity[o.Identity()], o)
	}
	return c
}
func (c Catalog) Objects() []Object { return slices.Clone(c.objects) }
func (c Catalog) OfKind(kind Kind) []Object {
	return slices.Clone(c.byKind[kind])
}
func (c Catalog) Find(kind Kind, name string) (Object, bool) {
	objects := c.byIdentity[string(kind)+"/"+name]
	if len(objects) != 1 {
		return Object{}, false
	}
	return objects[0], true
}

// WithUndecodable records the Kind/name identities of selected documents that
// failed decoding, so a check naming one adds no diagnostic of its own.
func (c Catalog) WithUndecodable(identities map[string]bool) Catalog {
	c.undecodable = maps.Clone(identities)
	return c
}

func (c Catalog) Undecodable(kind Kind, name string) bool {
	return c.undecodable[string(kind)+"/"+name]
}

type Issue struct {
	Code        string
	Field       string
	Message     string
	Remediation string
}

// FieldOrigin identifies the authored owner of a value materialized by a domain
// normalizer, allowing admission diagnostics to retain source provenance.
type FieldOrigin struct {
	Field       string
	SourceKind  Kind
	SourceName  string
	SourceField string
}
