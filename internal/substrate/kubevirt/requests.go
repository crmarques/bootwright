package kubevirt

import (
	machineref "github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/reconciliation"
)

// Identity names the block, context and object one request belongs to.
type Identity struct {
	Block   string `json:"block"`
	Context string `json:"context"`
	Object  string `json:"object"`
}

// Access is one permission the arm uses, proved by a SelfSubjectAccessReview
// in the provider's namespace.
type Access struct {
	Group       string `json:"group,omitempty"`
	Resource    string `json:"resource"`
	Subresource string `json:"subresource,omitempty"`
	Verb        string `json:"verb"`
}

// Network is the NetworkAttachmentDefinition one attachment reaches, and the
// ClusterUserDefinedNetwork that derives it, if any.
type Network struct {
	Attachment string `json:"attachment"`
	Derived    string `json:"derived,omitempty"`
	Name       string `json:"name"`
	Namespace  string `json:"namespace"`
}

// HostRequest is the complete frozen intent of one provider proof: the host's
// namespace, the root class the operator chose, if any, the permissions and
// networks to prove and the MACs to compare with KubeMacPool's range. It
// names the kubeconfig declaration, never its material.
type HostRequest struct {
	Access         []Access             `json:"access"`
	HostCluster    string               `json:"hostCluster,omitempty"`
	Identity       Identity             `json:"identity"`
	KubeconfigRef  string               `json:"kubeconfigRef,omitempty"`
	MACAddresses   []string             `json:"macAddresses"`
	Namespace      string               `json:"namespace"`
	Networks       []Network            `json:"networks"`
	Placement      machineref.Placement `json:"placement"`
	ServiceAccount string               `json:"serviceAccount,omitempty"`
	StorageClass   string               `json:"storageClass,omitempty"`
	Version        string               `json:"version"`
}

// Interface is one VirtualMachine interface on the bridge binding, with the
// MAC the realization derived and the Multus network name it attaches to.
type Interface struct {
	MACAddress string `json:"macAddress"`
	Name       string `json:"name"`
	Network    string `json:"network"`
}

// TPM is the vTPM a profile requests; Persistent keeps its state across a
// host-side stop and is frozen at creation.
type TPM struct {
	Persistent bool `json:"persistent"`
}

// MachineRequest is the complete frozen intent of one VirtualMachine and its
// root DataVolume. The root class is the one the provider proof reads when the
// provider names none.
type MachineRequest struct {
	CPU            int                  `json:"cpu"`
	DiskGiB        int                  `json:"diskGiB"`
	HostCluster    string               `json:"hostCluster,omitempty"`
	Identity       Identity             `json:"identity"`
	Interfaces     []Interface          `json:"interfaces"`
	KubeconfigRef  string               `json:"kubeconfigRef,omitempty"`
	MediaVolume    string               `json:"mediaVolume"`
	MemoryMiB      int                  `json:"memoryMiB"`
	Name           string               `json:"name"`
	Namespace      string               `json:"namespace"`
	Placement      machineref.Placement `json:"placement"`
	Provider       string               `json:"provider"`
	RootVolume     string               `json:"rootVolume"`
	ServiceAccount string               `json:"serviceAccount,omitempty"`
	Spread         string               `json:"spread,omitempty"`
	StorageClass   string               `json:"storageClass,omitempty"`
	TPM            *TPM                 `json:"tpm,omitempty"`
	Version        string               `json:"version"`
}

func (r HostRequest) Canonical() ([]byte, error) {
	return reconciliation.Freeze(r, "KubeVirt provider")
}

func (r MachineRequest) Canonical() ([]byte, error) {
	return reconciliation.Freeze(r, "KubeVirt machine")
}

func DecodeHostRequest(data []byte) (HostRequest, error) {
	return reconciliation.ThawVersion[HostRequest](data, "KubeVirt provider", hostRequestVersion)
}

func DecodeMachineRequest(data []byte) (MachineRequest, error) {
	return reconciliation.ThawVersion[MachineRequest](data, "KubeVirt machine", machineRequestVersion)
}

// SecretReferences names every declaration a request's execution needs bound.
func (r HostRequest) SecretReferences() []string { return references(r.Placement, r.KubeconfigRef) }

func (r MachineRequest) SecretReferences() []string { return references(r.Placement, r.KubeconfigRef) }

func references(placement machineref.Placement, kubeconfig string) []string {
	out := []string{}
	for _, reference := range append(placement.SecretReferences(), kubeconfig) {
		if reference != "" {
			out = append(out, reference)
		}
	}
	return out
}
