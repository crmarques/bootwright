package providedtime

import (
	"slices"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/reconciliation"
)

const (
	Implementation  = "os-provided-time-v1"
	Version         = "provided-os-time-request-v1"
	EvidenceVersion = "provided-os-time-evidence-v1"
	WorkerRule      = "chrony-servers-record-restore-ntp-synchronized-proof-v1"
)

type Identity struct {
	Block   string `json:"block"`
	Context string `json:"context"`
	Object  string `json:"object"`
}

type Request struct {
	Identity  Identity          `json:"identity"`
	Placement machine.Placement `json:"placement"`
	Servers   []string          `json:"servers"`
	Version   string            `json:"version"`
}

func blockID(name string) string {
	return reconciliation.BlockIdentity("os-time-", name)
}

func ContentDigest() string {
	return reconciliation.ContentDigestOf(Implementation, Version, EvidenceVersion, WorkerRule)
}

func decodeRequest(data []byte) (Request, error) {
	request, err := reconciliation.ThawVersion[Request](data, "provided time synchronization", Version)
	if err != nil {
		return Request{}, err
	}
	identity, placement := request.Identity, request.Placement
	if request.Version != Version || identity.Context == "" || identity.Object == "" || identity.Block != blockID(identity.Object) ||
		!placement.PinnedSSH() || placement.Machine != identity.Object || len(request.Servers) == 0 || !validServers(request.Servers) {
		return Request{}, requestInvalid.Fail("the frozen provided time synchronization does not name distinct time server addresses and a pinned SSH target", "")
	}
	return request, nil
}

func validServers(servers []string) bool {
	for index, server := range servers {
		if !api.ValidLexical(api.LexicalHost, server) || slices.Contains(servers[:index], server) {
			return false
		}
	}
	return true
}

func decodeBlock(block reconciliation.Block) (Request, error) {
	request, err := decodeRequest(block.Request)
	if err != nil {
		return Request{}, err
	}
	if block.Kind != string(api.Machine) || block.Object != request.Identity.Object ||
		block.ID != request.Identity.Block || block.Implementation != Implementation {
		return Request{}, requestInvalid.Fail("the provided time synchronization request names another block or Machine", "")
	}
	return request, nil
}
