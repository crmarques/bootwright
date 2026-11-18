package lifecycle

import (
	"context"
	"crypto/subtle"

	"github.com/crmarques/bootwright/internal/canonicaljson"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/secrets/custody"
	"github.com/crmarques/bootwright/internal/secrets/secretstore"
)

func (s AdminCredentialService) AdministratorPassword(ctx context.Context, contextName, cluster, block, seed string) (secretstore.ProducedMaterial, bool, error) {
	read := func(output string) (secretstore.ProducedMaterial, bool, error) {
		return s.binder.ReadProduced(ctx, custody.ReadProducedRequest{ContextName: contextName, Block: block, Name: output})
	}
	active, found, err := read("admin-password")
	if err == nil && !found {
		active, found, err = read(seed)
	}
	if err != nil {
		active.Material.Clear()
		return secretstore.ProducedMaterial{}, false, err
	}
	pending, present, err := read("admin-password-update")
	defer pending.Material.Clear()
	if err != nil {
		active.Material.Clear()
		return secretstore.ProducedMaterial{}, false, err
	}
	if present {
		encoded, _ := pending.Material.Part(secrets.ValuePart)
		defer clear(encoded)
		var update adminUpdate
		valid := len(encoded) <= 16384 && canonicaljson.DecodeClosed(encoded, &update) == nil && s.validAdminUpdate(update) && !pending.Unproved
		password, _ := active.Material.Part(secrets.ValuePart)
		defer clear(password)
		current := valid && subtle.ConstantTimeCompare(password, []byte(update.Password)) == 1
		if !current {
			active.Material.Clear()
			return secretstore.ProducedMaterial{}, false, engineCredentialPending.Fail("the administrator credential update has not completed in custody",
				"bootwright cluster update-admin-credentials --context "+contextName+" --name "+cluster+" finishes the retained update")
		}
	}
	return active, found, nil
}

func AdminCredentialProved(evidence []byte, digest string) bool {
	var proof struct {
		Proved  bool   `json:"proved"`
		Request string `json:"request"`
	}
	return len(evidence) <= 512 && digest != "" && canonicaljson.DecodeClosed(evidence, &proof) == nil && proof.Proved && proof.Request == digest
}
