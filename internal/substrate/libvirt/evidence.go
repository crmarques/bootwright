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
	Absent bool `json:"absent"`
	// Directory is whether anything exists at the pool directory's path, read
	// whether or not the URI answered. Evidence without it proves the
	// directory neither present nor absent.
	Directory  *bool             `json:"directory"`
	Hypervisor bool              `json:"hypervisor"`
	Networks   []NetworkEvidence `json:"networks"`
	Pool       string            `json:"pool"`
	// PoolAnswered is whether the storage driver answered for the pool: it
	// returned the pool, or completed a listing that does not name it. The
	// URI answering proves only that the hypervisor did, and a storage driver
	// that is silent reports no pool either, so an empty Pool means absent
	// only while PoolAnswered is true. Evidence without it decodes as silent.
	PoolAnswered  bool              `json:"poolAnswered"`
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
	// Answered is whether the network driver answered for a managed network:
	// it returned the definition, or completed a listing that does not name
	// it. A network driver that is silent reports no state and no ownership
	// either, so a managed network without state is absent only while
	// Answered is true. An external network is never read, so it is never
	// answered for. Evidence without the field decodes as silent.
	Answered bool `json:"answered"`
	Bridge   bool `json:"bridge"`
	// Definition is whether a managed network runs, and keeps for its next
	// start, everything its frozen entry sets. Defining an active network
	// changes only the definition it next starts from, so a redefinition is
	// proved only once both carry it. Evidence without the field decodes as a
	// definition not proved.
	Definition bool   `json:"definition"`
	Managed    bool   `json:"managed"`
	Name       string `json:"name"`
	Owned      bool   `json:"owned"`
	State      string `json:"state"`
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
	Answered   bool           `json:"answered"`
	Controller string         `json:"controller"`
	Disks      []DiskEvidence `json:"disks"`
	Domain     string         `json:"domain"`
	// Listener is whether anything listens on the controller's socket. Alone
	// it is not proved to be this Machine's, so only absence reads it: a
	// removal releases the socket only once nothing listens on it. Evidence
	// without it proves the socket neither held nor free.
	Listener      *bool  `json:"listener"`
	Owned         bool   `json:"owned"`
	Postcondition bool   `json:"postcondition"`
	Power         string `json:"power"`
	Request       string `json:"request"`
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
// answering, every managed network owned, active and carrying its frozen
// definition, every external bridge present, and the pool active.
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
	if !evidence.PoolAnswered || evidence.Pool != "active" {
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

// pairNetworks orders the observed networks as the frozen request orders its
// own, and requires exactly one entry of the same name and management for each.
func pairNetworks(observed []NetworkEvidence, frozen []Network) ([]NetworkEvidence, error) {
	if len(observed) != len(frozen) {
		return nil, refusal("lifecycle.state", "the provider host evidence does not cover every declared network", "")
	}
	sorted := slices.Clone(observed)
	slices.SortFunc(sorted, func(x, y NetworkEvidence) int { return strings.Compare(x.Name, y.Name) })
	for index, network := range frozen {
		if sorted[index].Name != network.Name || sorted[index].Managed != network.Managed {
			return nil, refusal("lifecycle.state", "the provider host evidence does not match its frozen networks", "")
		}
	}
	return sorted, nil
}

func matchNetworks(observed []NetworkEvidence, frozen []Network) error {
	sorted, err := pairNetworks(observed, frozen)
	if err != nil {
		return err
	}
	for index, network := range frozen {
		entry := sorted[index]
		if !entry.Bridge {
			return refusal("lifecycle.state", "a declared network bridge is not present on the provider host", "")
		}
		if !network.Managed {
			continue
		}
		if !entry.Answered || entry.State != "active" {
			return refusal("lifecycle.state", "a managed libvirt network is not active", "")
		}
		if !entry.Owned {
			return refusal("lifecycle.state", "a managed libvirt network does not carry this context's ownership", "")
		}
		if !entry.Definition {
			return refusal("lifecycle.state", "a managed libvirt network does not carry its frozen definition", "")
		}
	}
	return nil
}

// ValidateHostAbsence accepts evidence only when it positively proves that
// every owned network, the pool and its directory are gone: the networks and
// pool through a URI that answered and each through the driver that owns it,
// because a connection or a driver that does not answer reports none of them
// either, and the directory by its path. Packages, foreign networks and
// external bridges are never this block's to remove, so nothing about them
// refuses it.
func ValidateHostAbsence(data []byte, digest string) error {
	evidence, err := decodeHostEvidence(data, digest)
	if err != nil {
		return err
	}
	if !evidence.Postcondition || !evidence.Absent {
		return refusal("lifecycle.state", "the provider host adapter did not prove removal", "")
	}
	if !evidence.URI {
		return refusal("lifecycle.state", "the provider host's hypervisor did not answer, so its removal is not proved", "")
	}
	if evidence.Directory == nil || *evidence.Directory {
		return refusal("lifecycle.state", "the provider host removal evidence does not prove its pool directory gone", "")
	}
	if !evidence.PoolAnswered {
		return refusal("lifecycle.state", "the provider host's storage driver did not answer for its pool, so its removal is not proved", "")
	}
	if evidence.Pool != "" {
		return refusal("lifecycle.state", "the provider host removal evidence still reports its pool", "")
	}
	for _, network := range evidence.Networks {
		if !network.Managed {
			continue
		}
		if !network.Answered {
			return refusal("lifecycle.state", "the provider host's network driver did not answer for a managed network, so its removal is not proved", "")
		}
		if network.State != "" || network.Owned {
			return refusal("lifecycle.state", "the provider host removal evidence still reports a managed network", "")
		}
	}
	return nil
}

// ValidateHostUnremoved accepts observed evidence only when it proves a
// removal of the frozen request took nothing back: the presence form, through
// a URI that answered, reporting every managed network its driver answered for
// with this context's ownership and active, the pool its driver answered for
// active, and the pool directory present. The removal stops each network and
// the pool before it undefines it, so a stopped one may be its first effect
// and is never read as none. The hypervisor closure, the driver daemons,
// whether a declared bridge exists and whether a network carries its frozen
// definition are what the apply proves, not anything a removal takes back, so
// none plays any part.
func ValidateHostUnremoved(data []byte, request HostRequest, digest string) error {
	evidence, err := decodeHostRemains(data, digest)
	if err != nil {
		return err
	}
	return unremoved(evidence, request)
}

// ValidateHostRemovalUnfinished accepts observed evidence only when it proves
// a removal of the frozen request took back part of what it owns and not the
// rest: the presence form reporting the pool, its directory or one of this
// context's managed networks, without the whole that ValidateHostUnremoved
// reads. The adapter's postcondition is the apply's, which leaves the pool
// directory out, so a host holding everything else carries it proved while
// its directory is gone; like the rest of what the apply proves it plays no
// part here. A managed network defined without this context's ownership is
// foreign and leaves the effect unknown.
func ValidateHostRemovalUnfinished(data []byte, request HostRequest, digest string) error {
	evidence, err := decodeHostRemains(data, digest)
	if err != nil {
		return err
	}
	if err := holdsOwned(evidence); err != nil {
		return err
	}
	if unremoved(evidence, request) == nil {
		return refusal("lifecycle.state", "the provider host still holds everything its removal takes back", "")
	}
	return nil
}

// decodeHostRemains decodes observed evidence for this request in its
// presence form, the only form that reports what a removal has still to take
// back.
func decodeHostRemains(data []byte, digest string) (HostEvidence, error) {
	evidence, err := decodeHostEvidence(data, digest)
	if err != nil {
		return HostEvidence{}, err
	}
	if evidence.Absent {
		return HostEvidence{}, refusal("lifecycle.state", "the provider host evidence reports a removal, not what remains", "")
	}
	return evidence, nil
}

func unremoved(evidence HostEvidence, request HostRequest) error {
	if !evidence.URI {
		return refusal("lifecycle.state", "the declared libvirt connection does not answer", "")
	}
	if !evidence.PoolAnswered || evidence.Pool != "active" || evidence.Directory == nil || !*evidence.Directory {
		return refusal("lifecycle.state", "the provider host no longer holds its active pool and its directory", "")
	}
	sorted, err := pairNetworks(evidence.Networks, request.Networks)
	if err != nil {
		return err
	}
	for _, entry := range sorted {
		if entry.Managed && (!entry.Answered || !entry.Owned || entry.State != "active") {
			return refusal("lifecycle.state", "the provider host no longer holds every managed network it owns active", "")
		}
	}
	return nil
}

// ValidateHostPartial accepts evidence only when it positively proves this
// context's own provider host is part way realized: its pool, its pool
// directory or one of its owned managed networks is present while the whole
// is not. The directory is read by its path, so it counts whether or not the
// URI answered, and its path is this context's own reservation. A managed
// network the hypervisor defines without this context's ownership is foreign,
// so it proves nothing here and leaves the effect unknown. The hypervisor
// closure is shared host software this block never removes, so its presence
// alone is not a partial realization.
func ValidateHostPartial(data []byte, digest string) error {
	evidence, err := decodeHostEvidence(data, digest)
	if err != nil {
		return err
	}
	if evidence.Postcondition || evidence.Absent {
		return refusal("lifecycle.state", "the provider host evidence proves a settled state, not a partial one", "")
	}
	return holdsOwned(evidence)
}

// holdsOwned requires the evidence to report the pool, its directory or one of
// this context's managed networks, and no managed network defined without
// this context's ownership.
func holdsOwned(evidence HostEvidence) error {
	present := evidence.Pool != "" || (evidence.Directory != nil && *evidence.Directory)
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
// domain, its disks and the controller are gone and nothing listens on the
// controller's socket, so the removal can release that reservation. An empty
// domain proves the domain gone only when the hypervisor answered for it.
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
	if evidence.Listener == nil || *evidence.Listener {
		return refusal("lifecycle.state", "the machine removal evidence does not prove its controller socket free", "")
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
	if len(bytes.Trim(data[decoder.InputOffset():], " \t\r\n")) != 0 {
		return refusal("lifecycle.state", "the "+subject+" adapter returned trailing evidence", "")
	}
	return nil
}
