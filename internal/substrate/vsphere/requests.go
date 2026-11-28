package vsphere

import (
	"slices"

	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/substrate"
)

type Identity struct {
	Block   string `json:"block"`
	Context string `json:"context"`
	Object  string `json:"object"`
}

type Interface struct {
	MACAddress  string `json:"macAddress"`
	MTU         int64  `json:"mtu"`
	Name        string `json:"name"`
	Portgroup   string `json:"portgroup"`
	TaggedVLANs []int  `json:"taggedVLANs,omitempty"`
}

type DataDisk struct {
	Name    string `json:"name"`
	SizeGiB int64  `json:"sizeGiB"`
}

type Request struct {
	ComputeCluster   string            `json:"computeCluster"`
	CPU              int64             `json:"cpu"`
	CredentialsRef   string            `json:"credentialsRef"`
	DataDisks        []DataDisk        `json:"dataDisks"`
	Datacenter       string            `json:"datacenter"`
	Datastore        string            `json:"datastore"`
	DiskGiB          int64             `json:"diskGiB"`
	Folder           string            `json:"folder"`
	Identity         Identity          `json:"identity"`
	Interfaces       []Interface       `json:"interfaces"`
	MemoryMiB        int64             `json:"memoryMiB"`
	Name             string            `json:"name"`
	Owner            string            `json:"owner"`
	Placement        machine.Placement `json:"placement"`
	Provider         string            `json:"provider"`
	ResourcePool     string            `json:"resourcePool"`
	Server           string            `json:"server"`
	StagingDatastore string            `json:"stagingDatastore"`
	StagingFolder    string            `json:"stagingFolder"`
	Template         string            `json:"template,omitempty"`
	TLSVerify        bool              `json:"tlsVerify"`
	TPM              bool              `json:"tpm"`
	TrustBundleRef   string            `json:"trustBundleRef,omitempty"`
	Version          string            `json:"version"`
}

func (r Request) Canonical() ([]byte, error) { return reconciliation.Freeze(r, "machine") }

func DecodeRequest(data []byte) (Request, error) {
	return reconciliation.ThawVersion[Request](data, "machine", requestVersion)
}

func (r Request) Controller() substrate.Controller {
	media := substrate.VSphereMedia{Datacenter: r.Datacenter, Datastore: r.StagingDatastore, Folder: r.StagingFolder, Name: r.Name, Owner: r.Owner}
	return substrate.Controller{Endpoint: r.Server, CredentialsRef: r.CredentialsRef, TrustBundleRef: r.TrustBundleRef, TLSVerify: r.TLSVerify, MediaVolume: media.Volume()}
}

func (r Request) SecretReferences() []string {
	refs := append(r.Placement.SecretReferences(), r.CredentialsRef)
	if r.TrustBundleRef != "" {
		refs = append(refs, r.TrustBundleRef)
	}
	slices.Sort(refs)
	return slices.Compact(refs)
}
