package kubevirt

import (
	"net/url"
	"slices"

	"github.com/crmarques/bootwright/internal/reconciliation"
)

const maxEvidenceBytes = 64 << 10

// HostEvidence is what one provider proof read: the server and cluster the
// kubeconfig reached, the root class and upload proxy the Machines use, and
// each check that failed or could not be read.
type HostEvidence struct {
	Absent        bool              `json:"absent"`
	Failures      []string          `json:"failures"`
	KubeSystemUID string            `json:"kubeSystemUID"`
	MACPool       string            `json:"macPool"`
	Networks      []NetworkEvidence `json:"networks"`
	Postcondition bool              `json:"postcondition"`
	ReadWriteMany bool              `json:"readWriteMany"`
	Request       string            `json:"request"`
	Server        string            `json:"server"`
	StorageClass  string            `json:"storageClass"`
	UploadProxy   string            `json:"uploadProxy"`
	Version       string            `json:"version"`
}

type NetworkEvidence struct {
	Attachment string `json:"attachment"`
	Present    bool   `json:"present"`
}

// Receipt is one object this block owns: the UID its create reply returned or
// a read recovered, and how ownership was established (created, uid-matched,
// mark-recovered, or owner-referenced for what the VirtualMachine created).
type Receipt struct {
	Basis string `json:"basis"`
	Kind  string `json:"kind"`
	Name  string `json:"name"`
	UID   string `json:"uid"`
}

// VolumeEvidence is one DataVolume or claim the VirtualMachine owns, by kind
// and name, and whether a read that answered still found it.
type VolumeEvidence struct {
	Kind    string `json:"kind"`
	Name    string `json:"name"`
	Present bool   `json:"present"`
}

// MachineEvidence is one observation of a VirtualMachine. Answered says every
// read was answered: without it nothing here proves absence. Owned is a
// present VirtualMachine this block's receipt or mark proves its own; Foreign
// a same-named one that is not.
type MachineEvidence struct {
	Absent         bool             `json:"absent"`
	Answered       bool             `json:"answered"`
	Foreign        bool             `json:"foreign"`
	Instance       string           `json:"instance"`
	KubeSystemUID  string           `json:"kubeSystemUID"`
	Owned          bool             `json:"owned"`
	Postcondition  bool             `json:"postcondition"`
	Receipts       []Receipt        `json:"receipts"`
	Request        string           `json:"request"`
	Server         string           `json:"server"`
	VirtualMachine string           `json:"virtualMachine"`
	Volumes        []VolumeEvidence `json:"volumes"`
}

// ValidateHostPresence accepts a proof only when every check held: the host
// answered as OpenShift Virtualization with a cluster identity, the root class
// can be migrated, the upload proxy is published and every network is
// present.
func ValidateHostPresence(data []byte, request HostRequest, digest string) error {
	evidence, err := decodeHostEvidence(data, digest)
	if err != nil {
		return err
	}
	switch {
	case evidence.Absent:
		return refusal("lifecycle.state", "the KubeVirt provider evidence reports its proof released, not proved", "")
	case len(evidence.Failures) > 0:
		return refusal("lifecycle.state", "the KubeVirt provider "+request.Identity.Object+" was not proved: "+evidence.Failures[0],
			"correct the host or spec.kubevirt on InfraProvider/"+request.Identity.Object+", then run the apply again")
	case !evidence.Postcondition:
		return refusal("lifecycle.state", "the KubeVirt provider proof did not prove its postcondition", "")
	case !origin(evidence.Server) || evidence.KubeSystemUID == "":
		return refusal("lifecycle.state", "the KubeVirt provider proof names no server or cluster identity", "")
	case evidence.StorageClass == "" || !evidence.ReadWriteMany:
		return refusal("lifecycle.state", "the KubeVirt provider proof names no migratable root class", "")
	case evidence.UploadProxy == "":
		return refusal("lifecycle.state", "the KubeVirt provider proof names no upload proxy", "")
	case len(evidence.Networks) != len(request.Networks):
		return refusal("lifecycle.state", "the KubeVirt provider proof does not cover every network", "")
	}
	for index, network := range request.Networks {
		if evidence.Networks[index].Attachment != network.Attachment || !evidence.Networks[index].Present {
			return refusal("lifecycle.state", "the KubeVirt provider proof does not prove attachment "+network.Attachment, "")
		}
	}
	return nil
}

