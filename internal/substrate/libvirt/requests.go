package libvirt

import (
	machineref "github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"slices"

	"github.com/crmarques/bootwright/internal/substrate"
)

// Identity names the block, context and object one request belongs to.
type Identity struct {
	Block   string `json:"block"`
	Context string `json:"context"`
	Object  string `json:"object"`
}

// Network is one libvirt network a provider host owns or requires. A managed
// entry is defined from these fields; an external one is proved present as a
// link and never defined, changed or removed.
type Network struct {
	Address string `json:"address,omitempty"`
	Bridge  string `json:"bridge"`
	Forward string `json:"forward,omitempty"`
	Managed bool   `json:"managed"`
	Name    string `json:"name"`
}

// HostRequest is the complete frozen intent for one libvirt provider host: the
// closure it runs, the networks its attachments declare and the pool its
// emulated controllers stage media in. It carries no secret value.
type HostRequest struct {
	Identity  Identity             `json:"identity"`
	Networks  []Network            `json:"networks"`
	Packages  []string             `json:"packages"`
	Placement machineref.Placement `json:"placement"`
	PoolName  string               `json:"poolName"`
	PoolPath  string               `json:"poolPath"`
	// Provisioned is true when this block installs the closure itself through
	// the host's own package manager, which it does only on a host reached over
	// SSH. On the controller the controller stage installs the closure, so this
	// block proves it present there and installs nothing.
	Provisioned bool `json:"provisioned"`
	// Services are the libvirt driver daemons this provider depends on, in
	// canonical order. Each is enabled as well as started, because a network
	// and a pool set to autostart only come back after a restart when the
	// driver that owns them does.
	Services []string `json:"services"`
	URI      string   `json:"uri"`
	Version  string   `json:"version"`
}

// Disk is one qcow2 image a domain owns, at the exact size the profile froze.
type Disk struct {
	Name    string `json:"name"`
	Path    string `json:"path"`
	SizeGiB int    `json:"sizeGiB"`
	Target  string `json:"target"`
}

// Interface is one attachment the domain is wired to, with the address this
// declaration always derives. A managed attachment names the libvirt network
// that owns the bridge, so the hypervisor holds the dependency and refuses to
// start a domain whose network is not up; an external one names the bridge
// alone, because nothing on this host defines it.
type Interface struct {
	Bridge     string `json:"bridge"`
	MACAddress string `json:"macAddress"`
	Name       string `json:"name"`
	Network    string `json:"network"`
}

// Controller is the emulated BMC this Machine is managed through: its own
// container, unit and listener, exposing exactly this domain.
type Controller struct {
	Address        string `json:"address"`
	CredentialsRef string `json:"credentialsRef"`
	Endpoint       string `json:"endpoint"`
	Image          string `json:"image"`
	Port           int    `json:"port"`
	Unit           string `json:"unit"`
}

// MachineRequest is the complete frozen intent for one virtual machine and its
// management controller. It names the credential declaration its controller
// answers with, never any material.
type MachineRequest struct {
	Controller Controller           `json:"controller"`
	Directory  string               `json:"directory"`
	Disks      []Disk               `json:"disks"`
	Domain     string               `json:"domain"`
	Identity   Identity             `json:"identity"`
	Interfaces []Interface          `json:"interfaces"`
	MemoryMiB  int                  `json:"memoryMiB"`
	Placement  machineref.Placement `json:"placement"`
	PoolName   string               `json:"poolName"`
	PoolPath   string               `json:"poolPath"`
	TPM        bool                 `json:"tpm"`
	URI        string               `json:"uri"`
	UUID       string               `json:"uuid"`
	VCPU       int                  `json:"vcpu"`
	Version    string               `json:"version"`
}

func (r HostRequest) Canonical() ([]byte, error) {
	return reconciliation.Freeze(r, "provider host")
}

func (r MachineRequest) Canonical() ([]byte, error) {
	return reconciliation.Freeze(r, "machine")
}

func DecodeHostRequest(data []byte) (HostRequest, error) {
	request, err := reconciliation.Thaw[HostRequest](data, "provider host")
	if err != nil {
		return HostRequest{}, err
	}
	if request.Version != hostRequestVersion {
		return HostRequest{}, refusal("lifecycle.state",
			"the frozen provider host request has an unsupported version: "+request.Version, "")
	}
	return request, reconciliation.ProveCanonical(data, request, "provider host")
}

func DecodeMachineRequest(data []byte) (MachineRequest, error) {
	request, err := reconciliation.Thaw[MachineRequest](data, "machine")
	if err != nil {
		return MachineRequest{}, err
	}
	if request.Version != machineRequestVersion {
		return MachineRequest{}, refusal("lifecycle.state", "the frozen machine request has an unsupported version", "")
	}
	return request, reconciliation.ProveCanonical(data, request, "machine")
}

// ReservationKeys are the exclusive host resources each request claims before
// its first effect, so a second context refuses rather than taking them.
func (r HostRequest) ReservationKeys() []string {
	keys := []string{"path:" + r.PoolPath}
	for _, network := range r.Networks {
		if !network.Managed {
			continue
		}
		keys = append(keys, "bridge:"+network.Bridge, "libvirt-network:"+network.Name)
	}
	slices.Sort(keys)
	return slices.Compact(keys)
}

func (r MachineRequest) ReservationKeys() []string {
	keys := []string{
		"libvirt-domain:" + r.Domain,
		"unit:" + r.Controller.Unit,
		"socket:" + r.Controller.Address + ":" + substrate.FormatPort(r.Controller.Port),
		"path:" + r.Directory,
	}
	slices.Sort(keys)
	return slices.Compact(keys)
}

// SecretReferences names every declaration each request's execution needs
// bound, so the operation freezes them before it registers.
func (r HostRequest) SecretReferences() []string {
	return sortedUnique(r.Placement.SecretReferences())
}

func (r MachineRequest) SecretReferences() []string {
	return sortedUnique(append(r.Placement.SecretReferences(), r.Controller.CredentialsRef))
}

func sortedUnique(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value != "" {
			out = append(out, value)
		}
	}
	slices.Sort(out)
	return slices.Compact(out)
}
