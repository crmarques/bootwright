package providedupdate

import (
	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/reconciliation"
)

const (
	Implementation  = "os-provided-update-v1"
	Version         = "provided-os-update-request-v1"
	EvidenceVersion = "provided-os-update-evidence-v1"
	WorkerRule      = "pinned-rhel-transaction-and-reboot-proof-v1"
)

type Identity struct {
	Block   string `json:"block"`
	Context string `json:"context"`
	Object  string `json:"object"`
}

type Request struct {
	Identity  Identity          `json:"identity"`
	Placement machine.Placement `json:"placement"`
	Release   string            `json:"release"`
	Version   string            `json:"version"`
}

func blockID(name string) string {
	return reconciliation.BlockIdentity("os-update-", name)
}

func ContentDigest() string {
	return reconciliation.ContentDigestOf(Implementation, Version, EvidenceVersion, WorkerRule)
}

func decodeRequest(data []byte) (Request, error) {
	request, err := reconciliation.ThawVersion[Request](data, "provided OS update", Version)
	if err != nil {
		return Request{}, err
	}
	identity, placement := request.Identity, request.Placement
	if identity.Context == "" || identity.Object == "" || identity.Block != blockID(identity.Object) ||
		!placement.PinnedSSH() || placement.Machine != identity.Object || !api.ValidLexical(api.LexicalOSRelease, request.Release) {
		return Request{}, requestInvalid.Fail("the frozen provided OS update does not name a release and a pinned SSH target", "")
	}
	return request, nil
}

func decodeBlock(block reconciliation.Block) (Request, error) {
	request, err := decodeRequest(block.Request)
	if err != nil {
		return Request{}, err
	}
	if block.Kind != string(api.Machine) || block.Object != request.Identity.Object ||
		block.ID != request.Identity.Block || block.Implementation != Implementation {
		return Request{}, requestInvalid.Fail("the provided OS update request names another block or Machine", "")
	}
	return request, nil
}