// ValidateHostAbsence accepts only the released proof: a removal that read
// nothing and took nothing back.
func ValidateHostAbsence(data []byte, digest string) error {
	evidence, err := decodeHostEvidence(data, digest)
	if err != nil {
		return err
	}
	if !evidence.Absent || !evidence.Postcondition {
		return refusal("lifecycle.state", "the KubeVirt provider adapter did not publish its proof released", "")
	}
	return nil
}

// ValidateMachinePresence accepts evidence only when it proves the frozen
// VirtualMachine present, owned by a receipt carrying its UID, its root
// DataVolume present, and the cluster identity the proof recorded.
func ValidateMachinePresence(data []byte, request MachineRequest, digest string) error {
	evidence, err := decodeMachineEvidence(data, digest)
	if err != nil {
		return err
	}
	switch {
	case !evidence.Answered:
		return refusal("lifecycle.state", "the host cluster did not answer for the VirtualMachine", "")
	case !evidence.Postcondition || evidence.Absent:
		return refusal("lifecycle.state", "the KubeVirt machine adapter did not prove its postcondition", "")
	case evidence.VirtualMachine != request.Name:
		return refusal("lifecycle.state", "the KubeVirt machine evidence names another VirtualMachine", "")
	case !evidence.Owned || evidence.Foreign:
		return refusal("lifecycle.state", "the VirtualMachine "+request.Namespace+"/"+request.Name+" is not this context's", "")
	case !origin(evidence.Server) || evidence.KubeSystemUID == "":
		return refusal("lifecycle.state", "the KubeVirt machine evidence names no server or cluster identity", "")
	case receipt(evidence, "VirtualMachine", request.Name) == nil:
		return refusal("lifecycle.state", "the KubeVirt machine evidence records no receipt for its VirtualMachine", "")
	case !volumePresent(evidence, "DataVolume", request.RootVolume):
		return refusal("lifecycle.state", "the VirtualMachine's root DataVolume is missing", "")
	}
	return nil
}

// ValidateMachineAbsence accepts evidence only when a host that answered every
// read reports the VirtualMachine, its instance and every DataVolume and claim
// it owned gone.
func ValidateMachineAbsence(data []byte, digest string) error {
	evidence, err := decodeMachineEvidence(data, digest)
	if err != nil {
		return err
	}
	switch {
	case !evidence.Answered:
		return refusal("lifecycle.state", "the host cluster did not answer, so the VirtualMachine's removal is not proved", "")
	case !evidence.Absent || !evidence.Postcondition:
		return refusal("lifecycle.state", "the KubeVirt machine adapter did not prove removal", "")
	case evidence.VirtualMachine != "" || evidence.Instance != "" || evidence.Foreign:
		return refusal("lifecycle.state", "the KubeVirt removal evidence still reports the VirtualMachine", "")
	}
	read := 0
	for _, volume := range evidence.Volumes {
		if volume.Present {
			return refusal("lifecycle.state", "the KubeVirt removal evidence still reports "+volume.Kind+" "+volume.Name, "")
		}
		if volume.Kind == "DataVolume" {
			read++
		}
	}
	if read < 2 {
		return refusal("lifecycle.state", "the KubeVirt removal evidence did not read both DataVolumes", "")
	}
	return nil
}

// ValidateMachineUnremoved accepts observed evidence only when a removal took
// nothing back: the owned VirtualMachine and its root DataVolume are present.
func ValidateMachineUnremoved(data []byte, request MachineRequest, digest string) error {
	evidence, err := decodeMachineRemains(data, digest)
	if err != nil {
		return err
	}
	if !evidence.Answered || evidence.VirtualMachine != request.Name || !evidence.Owned ||
		!volumePresent(evidence, "DataVolume", request.RootVolume) {
		return refusal("lifecycle.state", "the VirtualMachine no longer holds everything its removal takes back", "")
	}
	return nil
}

