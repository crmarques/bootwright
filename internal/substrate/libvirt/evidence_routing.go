package libvirt

type RoutingEvidence struct {
	Answered   bool   `json:"answered"`
	Enabled    bool   `json:"enabled"`
	Forwarding bool   `json:"forwarding"`
	Loaded     bool   `json:"loaded"`
	Name       string `json:"name"`
	Owned      bool   `json:"owned"`
	Present    bool   `json:"present"`
	State      string `json:"state"`
	Table      string `json:"table"`
}

const routingRemedy = "repeat the apply to restore the owned routing unit and its table; remove any nftables table of its name that another tool created"

func (e RoutingEvidence) holdsAnything() bool {
	return e.Present || e.Owned || e.Enabled || e.Loaded || e.State != "" || e.Table != ""
}

func matchRouting(observed RoutingEvidence, request HostRequest, running bool) error {
	if request.Routing == nil {
		if observed.Name != "" || observed.holdsAnything() {
			return refusal("lifecycle.state", "the provider host evidence reports an undeclared routing unit or table", "")
		}
		return nil
	}
	if !observed.Answered || observed.Name != request.Routing.Unit {
		return refusal("lifecycle.state", "the provider host evidence does not prove its frozen routing unit", routingRemedy)
	}
	if !observed.Present || !observed.Owned || !observed.Enabled {
		return refusal("lifecycle.state", "the provider's routing unit is not owned and enabled", routingRemedy)
	}
	if !running {
		return nil
	}
	if observed.State != "active" || observed.Table != RoutingTable(*request.Routing) || !observed.Loaded {
		return refusal("lifecycle.state", "the provider's routing unit is not active with exactly its frozen table loaded", routingRemedy)
	}
	if request.Routing.Forwarding && !observed.Forwarding {
		return refusal("lifecycle.state", "the provider host does not forward IPv4, which its routed and translated VLAN legs need", routingRemedy)
	}
	return nil
}

func routingAbsent(observed RoutingEvidence) error {
	if observed.Name != "" && !observed.Answered || observed.holdsAnything() {
		return refusal("lifecycle.state", "the provider host removal evidence does not prove its routing unit and table gone", "")
	}
	return nil
}

func routingOwned(observed RoutingEvidence) (bool, error) {
	if !observed.Present && observed.Table == "" {
		return false, nil
	}
	if !observed.Answered || !observed.Owned {
		return false, refusal("lifecycle.state", "a routing unit or nftables table of the provider's name exists without this provider's ownership", "")
	}
	return true, nil
}
