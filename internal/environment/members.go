package environment

import api "github.com/crmarques/bootwright/api/v1alpha1"

type Member struct {
	Name, FQDN, Machine string
}

type Membership struct {
	Field string
	Nodes []Member
}

func Members(owners ...func(api.Object) (Membership, bool)) func(api.Object) (Membership, bool) {
	return func(object api.Object) (Membership, bool) {
		for _, owner := range owners {
			if owner == nil {
				continue
			}
			if membership, bound := owner(object); bound {
				return membership, true
			}
		}
		return Membership{}, false
	}
}

func MembersOf(field string, nodes api.Value) Membership {
	membership := Membership{Field: field, Nodes: []Member{}}
	for _, node := range nodes.Items() {
		membership.Nodes = append(membership.Nodes, Member{Name: node.Get("name").Text(), FQDN: node.Get("fqdn").Text(), Machine: node.Get("machineRef").Text()})
	}
	return membership
}
