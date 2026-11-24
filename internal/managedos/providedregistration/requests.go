package providedregistration

import (
	"net/url"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/reconciliation"
)

const (
	Implementation  = "os-provided-registration-v1"
	Version         = "provided-os-registration-request-v1"
	EvidenceVersion = "provided-os-registration-evidence-v1"
	WorkerRule      = "converge-existing-consumer-onto-declared-satellite-v1"
)

type Identity struct {
	Block   string `json:"block"`
	Context string `json:"context"`
	Object  string `json:"object"`
}

type Satellite struct {
	ContentBaseURL string `json:"contentBaseURL"`
	Hostname       string `json:"hostname"`
	TrustBundleRef string `json:"trustBundleRef"`
}

type Request struct {
	EntitlementRef string            `json:"entitlementRef"`
	Identity       Identity          `json:"identity"`
	Placement      machine.Placement `json:"placement"`
	Satellite      Satellite         `json:"satellite"`
	Version        string            `json:"version"`
}

func blockID(name string) string {
	return reconciliation.BlockIdentity("os-registration-", name)
}

func ContentDigest() string {
	return reconciliation.ContentDigestOf(Implementation, Version, EvidenceVersion, WorkerRule)
}

func decodeRequest(data []byte) (Request, error) {
	request, err := reconciliation.ThawVersion[Request](data, "provided registration", Version)
	if err != nil {
		return Request{}, err
	}
	identity, placement, satellite := request.Identity, request.Placement, request.Satellite
	if identity.Context == "" || identity.Object == "" || identity.Block != blockID(identity.Object) ||
		!placement.PinnedSSH() || placement.Machine != identity.Object || request.EntitlementRef == "" ||
		satellite.Hostname == "" || satellite.TrustBundleRef == "" || !httpsRoot(satellite.ContentBaseURL) {
		return Request{}, requestInvalid.Fail("the frozen provided registration does not name an Entitlement, a Satellite with its CA bundle and a pinned SSH target", "")
	}
	return request, nil
}

func httpsRoot(raw string) bool {
	parsed, err := url.Parse(raw)
	return err == nil && parsed.Scheme == "https" && parsed.Host != "" && parsed.User == nil && parsed.RawQuery == "" && parsed.Fragment == ""
}

func decodeBlock(block reconciliation.Block) (Request, error) {
	request, err := decodeRequest(block.Request)
	if err != nil {
		return Request{}, err
	}
	if block.Kind != string(api.Machine) || block.Object != request.Identity.Object ||
		block.ID != request.Identity.Block || block.Implementation != Implementation || request.Version != Version {
		return Request{}, requestInvalid.Fail("the provided registration request names another block or Machine", "")
	}
	return request, nil
}
