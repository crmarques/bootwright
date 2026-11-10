package managedservice

import (
	"bytes"
	"maps"

	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"

	"github.com/crmarques/bootwright/internal/secrets"
)

const maxBcryptPassword = 72

// ProveCredential proves a password file line can carry the client credential
// and bcrypt read all of its password; no refusal carries material.
func ProveCredential(material map[string]secrets.Material, secret, contextName string, registered bool) error {
	exit := "then apply again"
	if registered {
		exit = "then destroy this apply and apply again"
	}
	remedy := "store a printable ASCII username without a colon and a password of at most 72 bytes with bootwright secret set --name " + secret + " --username <name> --password-stdin --context " + contextName + "; " + exit
	bound, found := material[secret]
	if !found {
		return secrets.Refusal("store", "the client credential Secret "+secret+" this operation bound is not available to this attempt", contextName, secret,
			"run bootwright status --context "+contextName+" and repeat the command it names, which reopens this operation's Secret bindings")
	}
	username, named := bound.Part(secrets.UsernamePart)
	password, held := bound.Part(secrets.PasswordPart)
	if !named || !held {
		return secrets.Refusal("part", "the client credential Secret has no username or password part", contextName, secret, remedy)
	}
	if len(username) == 0 || bytes.ContainsFunc(username, func(r rune) bool { return r == ':' || r < 0x20 || r > 0x7e }) {
		return secrets.Refusal("part", "the client credential's username is empty or holds a colon, a control or a non-ASCII character, which a password file line cannot carry", contextName, secret, remedy)
	}
	if len(password) == 0 || len(password) > maxBcryptPassword || bytes.IndexByte(password, 0) >= 0 {
		return secrets.Refusal("part", "the client credential's password is empty, longer than the 72 bytes bcrypt reads, or holds a NUL", contextName, secret, remedy)
	}
	return nil
}

const passwordFileMaterial = "bootwright:password-file"

func (c Capability) passwordFile(execution lifecycle.Execution, request Request) (lifecycle.Execution, error) {
	if c.definition.PasswordFile == nil {
		return execution, Refusal("lifecycle.state", "the managed service capability derives no password file", "")
	}
	bound := execution.Material[request.CredentialsRef]
	username, _ := bound.Part(secrets.UsernamePart)
	password, _ := bound.Part(secrets.PasswordPart)
	defer clear(password)
	file, err := c.definition.PasswordFile(username, password)
	if err != nil {
		return execution, Refusal("lifecycle.state", "the password file of "+request.Kind+"/"+request.Identity.Service+" could not be derived", "")
	}
	material := maps.Clone(execution.Material)
	material[passwordFileMaterial] = secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: file})
	execution.Material = material
	return execution, nil
}
