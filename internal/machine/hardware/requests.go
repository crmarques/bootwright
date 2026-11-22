package hardware

import (
	api "github.com/crmarques/bootwright/api/v1alpha1"
	machineref "github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/secrets"
)

const (
	Kind            = string(api.Machine)
	Family          = machineref.HardwareFamily
	Implementation  = "machine-hardware-v1"
	EvidenceVersion = "machine-hardware-evidence-v1"
	ClaimKind       = "machine-hardware"
	AddressRule     = "permanent-hardware-address-v1"
	ClaimRule       = "unqualified-bmc-mac-claim"
	MaxAddresses    = 256
	UnclaimedServer = "substrate.unclaimed-server"
	variable        = "bootwright_machine_hardware"
)

type Identity struct {
	Block   string `json:"block"`
	Context string `json:"context"`
	Object  string `json:"object"`
}

type Request struct {
	Bootstrap *machineref.Bootstrap `json:"bootstrap,omitempty"`
	Identity  Identity              `json:"identity"`
	Placement machineref.Placement  `json:"placement"`
	Version   string                `json:"version"`
}

func BlockID(machine string) string {
	return reconciliation.BlockIdentity("machine-hardware-", machine)
}

func ContentDigest() string {
	return reconciliation.ContentDigestOf("bootwright.machine.hardware-v1", Implementation, EvidenceVersion, AddressRule, ClaimRule)
}

func Requests(catalog api.Catalog, controllerMachine, contextName string) ([]Request, error) {
	placements, err := machineref.HardwareHosts(catalog, controllerMachine)
	if err != nil {
		return nil, err
	}
	requests := make([]Request, 0, len(placements))
	for _, placement := range placements {
		var access *machineref.Bootstrap
		if object, ok := catalog.Find(api.Machine, placement.Machine); ok {
			if value, managed := machineref.BootstrapOf(object); managed {
				access = &value
			}
		}
		requests = append(requests, Request{Bootstrap: access,
			Identity:  Identity{Block: BlockID(placement.Machine), Context: contextName, Object: placement.Machine},
			Placement: placement, Version: Implementation,
		})
	}
	return requests, nil
}

func (r Request) Canonical() ([]byte, error) { return reconciliation.Freeze(r, "machine hardware") }

func DecodeRequest(data []byte) (Request, error) {
	request, err := reconciliation.ThawVersion[Request](data, "machine hardware", Implementation)
	if err != nil {
		return Request{}, err
	}
	placement := request.Placement
	if request.Identity.Object == "" || request.Identity.Context == "" || request.Identity.Block != BlockID(request.Identity.Object) ||
		placement.Connection != machineref.ConnectionSSH || placement.Machine != request.Identity.Object || placement.Address == "" ||
		placement.KnownHostsRef == "" || placement.PrivateKeyRef == "" || request.Bootstrap != nil && !request.Bootstrap.Valid(placement.User) {
		return Request{}, hardwareRequestInvalid.Fail("the frozen machine hardware request names no Machine reached over SSH", "destroy it with the build that applied it")
	}
	return request, nil
}

func (r Request) SecretReferences() []string {
	if r.Bootstrap == nil {
		return r.Placement.SecretReferences()
	}
	return []string{r.Placement.KnownHostsRef, r.Placement.PrivateKeyRef, r.Bootstrap.AuthRef}
}

func (r Request) SecretClasses() secrets.Classes {
	if r.Bootstrap == nil {
		return r.Placement.SecretClasses()
	}
	return secrets.Classes{}.Add(secrets.AccessCredential, r.Bootstrap.AuthRef, r.Placement.PrivateKeyRef).Add(secrets.TrustAnchor, r.Placement.KnownHostsRef)
}
