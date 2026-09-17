package access

import (
	"context"
	"io"

	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/trust"
)

// EffectiveState compiles the selected context's immutable input into the
// graph a session resolves its target in.
type EffectiveState interface {
	RenderEffective(context.Context, compilation.EffectiveRequest) (*compilation.EffectiveResult, error)
}

// MaterialLender opens exactly the Secrets one session needs for its duration.
// A session registers no operation, so it borrows material rather than
// freezing it: the binding exists only while the client runs.
type MaterialLender interface {
	WithMaterial(context.Context, lifecycle.MaterialRequest, func(context.Context, map[string]secrets.Material) error) error
}

// Ownership reports what durable evidence proves about each object. A Machine
// this context installed is proved against its installation's own host key, so
// a session asks whether that installation is still this context's own.
type Ownership interface {
	Ownership(context.Context, string) (map[string]machine.OwnershipState, error)
}

// Evidence reports the host key one context's own installation proved for a
// Machine. The second result distinguishes an installation that proved no key
// from one this context never performed.
type Evidence interface {
	HostKey(ctx context.Context, contextName, name string) (machine.HostKeyEvidence, bool, error)
}

// HostKeyStore holds the host keys a context trusts. Publication is compared
// against the exact records the caller read, so two operators confirming a
// first-use key at once cannot lose one another's decision.
type HostKeyStore interface {
	ReadHostKeys(ctx context.Context, contextName string) ([]byte, error)
	ReplaceHostKeys(ctx context.Context, contextName string, data, expected []byte) error
}

// Observer reads the key a server presents. It offers no credential and
// records nothing, so what it returns is an observation and not yet trust.
type Observer interface {
	Observe(ctx context.Context, address string, port int) (trust.HostKey, error)
}

// Confirmer asks the operator to accept one unproved host key. It is the only
// thing that turns an observation into a record.
type Confirmer interface {
	ConfirmHostKey(ctx context.Context, name, address, keyType, fingerprint string) error
}

// Launcher runs the one pinned SSH client. Run returns the client's own exit
// status, so nothing between here and the operator interprets it.
type Launcher interface {
	Run(ctx context.Context, session machine.Session, in io.Reader, out, errOut io.Writer) (int, error)
	IdentityFile(path string) (string, error)
}

// Streams are the operator's own terminal streams. A session hands them to the
// client unchanged for its whole duration.
type Streams struct {
	In       io.Reader
	Out, Err io.Writer
}

func failure(code, message, remediation string) error {
	return diagnostics.NewFailureWithRemediation(code, message, "", remediation)
}
