package libvirt

import (
	"bytes"
	"encoding/json"
	"slices"
	"strings"
)

const maxEvidenceBytes = 64 << 10

// HostEvidence is the only result shape the provider host adapter may return.
// Go validates it strictly, so an adapter cannot widen a postcondition or
// report a network the operation did not freeze.
type HostEvidence struct {
	Absent        bool              `json:"absent"`
	Hypervisor    bool              `json:"hypervisor"`
	Networks      []NetworkEvidence `json:"networks"`
	Pool          string            `json:"pool"`
	Postcondition bool              `json:"postcondition"`
	Request       string            `json:"request"`
	Services      []ServiceEvidence `json:"services"`
	URI           bool              `json:"uri"`
}

// ServiceEvidence is one driver daemon's observed state. Enablement is proved
// through the state the host reports, never through the request that asked for
// it.
type ServiceEvidence struct {
	Enabled bool   `json:"enabled"`
	Name    string `json:"name"`
	State   string `json:"state"`
}

type NetworkEvidence struct {
	Bridge  bool   `json:"bridge"`
	Managed bool   `json:"managed"`
	Name    string `json:"name"`
	Owned   bool   `json:"owned"`
	State   string `json:"state"`
}

// MachineEvidence is the only result shape the machine adapter may return. It
// proves the domain, its disks, the controller unit and the controller's own
// answer; a power state it did not read is empty, never assumed.
type MachineEvidence struct {
	Absent bool `json:"absent"`
	// Answered is whether the hypervisor answered for the domain: it returned
	// the definition, or said it defines no such domain. A silent hypervisor
	// reports an empty Domain and State too, and neither then proves anything,
	// so an empty Domain means absent only while Answered is true. Evidence
	// without the field decodes as silent.
	Answered      bool           `json:"answered"`
	Controller    string         `json:"controller"`
	Disks         []DiskEvidence `json:"disks"`
	Domain        string         `json:"domain"`
	Owned         bool           `json:"owned"`
	Postcondition bool           `json:"postcondition"`
	Power         string         `json:"power"`
	Request       string         `json:"request"`
	// State is what the hypervisor says the domain is doing, in libvirt's own
	// words. Only `shut off` means removing it interrupts nothing.
	State  string `json:"state"`
	System string `json:"system"`
	Unit   string `json:"unit"`
}

type DiskEvidence struct {
	Name    string `json:"name"`
	Present bool   `json:"present"`
	SizeGiB int    `json:"sizeGiB"`
}

// ValidateHostPresence accepts evidence only when it proves the exact frozen
// request is realized: the closure present, the daemon active, the declared URI
// answering, every managed network owned and active, every external bridge
// present, and the pool active.
func ValidateHostPresence(data []byte, request HostRequest, digest string) error {
	evidence, err := decodeHostEvidence(data, digest)
	if err != nil {
		return err
	}
	if !evidence.Postcondition || evidence.Absent {
		return refusal("lifecycle.state", "the provider host adapter did not prove its postcondition", "")
	}
	if !evidence.Hypervisor {
		return refusal("lifecycle.state", "the provider host does not carry the hypervisor closure", "")
	}
	if err := matchServices(evidence.Services, request.Services); err != nil {
		return err
	}
	if !evidence.URI {
		return refusal("lifecycle.state", "the declared libvirt connection does not answer", "")
	}
	if evidence.Pool != "active" {
		return refusal("lifecycle.state", "the provider's virtual-media pool is not active", "")
	}
	return matchNetworks(evidence.Networks, request.Networks)
}

// matchServices requires every frozen driver daemon to be running and enabled.
// A daemon that runs only because something woke it leaves the networks and
// pool it owns absent after the host restarts, so enablement is as much a
// postcondition as the running state is.
func matchServices(observed []ServiceEvidence, frozen []string) error {
	if len(observed) != len(frozen) {
		return refusal("lifecycle.state", "the provider host evidence does not cover every libvirt driver daemon", "")
	}
	sorted := slices.Clone(observed)
	slices.SortFunc(sorted, func(x, y ServiceEvidence) int { return strings.Compare(x.Name, y.Name) })
	ordered := slices.Sorted(slices.Values(frozen))
	for index, name := range ordered {
		entry := sorted[index]
		if entry.Name != name {
			return refusal("lifecycle.state", "the provider host evidence does not match its frozen driver daemons", "")
		}
		if entry.State != "active" {
			return refusal("lifecycle.state", "a libvirt driver daemon the provider depends on is not active", "")
		}
		if !entry.Enabled {
			return refusal("lifecycle.state", "a libvirt driver daemon the provider depends on does not start with the host", "")
		}
	}
	return nil
}

