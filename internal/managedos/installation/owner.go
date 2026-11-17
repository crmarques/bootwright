package installation

import (
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

func IsInstallation(implementation string) bool { return implementation == Implementation }

func RequestDigestOf(request Request) (string, error) {
	canonical, err := request.Canonical()
	if err != nil {
		return "", err
	}
	return reconciliation.RequestDigest(reconciliation.BlockDefinition{
		Kind: Kind, Object: request.Identity.Object, Implementation: Implementation, ContentDigest: ContentDigest(), Request: canonical,
	})
}

func OriginalInstall(proof lifecycle.BlockEvidence, machine string) bool {
	return OriginalApply(proof, machine, Implementation, BlockID(machine))
}

func OriginalApply(proof lifecycle.BlockEvidence, machine, implementation, id string) bool {
	digest, err := reconciliation.RequestDigest(proof.Block.BlockDefinition)
	return proof.Verb == reconciliation.Apply && proof.Kind == Kind && proof.Object == machine && proof.Implementation == implementation &&
		proof.Block.ID == id && proof.Block.Kind == Kind && proof.Block.Object == machine && proof.Block.Implementation == implementation &&
		err == nil && digest == proof.Block.RequestDigest
}
