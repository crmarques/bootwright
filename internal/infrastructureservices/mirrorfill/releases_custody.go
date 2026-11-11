package mirrorfill

import (
	"maps"

	"github.com/crmarques/bootwright/internal/infrastructureservices/managedservice"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets"
)

const (
	releaseAuthMaterial = "bootwright:release-auth"
	releaseOrigin       = "quay.io"
)

type ReleaseAuth func(pullSecret []byte, origin string, username, password []byte, mirror string) ([]byte, error)

func (c ReleaseFill) lendReleaseAuth(execution lifecycle.Execution, request ReleaseRequest) (lifecycle.Execution, error) {
	if c.auth == nil {
		return execution, refusal("lifecycle.state", "the release copy derives no auth document", "")
	}
	if err := managedservice.ProveCredential(execution.Material, request.Mirror.CredentialsRef, execution.Context, false); err != nil {
		return execution, err
	}
	bound, found := execution.Material[request.PullSecretRef]
	if !found {
		return execution, secrets.Refusal("store", "the pull secret "+request.PullSecretRef+" this operation bound is not available to this attempt", execution.Context, request.PullSecretRef,
			lifecycle.ReopenBindings(execution.Context, execution.Continuation))
	}
	pullSecret, _ := bound.Part(secrets.ValuePart)
	defer clear(pullSecret)
	credential := execution.Material[request.Mirror.CredentialsRef]
	username, _ := credential.Part(secrets.UsernamePart)
	defer clear(username)
	password, _ := credential.Part(secrets.PasswordPart)
	defer clear(password)
	document, err := c.auth(pullSecret, releaseOrigin, username, password, request.Mirror.Host)
	if err != nil {
		return execution, secrets.Refusal("part", "the pull secret holds no quay.io entry with an auth token, which the release copy reads the release with", execution.Context, request.PullSecretRef,
			"store a dockerConfigJson pull secret whose auths name quay.io with bootwright secret set --context "+execution.Context+" --name "+request.PullSecretRef+" --value-file <path>, then apply again")
	}
	defer clear(document)
	material := maps.Clone(execution.Material)
	material[releaseAuthMaterial] = secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: document})
	execution.Material = material
	return execution, nil
}