func matchNetworks(observed []NetworkEvidence, frozen []Network) error {
	if len(observed) != len(frozen) {
		return refusal("lifecycle.state", "the provider host evidence does not cover every declared network", "")
	}
	sorted := slices.Clone(observed)
	slices.SortFunc(sorted, func(x, y NetworkEvidence) int { return strings.Compare(x.Name, y.Name) })
	for index, network := range frozen {
		entry := sorted[index]
		if entry.Name != network.Name || entry.Managed != network.Managed {
			return refusal("lifecycle.state", "the provider host evidence does not match its frozen networks", "")
		}
		if !entry.Bridge {
			return refusal("lifecycle.state", "a declared network bridge is not present on the provider host", "")
		}
		if !network.Managed {
			continue
		}
		if entry.State != "active" {
			return refusal("lifecycle.state", "a managed libvirt network is not active", "")
		}
		if !entry.Owned {
			return refusal("lifecycle.state", "a managed libvirt network does not carry this context's ownership", "")
		}
	}
	return nil
}

// ValidateHostAbsence accepts evidence only when it positively proves that
// every owned network and the pool are gone. Packages, foreign networks and
// external bridges are never this block's to remove, so it reports nothing
// about them.
func ValidateHostAbsence(data []byte, digest string) error {
	evidence, err := decodeHostEvidence(data, digest)
	if err != nil {
		return err
	}
	if !evidence.Postcondition || !evidence.Absent {
		return refusal("lifecycle.state", "the provider host adapter did not prove removal", "")
	}
	if evidence.Pool != "" {
		return refusal("lifecycle.state", "the provider host removal evidence still reports its pool", "")
	}
	for _, network := range evidence.Networks {
		if network.Managed {
			return refusal("lifecycle.state", "the provider host removal evidence still reports a managed network", "")
		}
	}
	return nil
}

// ValidateHostPartial accepts evidence only when it positively proves this
// context's own provider host is part way realized: its pool or one of its
// owned managed networks is present while the whole is not. A managed network
// the hypervisor defines without this context's ownership is foreign, so it
// proves nothing here and leaves the effect unknown. The hypervisor closure is
// shared host software this block never removes, so its presence alone is not
// a partial realization.
func ValidateHostPartial(data []byte, digest string) error {
	evidence, err := decodeHostEvidence(data, digest)
	if err != nil {
		return err
	}
	if evidence.Postcondition || evidence.Absent {
		return refusal("lifecycle.state", "the provider host evidence proves a settled state, not a partial one", "")
	}
	present := evidence.Pool != ""
	for _, network := range evidence.Networks {
		if !network.Managed || (network.State == "" && !network.Owned) {
			continue
		}
		if !network.Owned {
			return refusal("lifecycle.state", "a managed libvirt network exists without this context's ownership", "")
		}
		present = true
	}
	if !present {
		return refusal("lifecycle.state", "the provider host evidence reports nothing this context owns", "")
	}
	return nil
}

// ValidateMachinePresence accepts evidence only when it proves the frozen
// domain is defined and owned, every disk is present at its frozen size, the
// controller unit runs the pinned image and its ComputerSystem answers.
func ValidateMachinePresence(data []byte, request MachineRequest, digest string) error {
	evidence, err := decodeMachineEvidence(data, digest)
	if err != nil {
		return err
	}
	if !evidence.Postcondition || evidence.Absent {
		return refusal("lifecycle.state", "the machine adapter did not prove its postcondition", "")
	}
	if evidence.Domain != request.Domain {
		return refusal("lifecycle.state", "the machine evidence names another domain", "")
	}
	if !evidence.Owned {
		return refusal("lifecycle.state", "the realized domain does not carry this context's ownership", "")
	}
	if evidence.Unit != "active" {
		return refusal("lifecycle.state", "the machine's management controller unit is not active", "")
	}
	if evidence.Controller != request.Controller.Image {
		return refusal("lifecycle.state", "the running management controller is not the frozen image", "")
	}
	if evidence.System != request.UUID {
		return refusal("lifecycle.state", "the management controller does not expose this machine's system", "")
	}
	if evidence.Power == "" {
		return refusal("lifecycle.state", "the management controller reported no power state", "")
	}
	return matchDisks(evidence.Disks, request.Disks)
}

