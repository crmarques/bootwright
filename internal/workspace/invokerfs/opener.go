// Package invokerfs opens operator-named paths with the invoking account's
// credentials and hands the caller descriptors, never pathnames. The adapter
// that receives a descriptor keeps every type, ownership and stability proof.
package invokerfs

import "context"

// HelperMode is the one argument the credential-dropped helper accepts.
const HelperMode = "__bootwright_open"

// Account must come from the verified local account, never HOME, XDG or SUDO_*
// variables.
type Account struct {
	UID, GID int
	Groups   []uint32
}

// Opener resolves the invoking account only when a root process begins a
// session, so constructing one acquires no account capability.
type Opener struct {
	account func(context.Context) (Account, error)
}

func New(account func(context.Context) (Account, error)) *Opener {
	return &Opener{account: account}
}
