package access

import (
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/machine"
)

// SSHClient is the client identity every Machine handoff names. The product
// pins one absolute executable and never searches an ambient path, so the
// descriptor an operator reads always identifies the program it means.
const SSHClient = "/usr/bin/ssh"

// Descriptor is one bounded handoff: the pinned client, the exact target and
// the argument vector as data. Advisory names the safe next action when the
// operator must obtain access material themselves; it carries no material.
type Descriptor struct {
	Context   string
	Machine   string
	Client    string
	Arguments []string
	Advisory  string
}

// describe resolves one Machine's SSH handoff. A borrowed account replaces the
// authored identity without changing desired state; on a Bootwright-installed
// Machine it is eligible only when the invocation extends it explicitly.
func describe(catalog api.Catalog, contextName, name string, options machine.SSHOptions) (*Descriptor, error) {
	if name == "" {
		return nil, failure("access.target", "no Machine name was supplied", "name one with --name <machine>")
	}
	object, ok := catalog.Find(api.Machine, name)
	if !ok {
		return nil, failure("access.target", "the selected context declares no Machine named "+name,
			"list the Machines this context selects with bootwright machine list")
	}
	if object.Spec().Get("access", "local").Bool() {
		return nil, failure("access.unavailable", "this Machine is reached locally and is not an SSH target",
			"run the command directly on "+object.Identity())
	}
	target, ok := machine.SSH(object)
	if !ok {
		return nil, failure("access.unavailable", "this Machine declares no resolvable SSH access",
			"author access.ssh with an address this context declares on "+object.Identity())
	}
	if options.User != "" && machine.Installed(object) && !options.UserForProvisioned {
		return nil, failure("access.unavailable", "a borrowed account is not eligible on a Bootwright-provisioned Machine",
			"repeat the command with --ssh-user-for-provisioned, or without --ssh-user")
	}
	user := target.User
	if options.User != "" {
		user = options.User
	}
	arguments := []string{}
	if options.IdentityFile != "" {
		arguments = append(arguments, "-i", options.IdentityFile, "-o", "IdentitiesOnly=yes")
	}
	if target.Port != 22 {
		arguments = append(arguments, "-p", strconv.Itoa(target.Port))
	}
	if user != "" {
		arguments = append(arguments, "-l", user)
	}
	arguments = append(arguments, target.Address)
	return &Descriptor{
		Context: contextName, Machine: object.Name(), Client: SSHClient,
		Arguments: arguments, Advisory: advisory(target, options),
	}, nil
}

// advisory names the export an operator performs themselves when the Machine's
// identity is confidential material this context holds. A private key is never
// written to a descriptor, an argument or a path on the operator's behalf.
func advisory(target machine.SSHTarget, options machine.SSHOptions) string {
	if options.IdentityFile != "" {
		return ""
	}
	if target.PrivateKeyRef != "" {
		return "this Machine authenticates with the " + target.PrivateKeyRef +
			" Secret; export it with bootwright secret show and offer it with --ssh-id-file"
	}
	if target.PasswordRef != "" {
		return "this Machine authenticates with the " + target.PasswordRef +
			" Secret; export it with bootwright secret show"
	}
	return ""
}

// withCommand appends the requested remote command. Values are preserved as
// data in the argument vector; the product never composes them into shell text.
func (d *Descriptor) withCommand(command []string) (*Descriptor, error) {
	if len(command) == 0 {
		return nil, failure("cli.usage", "a non-empty command argument vector is required", "supply the command to run, after -- if it begins with a flag")
	}
	d.Arguments = append(d.Arguments, slices.Clone(command)...)
	return d, nil
}

// encodable refuses a descriptor whose values cannot be presented exactly. An
// argument that needs escaping to be displayed would not be the argument the
// operator runs, so the handoff fails rather than emitting a changed vector.
func (d *Descriptor) encodable() error {
	for _, value := range append([]string{d.Client}, d.Arguments...) {
		if !utf8.ValidString(value) || strings.ContainsFunc(value, unsafeRune) {
			return failure("access.handoff", "an access descriptor value cannot be safely encoded",
				"remove control characters from the requested target and command")
		}
	}
	return nil
}

func unsafeRune(r rune) bool {
	return r == utf8.RuneError || unicode.IsControl(r) || !unicode.IsPrint(r) && !unicode.IsSpace(r)
}
