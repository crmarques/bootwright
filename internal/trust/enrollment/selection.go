package enrollment

import (
	"slices"
	"strconv"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/machine"
)

// candidate is one Machine enrollment may record a key for, or the exact
// reason it may not.
type candidate struct {
	name    string
	address string
	port    int
	skip    string
}

func (c candidate) eligible() bool { return c.skip == "" }

// candidates orders the selected Machines and says which of them use
// context-managed trust. A Machine that proves its key another way is
// reported rather than omitted, so an operator can see why it was left alone.
func candidates(catalog api.Catalog, selected, replace []string, contextName string) ([]candidate, error) {
	objects := catalog.OfKind(api.Machine)
	known := map[string]bool{}
	out := make([]candidate, 0, len(objects))
	for _, object := range objects {
		known[object.Name()] = true
		if len(selected) != 0 && !slices.Contains(selected, object.Name()) {
			continue
		}
		out = append(out, classify(object))
	}
	slices.SortFunc(out, func(a, b candidate) int { return strings.Compare(a.name, b.name) })
	for _, name := range selected {
		if !known[name] {
			return nil, failure("access.target", "the selected context declares no Machine named "+name,
				listRemedy(contextName))
		}
	}
	return out, replaceable(out, selected, replace, contextName)
}

func listRemedy(contextName string) string {
	return "list the Machines this context selects with bootwright machine list --context " + contextName
}

// classify reports whether one Machine's host key is this store's to hold.
// Every other source of proof outranks the store, so a Machine that has one is
// never observed and never recorded.
func classify(object api.Object) candidate {
	entry := candidate{name: object.Name(), skip: machine.TrustExemption(object)}
	if ssh, ok := machine.SSH(object); ok && entry.skip == "" {
		entry.address, entry.port = ssh.Address, ssh.Port
	}
	return entry
}

// replaceable refuses a re-trust that could not apply. Superseding a key is
// the one deliberate way a changed host is accepted, so naming a Machine it
// cannot reach is a mistake worth reporting rather than ignoring.
func replaceable(selection []candidate, selected, replace []string, contextName string) error {
	for _, name := range replace {
		if len(selected) != 0 && !slices.Contains(selected, name) {
			return failure("cli.usage", "--replace names "+name+", which --machines does not select",
				"add it to --machines, or drop it from --replace")
		}
		index := slices.IndexFunc(selection, func(c candidate) bool { return c.name == name })
		if index < 0 {
			return failure("access.target", "the selected context declares no Machine named "+name, listRemedy(contextName))
		}
		if !selection[index].eligible() {
			return failure("access.unavailable", name+" does not use context-managed trust: "+selection[index].skip, "")
		}
	}
	return nil
}

func token(address string, port int) string {
	if port == 0 || port == 22 {
		return address
	}
	return "[" + address + "]:" + strconv.Itoa(port)
}
