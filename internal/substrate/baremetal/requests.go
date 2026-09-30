package baremetal

import (
	"bytes"
	"encoding/json"
	machineref "github.com/crmarques/bootwright/internal/machine"
	"slices"

	"github.com/crmarques/bootwright/internal/substrate"
)

// Identity names the block, context and Machine one request belongs to.
type Identity struct {
	Block   string `json:"block"`
	Context string `json:"context"`
	Object  string `json:"object"`
}

// Controller is the management controller this machine is proved through, and
// the declaration whose credential answers it. No material is named here.
type Controller struct {
	CredentialsRef string `json:"credentialsRef"`
	Endpoint       string `json:"endpoint"`
	TLSVerify      bool   `json:"tlsVerify"`
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
	data, err := json.Marshal(r)
	if err != nil {
		return nil, refusal("lifecycle.state", "the machine request cannot be encoded", "")
	}
	var probe map[string]any
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&probe); err != nil {
		return nil, refusal("lifecycle.state", "the machine request cannot be decoded", "")
	}
	reencoded, err := json.Marshal(probe)
	if err != nil || !bytes.Equal(data, reencoded) {
		return nil, refusal("lifecycle.state", "the machine request is not canonically ordered", "")
	}
	return data, nil
}

func DecodeRequest(data []byte) (Request, error) {
	var request Request
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		return Request{}, refusal("lifecycle.state", "the frozen machine request is malformed", "")
	}
	if len(bytes.Trim(data[decoder.InputOffset():], " \t\r\n")) != 0 {
		return Request{}, refusal("lifecycle.state", "the frozen machine request contains trailing data", "")
	}
	if request.Version != requestVersion {
		return Request{}, refusal("lifecycle.state", "the frozen machine request has an unsupported version", "")
	}
	canonical, err := request.Canonical()
	if err != nil {
		return Request{}, err
	}
	if !bytes.Equal(canonical, data) {
		return Request{}, refusal("lifecycle.state", "the frozen machine request is not canonical", "")
	}
	return request, nil
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
	references := append(r.Placement.SecretReferences(), r.Controller.CredentialsRef)
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
