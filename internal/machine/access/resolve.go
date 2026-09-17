package access

import (
	"strings"
	"unicode"
	"unicode/utf8"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/machine"
)

// target is one Machine's resolved endpoint together with the declarations a
// session needs opened to reach it.
type target struct {
	object    api.Object
	name      string
	address   string
	port      int
	installed bool
	ssh       machine.SSHTarget
}

// resolveTarget fixes the exact Machine a session acts on. A name outside the
// selected graph, or one that declares no reachable SSH access, fails before
// anything is opened or contacted.
func resolveTarget(catalog api.Catalog, name string) (target, error) {
	if name == "" {
		return target{}, failure("access.target", "no Machine name was supplied", "name one with --name <machine>")
	}
	object, ok := catalog.Find(api.Machine, name)
	if !ok {
		return target{}, failure("access.target", "the selected context declares no Machine named "+name,
			"list the Machines this context selects with bootwright machine list")
	}
	if object.Spec().Get("access", "local").Bool() {
		return target{}, failure("access.unavailable", "this Machine is reached locally and is not an SSH target",
			"run the command directly on "+object.Identity())
	}
	ssh, ok := machine.SSH(object)
	if !ok {
		return target{}, failure("access.unavailable", "this Machine declares no resolvable SSH access",
			"author access.ssh with an address this context declares on "+object.Identity())
	}
	return target{
		object: object, name: object.Name(), address: ssh.Address, port: ssh.Port,
		installed: machine.Installed(object), ssh: ssh,
	}, nil
}

// resolveIdentity fixes the account one session logs in as together with the
// credential that opens it. The two are one value: a borrowed account naming
// an identity this context holds no credential for is offered none, rather
// than being handed the key that belongs to another account.
func resolveIdentity(selected target, options machine.SSHOptions, launcher Launcher) (machine.Identity, string, error) {
	offered, err := launcher.IdentityFile(options.IdentityFile)
	if err != nil {
		return machine.Identity{}, "", err
	}
	identity := machine.Identity{User: selected.ssh.User, IdentityFile: offered}
	borrowed := options.User != "" && options.User != selected.ssh.User
	if borrowed {
		// A credential this context holds opens exactly the account it was
		// authored for, so another account receives no stored material.
		identity.User = options.User
		identity.Kind = machine.IdentityOperator
		if offered == "" {
			return identity, "", failure("access.unavailable",
				"this context holds no credential for the account "+options.User+" on "+selected.object.Identity(),
				"offer one with --ssh-id-file, or drop --ssh-user to use the Machine's own identity")
		}
		return identity, "", nil
	}
	switch {
	case selected.ssh.PrivateKeyRef != "":
		identity.Kind = machine.IdentityKey
		return identity, selected.ssh.PrivateKeyRef, nil
	case selected.ssh.PasswordRef != "":
		identity.Kind = machine.IdentityPassword
		return identity, "", nil
	default:
		identity.Kind = machine.IdentityOperator
		return identity, "", nil
	}
}

// command refuses a value that could not reach the remote host as the value
// the operator wrote. A control character would be reinterpreted by the
// remote shell or the terminal, so the session refuses rather than running
// something other than what was asked.
func command(words []string) ([]string, error) {
	out := make([]string, 0, len(words))
	for _, word := range words {
		if !utf8.ValidString(word) || strings.ContainsFunc(word, unsafeRune) {
			return nil, failure("access.handoff", "a requested command value cannot be safely encoded",
				"remove control characters from the requested command")
		}
		out = append(out, word)
	}
	return out, nil
}

func unsafeRune(r rune) bool {
	return r == utf8.RuneError || unicode.IsControl(r) || !unicode.IsPrint(r) && !unicode.IsSpace(r)
}
