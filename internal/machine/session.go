package machine

import "github.com/crmarques/bootwright/internal/trust"

// IdentityKind names how a session authenticates. The credential and the
// account it opens are resolved together, so a kind never travels without the
// material or the account that belongs to it.
type IdentityKind string

const (
	// IdentityKey authenticates with a private key this context holds.
	IdentityKey IdentityKind = "key"
	// IdentityOperator authenticates as whoever runs the client, with that
	// account's own default identities and no material from this context.
	IdentityOperator IdentityKind = "operator"
	// IdentityPassword leaves the client to ask for a password this context
	// holds but does not answer with.
	IdentityPassword IdentityKind = "password"
)

// Identity is the account one session logs in as together with the credential
// that opens it. PrivateKey is bounded memory owned by the caller; the client
// receives it as an open descriptor and never as a path this product wrote.
type Identity struct {
	Kind         IdentityKind
	User         string
	PrivateKey   []byte
	IdentityFile string
}

// Session is one resolved SSH session: the exact endpoint, the identity that
// opens it, the host key it pins, and the argument vector to run there. An
// empty Command is an interactive session.
type Session struct {
	Machine string
	Address string
	Port    int
	Identity
	HostKey trust.HostKey
	Command []string
}

// SessionResult is what an access command reports once the client has exited.
// The exit status is the client's, so nothing here describes success: the
// remote process already said what happened on its own streams.
type SessionResult struct {
	Context  string
	Machine  string
	Address  string
	ExitCode int
}
