package providedhostname

import (
	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/reconciliation"
)

const (
	Implementation  = "os-provided-hostname-v1"
	Version         = "provided-os-hostname-request-v1"
	EvidenceVersion = "provided-os-hostname-evidence-v1"
	WorkerRule      = "static-and-kernel-hostname-proof-v1"
)

type Identity struct {
	Block   string `json:"block"`
	Context string `json:"context"`
	Object  string `json:"object"`
}

type Request struct {
	Hostname  string            `json:"hostname"`
	Identity  Identity          `json:"identity"`
	Placement machine.Placement `json:"placement"`
	Version   string            `json:"version"`
}

func blockID(name string) string {
	return reconciliation.BlockIdentity("os-hostname-", name)
}

func ContentDigest() string {
	return reconciliation.ContentDigestOf(Implementation, Version, EvidenceVersion, WorkerRule)
}

func decodeRequest(data []byte) (Request, error) {
	request, err := reconciliation.ThawVersion[Request](data, "provided OS hostname", Version)
	if err != nil {
		return Request{}, err
	}
	identity, placement := request.Identity, request.Placement
	if request.Version != Version || identity.Context == "" || identity.Object == "" || identity.Block != blockID(identity.Object) ||
		!placement.PinnedSSH() || placement.Machine != identity.Object || !api.ValidLexical(api.LexicalDNS, request.Hostname) {
		return Request{}, requestInvalid.Fail("the frozen provided OS hostname does not name a DNS hostname and a pinned SSH target", "")
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
		return Request{}, requestInvalid.Fail("the provided OS hostname request names another block or Machine", "")
	}
	return request, nil
}
