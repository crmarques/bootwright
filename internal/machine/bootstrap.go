package machine

import (
	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/secrets"
)

const EnrollmentFamily = "machine-enrollment"

type Bootstrap struct {
	Auth       string `json:"auth"`
	AuthRef    string `json:"authRef,omitempty"`
	Sudo       bool   `json:"sudo,omitempty"`
	SudoPrompt bool   `json:"sudoPrompt,omitempty"`
	SudoRef    string `json:"sudoRef,omitempty"`
	User       string `json:"user,omitempty"`
}

func BootstrapOf(o api.Object) (Bootstrap, bool) {
	block := o.Spec().Get("access", "lifecycle", "bootstrap")
	if !block.Present() {
		return Bootstrap{}, false
	}
	b := Bootstrap{User: block.Get("user").Text(), Sudo: block.Has("privilege", "sudo"),
		SudoPrompt: block.Has("privilege", "sudo", "askPassword"), SudoRef: block.Get("privilege", "sudo", "passwordRef").Text()}
	for _, arm := range []string{"operatorIdentity", "privateKeyRef", "askPassword", "passwordRef"} {
		if block.Has("auth", arm) {
			b.Auth, b.AuthRef = arm, block.Get("auth", arm).Text()
			break
		}
	}
	return b, true
}

func (b Bootstrap) Valid(successor string) bool {
	auth, user := api.ValidLexical(api.LexicalName, b.AuthRef), api.ValidLexical(api.LexicalPOSIXUser, b.User)
	if b.User == successor || (b.User != "" && !user) || !b.Sudo && b.User != "root" ||
		b.Sudo && b.User == "root" || b.SudoRef != "" && (!b.Sudo || b.SudoPrompt || !api.ValidLexical(api.LexicalName, b.SudoRef)) ||
		b.SudoPrompt && !b.Sudo {
		return false
	}
	switch b.Auth {
	case "privateKeyRef", "passwordRef":
		return auth && user
	case "operatorIdentity", "askPassword":
		return b.AuthRef == ""
	}
	return false
}

func (b Bootstrap) Classes() secrets.Classes {
	return secrets.Classes{}.Add(secrets.AccessCredential, b.AuthRef, b.SudoRef)
}
