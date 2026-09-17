package libvirt

import (
	"bytes"
	"encoding/json"
	"slices"

	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
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
	Identity  Identity            `json:"identity"`
	Networks  []Network           `json:"networks"`
	Packages  []string            `json:"packages"`
	Placement lifecycle.Placement `json:"placement"`
	PoolName  string              `json:"poolName"`
	PoolPath  string              `json:"poolPath"`
	// Provisioned is true when this block installs the closure itself through
	// the host's own package manager. The controller stage installs only the
	// libvirt client a provider selects, so the daemon, its emulator and the
	// TPM helper are this block's on either placement arm.
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
	Controller Controller          `json:"controller"`
	Directory  string              `json:"directory"`
	Disks      []Disk              `json:"disks"`
	Domain     string              `json:"domain"`
	Identity   Identity            `json:"identity"`
	Interfaces []Interface         `json:"interfaces"`
	MemoryMiB  int                 `json:"memoryMiB"`
	Placement  lifecycle.Placement `json:"placement"`
	PoolName   string              `json:"poolName"`
	PoolPath   string              `json:"poolPath"`
	TPM        bool                `json:"tpm"`
	URI        string              `json:"uri"`
	UUID       string              `json:"uuid"`
	VCPU       int                 `json:"vcpu"`
	Version    string              `json:"version"`
}

func (r HostRequest) Canonical() ([]byte, error)    { return canonical(r, "provider host") }
func (r hostRequestV1) Canonical() ([]byte, error)  { return canonical(r, "provider host") }
func (r MachineRequest) Canonical() ([]byte, error) { return canonical(r, "machine") }

// hostRequestV1 is the provider host request as substrate-host-libvirt-v1
// froze it: one daemon rather than the drivers a managed network and pool live
// in. Its field order is the order that version encoded, because the bytes it
// froze are checked against it.
type hostRequestV1 struct {
	Identity    Identity            `json:"identity"`
	Networks    []Network           `json:"networks"`
	Packages    []string            `json:"packages"`
	Placement   lifecycle.Placement `json:"placement"`
	PoolName    string              `json:"poolName"`
	PoolPath    string              `json:"poolPath"`
	Provisioned bool                `json:"provisioned"`
	Service     string              `json:"service"`
	URI         string              `json:"uri"`
	Version     string              `json:"version"`
}

// upgrade keeps the daemon the frozen request named rather than substituting
// the set this build derives, because what is being removed is what that host
// was realized with.
func (r hostRequestV1) upgrade() HostRequest {
	return HostRequest{
		Identity: r.Identity, Networks: r.Networks, Packages: r.Packages,
		Placement: r.Placement, PoolName: r.PoolName, PoolPath: r.PoolPath,
		Provisioned: r.Provisioned, Services: []string{r.Service},
		URI: r.URI, Version: hostRequestVersion,
	}
}

// DecodeHostRequest reads the version this build writes and the one before it,
// so a provider host realized by an earlier build is still removable here.
func DecodeHostRequest(data []byte) (HostRequest, error) {
	version, err := frozenVersion(data, "provider host")
	if err != nil {
		return HostRequest{}, err
	}
	if version == priorHostRequestVersion {
		var prior hostRequestV1
		if err := decode(data, &prior, "provider host"); err != nil {
			return HostRequest{}, err
		}
		if err := verifyCanonical(data, prior, "provider host"); err != nil {
			return HostRequest{}, err
		}
		return prior.upgrade(), nil
	}
	var request HostRequest
	if err := decode(data, &request, "provider host"); err != nil {
		return HostRequest{}, err
	}
	if request.Version != hostRequestVersion {
		return HostRequest{}, refusal("lifecycle.state",
			"the frozen provider host request has an unsupported version: "+version, "")
	}
	return request, verifyCanonical(data, request, "provider host")
}

// frozenVersion reads only the version a frozen request declares, so the shape
// it is decoded into is chosen by what wrote it.
func frozenVersion(data []byte, subject string) (string, error) {
	var declared struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(data, &declared); err != nil || declared.Version == "" {
		return "", refusal("lifecycle.state", "the frozen "+subject+" request declares no version", "")
	}
	return declared.Version, nil
}

func DecodeMachineRequest(data []byte) (MachineRequest, error) {
	var request MachineRequest
	if err := decode(data, &request, "machine"); err != nil {
		return MachineRequest{}, err
	}
	if request.Version != machineRequestVersion {
		return MachineRequest{}, refusal("lifecycle.state", "the frozen machine request has an unsupported version", "")
	}
	return request, verifyCanonical(data, request, "machine")
}

// canonical encodes a request exactly as the plan digest and the adapter both
// consume it, refusing anything a later reader could interpret differently.
func canonical(value any, subject string) ([]byte, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, refusal("lifecycle.state", "the "+subject+" request cannot be encoded", "")
	}
	var probe map[string]any
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&probe); err != nil {
		return nil, refusal("lifecycle.state", "the "+subject+" request cannot be decoded", "")
	}
	reencoded, err := json.Marshal(probe)
	if err != nil || !bytes.Equal(data, reencoded) {
		return nil, refusal("lifecycle.state", "the "+subject+" request is not canonically ordered", "")
	}
	return data, nil
}

func decode(data []byte, target any, subject string) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return refusal("lifecycle.state", "the frozen "+subject+" request is malformed", "")
	}
	if decoder.More() {
		return refusal("lifecycle.state", "the frozen "+subject+" request contains trailing data", "")
	}
	return nil
}

type encodable interface{ Canonical() ([]byte, error) }

func verifyCanonical(data []byte, request encodable, subject string) error {
	reencoded, err := request.Canonical()
	if err != nil {
		return err
	}
	if !bytes.Equal(reencoded, data) {
		return refusal("lifecycle.state", "the frozen "+subject+" request is not canonical", "")
	}
	return nil
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
