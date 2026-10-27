package v1alpha1

import (
	"maps"
	"slices"
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
