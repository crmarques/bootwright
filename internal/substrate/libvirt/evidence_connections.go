package libvirt

import (
	"slices"
	"strings"
)

const (
	roleTrunk = "trunk"
	roleLeg   = "leg"
	legZone   = "trusted"
)

type ConnectionEvidence struct {
	Active     bool   `json:"active"`
	Answered   bool   `json:"answered"`
	Definition bool   `json:"definition"`
	Device     string `json:"device"`
	Name       string `json:"name"`
	Owned      bool   `json:"owned"`
	Present    bool   `json:"present"`
	Role       string `json:"role"`
	Zone       string `json:"zone"`
}

type frozenConnection struct{ name, role, device string }

func (r HostRequest) connections() []frozenConnection {
	var frozen []frozenConnection
	for _, trunk := range r.Trunks {
		frozen = append(frozen, frozenConnection{name: trunk.Name, role: roleTrunk, device: trunk.Bridge})
	}
	for _, leg := range r.Legs {
		frozen = append(frozen, frozenConnection{name: leg.Name, role: roleLeg, device: leg.Device})
	}
	slices.SortFunc(frozen, func(x, y frozenConnection) int { return strings.Compare(x.name, y.name) })
	return frozen
}

const connectionRemedy = "repeat the apply to restore the owned trunk and VLAN legs; remove any NetworkManager profile or link that takes their name, UUID or device"

func pairConnections(observed []ConnectionEvidence, request HostRequest) ([]ConnectionEvidence, error) {
	frozen := request.connections()
	if len(observed) != len(frozen) {
		return nil, refusal("lifecycle.state", "the provider host evidence does not cover exactly its frozen trunks and VLAN legs", "")
	}
	sorted := slices.Clone(observed)
	slices.SortFunc(sorted, func(x, y ConnectionEvidence) int { return strings.Compare(x.Name, y.Name) })
	for index, connection := range frozen {
		entry := sorted[index]
		if entry.Name != connection.name || entry.Role != connection.role || entry.Device != connection.device {
			return nil, refusal("lifecycle.state", "the provider host evidence does not match its frozen trunks and VLAN legs", "")
		}
		if !entry.Answered {
			return nil, refusal("lifecycle.state", "NetworkManager did not answer for an owned trunk or VLAN leg", connectionRemedy)
		}
	}
	return sorted, nil
}

func matchConnections(evidence HostEvidence, request HostRequest) error {
	sorted, err := pairConnections(evidence.Connections, request)
	if err != nil {
		return err
	}
	if len(sorted) != 0 && !evidence.NetworkManager {
		return refusal("lifecycle.state", "the provider host does not run NetworkManager with nmcli, which its trunks and VLAN legs need",
			"start and enable NetworkManager.service on the provider host, with /usr/bin/nmcli present, then repeat the apply")
	}
	for _, entry := range sorted {
		if !entry.Present || !entry.Owned {
			return refusal("lifecycle.state", "an owned trunk or VLAN leg is not present with this context's ownership", connectionRemedy)
		}
		if !entry.Definition {
			return refusal("lifecycle.state", "an owned trunk or VLAN leg does not carry its frozen definition", connectionRemedy)
		}
		if !entry.Active {
			return refusal("lifecycle.state", "an owned trunk or VLAN leg is not active", connectionRemedy)
		}
		if entry.Zone != legZone {
			return refusal("lifecycle.state", "an owned trunk or VLAN leg is not bound to the host firewall's trusted zone", connectionRemedy)
		}
	}
	return nil
}

func connectionsAbsent(observed []ConnectionEvidence) error {
	for _, entry := range observed {
		if !entry.Answered {
			return refusal("lifecycle.state", "NetworkManager did not answer for an owned trunk or VLAN leg, so its removal is not proved", "")
		}
		if entry.Present || entry.Owned || entry.Active {
			return refusal("lifecycle.state", "the provider host removal evidence still reports an owned trunk or VLAN leg", "")
		}
	}
	return nil
}

func connectionsHeld(observed []ConnectionEvidence, request HostRequest) error {
	sorted, err := pairConnections(observed, request)
	if err != nil {
		return err
	}
	for _, entry := range sorted {
		if !entry.Present || !entry.Owned || !entry.Active {
			return refusal("lifecycle.state", "the provider host no longer holds every owned trunk and VLAN leg active", "")
		}
	}
	return nil
}

func connectionsOwned(observed []ConnectionEvidence) (bool, error) {
	held := false
	for _, entry := range observed {
		if !entry.Present {
			continue
		}
		if !entry.Answered || !entry.Owned {
			return false, refusal("lifecycle.state", "a NetworkManager profile of an owned trunk or VLAN leg's name exists without this context's ownership", "")
		}
		held = true
	}
	return held, nil
}
