package baremetal

import (
	machineref "github.com/crmarques/bootwright/internal/machine"
	"slices"

	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/substrate"
)

// Identity names the block, context and Machine one request belongs to.
type Identity struct {
	Block   string `json:"block"`
	Context string `json:"context"`
	Object  string `json:"object"`
}

// Controller is the management controller this machine is proved through, the
// declaration whose credential answers it, and the bundle that is the one
// anchor its transport verifies against when declared. No material is named
// here.
type Controller struct {
	CredentialsRef string `json:"credentialsRef"`
	Endpoint       string `json:"endpoint"`
	TLSVerify      bool   `json:"tlsVerify"`
	TrustBundleRef string `json:"trustBundleRef,omitempty"`
}

// Interface is one NIC the hardware must report for this to be the machine the
// declaration names.
type Interface struct {
	MACAddress string `json:"macAddress"`
	Name       string `json:"name"`
}

// Request is the complete frozen intent for claiming one physical machine.
// There is nothing here to create: the request names what must be proved, and
// the proof is the whole of what this block does.
type Request struct {
	Controller Controller           `json:"controller"`
	Hardware   []Interface          `json:"hardware"`
	Identity   Identity             `json:"identity"`
	Placement  machineref.Placement `json:"placement"`
	Provider   string               `json:"provider"`
	Version    string               `json:"version"`
}

// Canonical encodes the request exactly as the plan digest and the adapter both
// consume it, refusing anything a later reader could interpret differently.
func (r Request) Canonical() ([]byte, error) {
	return reconciliation.Freeze(r, "machine")
}

func DecodeRequest(data []byte) (Request, error) {
	return reconciliation.ThawVersion[Request](data, "machine", requestVersion)
}

// ReservationKeys claim the one exclusive thing this block takes: the machine
// itself, named by its controller. It owns no path, unit or socket, because it
// creates none of those.
func (r Request) ReservationKeys() []string {
	return []string{substrate.ControllerReservationKey(r.Controller.Endpoint)}
}

// SecretReferences names every declaration this request's execution needs
// bound, so the operation freezes them before it registers.
func (r Request) SecretReferences() []string {
	references := append(r.Placement.SecretReferences(), r.Controller.CredentialsRef, r.Controller.TrustBundleRef)
	out := make([]string, 0, len(references))
	for _, reference := range references {
		if reference != "" {
			out = append(out, reference)
		}
	}
	slices.Sort(out)
	return slices.Compact(out)
}

// Addresses are the hardware addresses this machine must report, in canonical
// order, so evidence is compared against one settled set.
func (r Request) Addresses() []string {
	addresses := make([]string, 0, len(r.Hardware))
	for _, declared := range r.Hardware {
		addresses = append(addresses, declared.MACAddress)
	}
	slices.Sort(addresses)
	return slices.Compact(addresses)
}
