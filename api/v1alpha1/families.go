package v1alpha1

import (
	"maps"
	"slices"
	"strings"
)

type Category string

const (
	ContainerClusterCategory Category = "containerCluster"
	StorageClusterCategory   Category = "storageCluster"
)

type Role string

const (
	ClientRole        Role = "client"
	ObjectGatewayRole Role = "objectGateway"
	ObjectUserRole    Role = "objectUser"
	ConsumerRole      Role = "consumer"
	ConsumerShareRole Role = "consumerShare"
)

type Family struct {
	Category Category
	Kinds    []Kind
	Roles    map[Role]Kind
}

var families = []Family{
	{ContainerClusterCategory, []Kind{OpenShiftCluster}, nil},
	{StorageClusterCategory, []Kind{CephCluster}, map[Role]Kind{ClientRole: CephClient, ObjectGatewayRole: CephObjectGateway, ObjectUserRole: CephObjectUser, ConsumerRole: CephConsumer, ConsumerShareRole: CephNFSExport}},
}

func Families() []Family {
	out := make([]Family, 0, len(families))
	for _, family := range families {
		out = append(out, Family{Category: family.Category, Kinds: slices.Clone(family.Kinds), Roles: maps.Clone(family.Roles)})
	}
	return out
}

func RoleKind(category Category, role Role) (Kind, bool) {
	for _, family := range families {
		if family.Category == category {
			kind, ok := family.Roles[role]
			return kind, ok
		}
	}
	return "", false
}

func FamilyKinds() []Kind {
	kinds := ClusterKinds()
	for _, root := range ClusterKinds() {
		for _, child := range Children(root) {
			if field, _ := Schema(child).Field("clusterRef"); field.Shape.Category == "" {
				kinds = append(kinds, child)
			}
		}
	}
	return kinds
}

func (c Category) SelectionField() string { return string(c) + "s" }

func CategoryKinds(category Category) []Kind {
	for _, family := range families {
		if family.Category == category {
			return slices.Clone(family.Kinds)
		}
	}
	return nil
}

func CategoryOf(kind Kind) (Category, bool) {
	for _, family := range families {
		if slices.Contains(family.Kinds, kind) {
			return family.Category, true
		}
	}
	return "", false
}

func ClusterKinds() []Kind {
	var kinds []Kind
	for _, family := range families {
		kinds = append(kinds, family.Kinds...)
	}
	return kinds
}

func IsClusterKind(kind Kind) bool {
	_, ok := CategoryOf(kind)
	return ok
}

func clusters(categories ...Category) *Shape {
	var kinds []Kind
	for _, category := range categories {
		kinds = append(kinds, CategoryKinds(category)...)
	}
	s := ref(kinds...)
	if len(categories) == 1 {
		s.Category = categories[0]
	}
	return s
}

func (c Catalog) FindInCategory(category Category, name string) (Object, bool) {
	for _, kind := range CategoryKinds(category) {
		if object, ok := c.Find(kind, name); ok {
			return object, true
		}
	}
	return Object{}, false
}

func (c Catalog) OfCategory(category Category) []Object {
	var objects []Object
	for _, kind := range CategoryKinds(category) {
		objects = append(objects, c.OfKind(kind)...)
	}
	return objects
}

type clusterLayout struct {
	nodes          []string
	nodeLogins     [][]string
	successorLogin []string
	controllerFIPS []string
	agentInstall   []string
}

var clusterLayouts = map[Kind]clusterLayout{
	OpenShiftCluster: {
		nodes:          []string{"nodes"},
		nodeLogins:     [][]string{{"install", "nodeSSH", "keyPairRef"}, {"install", "nodeSSH", "publicKeyRef"}, {"install", "nodeSSH", "privateKeyRef"}},
		controllerFIPS: []string{"security", "fips", "enabled"},
		agentInstall:   []string{"install", "agent"},
	},
	CephCluster: {
		nodes:          []string{"topology", "nodes"},
		nodeLogins:     [][]string{{"cephadm", "clusterSSH", "keyRef"}},
		successorLogin: []string{"cephadm", "clusterSSH"},
	},
}

func (c Catalog) Clusters() []Object {
	var objects []Object
	for _, kind := range ClusterKinds() {
		objects = append(objects, c.OfKind(kind)...)
	}
	return objects
}

func ClusterNodes(cluster Object) (string, Value, bool) {
	layout, ok := clusterLayouts[cluster.Kind()]
	if !ok {
		return "", Value{}, false
	}
	return "$.spec." + strings.Join(layout.nodes, "."), cluster.Spec().Get(layout.nodes...), true
}

func ClusterNodeMachines(cluster Object) []string {
	_, nodes, _ := ClusterNodes(cluster)
	var names []string
	for _, node := range nodes.Items() {
		names = append(names, node.Get("machineRef").Text())
	}
	return names
}

func ClusterNodeLoginField(cluster Object, key string) (string, bool) {
	if key == "" {
		return "", false
	}
	for _, path := range clusterLayouts[cluster.Kind()].nodeLogins {
		if cluster.Spec().Get(path...).Text() == key {
			return "spec." + strings.Join(path, "."), true
		}
	}
	return "", false
}

func ClusterSuccessorLogin(cluster Object) bool {
	path := clusterLayouts[cluster.Kind()].successorLogin
	if path == nil || cluster.Spec().Get("management").Text() != "managed" {
		return false
	}
	login := cluster.Spec().Get(path...)
	user := login.Get("user").Text()
	return user != "" && user != "root" && login.Has("keyRef")
}

func ClusterRequiresControllerFIPS(cluster Object) bool {
	path := clusterLayouts[cluster.Kind()].controllerFIPS
	return path != nil && cluster.Spec().Get(path...).Bool()
}

func ClusterInstallsByAgent(cluster Object) bool {
	path := clusterLayouts[cluster.Kind()].agentInstall
	return path != nil && cluster.Spec().Has(path...)
}