// ValidateMachineRemovalUnfinished accepts observed evidence only when a
// removal took back part of what it owns: the owned VirtualMachine without its
// root, or no VirtualMachine but a DataVolume or claim it owned. A foreign
// same-named VirtualMachine leaves the effect unknown.
func ValidateMachineRemovalUnfinished(data []byte, request MachineRequest, digest string) error {
	evidence, err := decodeMachineRemains(data, digest)
	if err != nil {
		return err
	}
	if err := holds(evidence); err != nil {
		return err
	}
	if ValidateMachineUnremoved(data, request, digest) == nil {
		return refusal("lifecycle.state", "the VirtualMachine still holds everything its removal takes back", "")
	}
	return nil
}

// ValidateMachinePartial accepts evidence only when it proves this block's own
// VirtualMachine part way realized: owned, without everything the presence
// form needs.
func ValidateMachinePartial(data []byte, digest string) error {
	evidence, err := decodeMachineEvidence(data, digest)
	if err != nil {
		return err
	}
	if evidence.Postcondition || evidence.Absent {
		return refusal("lifecycle.state", "the KubeVirt machine evidence proves a settled state, not a partial one", "")
	}
	return holds(evidence)
}

func holds(evidence MachineEvidence) error {
	if !evidence.Answered {
		return refusal("lifecycle.state", "the host cluster did not answer for the VirtualMachine", "")
	}
	if evidence.Foreign {
		return refusal("lifecycle.state", "a VirtualMachine of the same name exists that is not this context's", "")
	}
	if !evidence.Owned && !slices.ContainsFunc(evidence.Volumes, func(volume VolumeEvidence) bool { return volume.Present }) {
		return refusal("lifecycle.state", "the KubeVirt machine evidence reports nothing this context owns", "")
	}
	return nil
}

func decodeMachineRemains(data []byte, digest string) (MachineEvidence, error) {
	evidence, err := decodeMachineEvidence(data, digest)
	if err != nil {
		return MachineEvidence{}, err
	}
	if evidence.Absent {
		return MachineEvidence{}, refusal("lifecycle.state", "the KubeVirt machine evidence reports a removal, not what remains", "")
	}
	return evidence, nil
}

func receipt(evidence MachineEvidence, kind, name string) *Receipt {
	for index, held := range evidence.Receipts {
		if held.Kind == kind && held.Name == name && held.UID != "" {
			return &evidence.Receipts[index]
		}
	}
	return nil
}

func volumePresent(evidence MachineEvidence, kind, name string) bool {
	return slices.ContainsFunc(evidence.Volumes, func(volume VolumeEvidence) bool {
		return volume.Kind == kind && volume.Name == name && volume.Present
	})
}

// origin reports whether a recorded API server is one https origin: a scheme,
// a host and an optional port, with no path, query or user information.
func origin(value string) bool {
	parsed, err := url.Parse(value)
	return err == nil && parsed.Scheme == "https" && parsed.Host != "" && parsed.User == nil &&
		(parsed.Path == "" || parsed.Path == "/") && parsed.RawQuery == "" && parsed.Fragment == ""
}

func decodeHostEvidence(data []byte, digest string) (HostEvidence, error) {
	evidence, err := reconciliation.DecodeEvidence[HostEvidence](data, maxEvidenceBytes, "KubeVirt provider")
	if err != nil {
		return HostEvidence{}, err
	}
	if evidence.Request != digest {
		return HostEvidence{}, refusal("lifecycle.state", "the KubeVirt provider evidence names another request", "")
	}
	return evidence, nil
}

func decodeMachineEvidence(data []byte, digest string) (MachineEvidence, error) {
	evidence, err := reconciliation.DecodeEvidence[MachineEvidence](data, maxEvidenceBytes, "KubeVirt machine")
	if err != nil {
		return MachineEvidence{}, err
	}
	if evidence.Request != digest {
		return MachineEvidence{}, refusal("lifecycle.state", "the KubeVirt machine evidence names another request", "")
	}
	return evidence, nil
}