func matchDisks(observed []DiskEvidence, frozen []Disk) error {
	if len(observed) != len(frozen) {
		return refusal("lifecycle.state", "the machine evidence does not cover every frozen disk", "")
	}
	sorted := slices.Clone(observed)
	slices.SortFunc(sorted, func(x, y DiskEvidence) int { return strings.Compare(x.Name, y.Name) })
	expected := slices.Clone(frozen)
	slices.SortFunc(expected, func(x, y Disk) int { return strings.Compare(x.Name, y.Name) })
	for index, disk := range expected {
		entry := sorted[index]
		if entry.Name != disk.Name || !entry.Present {
			return refusal("lifecycle.state", "a frozen machine disk is missing", "")
		}
		if entry.SizeGiB != disk.SizeGiB {
			return refusal("lifecycle.state", "a machine disk is not the size the profile froze", "")
		}
	}
	return nil
}

// ValidateMachineAbsence accepts evidence only when it positively proves the
// domain, its disks and the controller are gone. An empty domain proves the
// domain gone only when the hypervisor answered for it.
func ValidateMachineAbsence(data []byte, digest string) error {
	evidence, err := decodeMachineEvidence(data, digest)
	if err != nil {
		return err
	}
	if !evidence.Postcondition || !evidence.Absent {
		return refusal("lifecycle.state", "the machine adapter did not prove removal", "")
	}
	if !evidence.Answered {
		return refusal("lifecycle.state", "the hypervisor did not answer for the machine's domain, so its removal is not proved", "")
	}
	if evidence.Domain != "" || evidence.Unit != "" || evidence.Controller != "" || evidence.System != "" || evidence.Power != "" || evidence.State != "" {
		return refusal("lifecycle.state", "the machine removal evidence still reports an owned resource", "")
	}
	for _, disk := range evidence.Disks {
		if disk.Present {
			return refusal("lifecycle.state", "the machine removal evidence still reports a disk", "")
		}
	}
	return nil
}

// ValidateMachinePartial accepts evidence only when it positively proves this
// context's own machine is part way realized: its domain, controller unit or
// one of its disks is present while the whole is not. A same-name domain
// without this context's ownership is foreign and is never converged, so it
// leaves the effect unknown rather than failed.
func ValidateMachinePartial(data []byte, digest string) error {
	evidence, err := decodeMachineEvidence(data, digest)
	if err != nil {
		return err
	}
	if evidence.Postcondition || evidence.Absent {
		return refusal("lifecycle.state", "the machine evidence proves a settled state, not a partial one", "")
	}
	if evidence.Domain != "" && !evidence.Owned {
		return refusal("lifecycle.state", "a domain of the same name exists without this context's ownership", "")
	}
	present := evidence.Domain != "" || evidence.Unit != "" || evidence.Controller != ""
	for _, disk := range evidence.Disks {
		present = present || disk.Present
	}
	if !present {
		return refusal("lifecycle.state", "the machine evidence reports nothing this context owns", "")
	}
	return nil
}

func decodeHostEvidence(data []byte, digest string) (HostEvidence, error) {
	var evidence HostEvidence
	if err := decodeEvidence(data, &evidence, "provider host"); err != nil {
		return HostEvidence{}, err
	}
	if evidence.Request != digest {
		return HostEvidence{}, refusal("lifecycle.state", "the provider host evidence names another request", "")
	}
	return evidence, nil
}

func decodeMachineEvidence(data []byte, digest string) (MachineEvidence, error) {
	var evidence MachineEvidence
	if err := decodeEvidence(data, &evidence, "machine"); err != nil {
		return MachineEvidence{}, err
	}
	if evidence.Request != digest {
		return MachineEvidence{}, refusal("lifecycle.state", "the machine evidence names another request", "")
	}
	return evidence, nil
}

func decodeEvidence(data []byte, target any, subject string) error {
	if len(data) == 0 || len(data) > maxEvidenceBytes {
		return refusal("lifecycle.state", "the "+subject+" adapter returned no bounded evidence", "")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return refusal("lifecycle.state", "the "+subject+" adapter returned malformed evidence", "")
	}
	if decoder.More() {
		return refusal("lifecycle.state", "the "+subject+" adapter returned trailing evidence", "")
	}
	return nil
}
