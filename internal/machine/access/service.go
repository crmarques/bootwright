package access

import (
	"context"
	"time"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/availability"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets"
)

// Options names everything a session needs beyond the graph it resolves in.
// A nil capability is not a silent fallback: the resolution that needs it
// refuses rather than reaching the Machine with less proof than it requires.
type Options struct {
	Copier    Copier
	Lender    MaterialLender
	Ownership Ownership
	Evidence  Evidence
	Trust     HostKeyStore
	Observer  Observer
	Confirmer Confirmer
	Launcher  Launcher
	Streams   Streams
	// Terminal reports whether the operator can answer a host-key
	// confirmation. Without one there is no first use.
	Terminal      func() (bool, error)
	Clock         func() time.Time
	FleetIdentity func(api.Catalog, api.Object) (FleetIdentity, bool)
	Prepared      func(context.Context)
	Authorize     func(context.Context, string, api.Catalog, []machine.SessionTarget, []string) error
}

type FleetIdentity struct{ User, KeyRef string }

type Service struct {
	state     EffectiveState
	selection CurrentSelection
	options   Options
}

func (s Service) WithCatalog(catalog api.Catalog) Service {
	s.state = sessionCatalog{catalog}
	return s
}

type sessionCatalog struct{ catalog api.Catalog }

func (s sessionCatalog) RenderEffective(ctx context.Context, _ compilation.EffectiveRequest) (*compilation.EffectiveResult, error) {
	return &compilation.EffectiveResult{Effective: s.catalog}, ctx.Err()
}

func New(state EffectiveState, selection CurrentSelection, options Options) Service {
	return Service{state: state, selection: selection, options: options}
}

// Rsh opens one interactive session. It accepts no command tail: an
// interactive shell and a single command are different requests, and running
// one as the other would silently change what the operator asked for.
func (s Service) Rsh(ctx context.Context, request RshRequest) (*machine.SessionResult, error) {
	return s.open(ctx, request.ContextName, request.Name, request.SSH, nil)
}

// Exec runs one exact argument vector on each named Machine and reports the
// status that command exited with.
func (s Service) Exec(ctx context.Context, request ExecRequest) (*machine.SessionResult, error) {
	if len(request.Command) == 0 {
		return nil, failure("cli.usage", "a non-empty command argument vector is required",
			"supply the command to run, after -- if it begins with a flag")
	}
	names := memberNames(request.Names)
	switch len(names) {
	case 0:
		return nil, failure("cli.usage", "no Machine name was supplied",
			"name one or more with --name <machine>[,<machine>...]")
	case 1:
		return s.open(ctx, request.ContextName, names[0], request.SSH, request.Command)
	}
	return s.several(ctx, request.ContextName, names, request.SSH, request.Command)
}

func (s Service) open(ctx context.Context, contextName, name string, options machine.SSHOptions,
	words []string) (*machine.SessionResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.state == nil || s.options.Launcher == nil || s.options.Lender == nil {
		return nil, availability.ErrNotImplemented
	}
	selected, err := machine.SelectedContext(ctx, s.selection, contextName)
	if err != nil {
		return nil, err
	}
	effective, err := s.state.RenderEffective(ctx, compilation.EffectiveRequest{ContextName: selected})
	if err != nil {
		return nil, err
	}
	resolved, err := resolveTarget(effective.Effective, name, selected)
	if err != nil {
		return nil, err
	}
	if err := s.admitOptions(options); err != nil {
		return nil, err
	}
	if options.AskPassword {
		terminal, err := s.interactive()
		if err != nil {
			return nil, err
		}
		if !terminal {
			return nil, failure("access.unavailable", "SSH password prompting requires a terminal", "use --ssh-id-file for unattended access")
		}
	}
	identity, privateKeyRef, err := s.identity(ctx, effective.Effective, resolved, options)
	if err != nil {
		return nil, err
	}
	requested, err := command(words)
	if err != nil {
		return nil, err
	}
	if err := s.authorize(ctx, selected, effective.Effective, []member{{target: resolved, identity: identity, privateKeyRef: privateKeyRef}}, requested); err != nil {
		return nil, err
	}
	delegated := s.options.Authorize != nil
	input := s.options.Streams.In
	if delegated && len(requested) > 0 {
		input = nil
	}
	session := machine.Session{
		Delegated: delegated, Batch: delegated && len(requested) > 0,
		Machine: resolved.name, Address: resolved.address, Port: resolved.port,
		Identity: identity, Command: requested,
	}
	result := &machine.SessionResult{Context: selected, Machine: resolved.name, Address: resolved.address}
	request := lifecycle.MaterialRequest{ContextName: selected, Secrets: references(resolved, privateKeyRef)}
	err = s.options.Lender.WithMaterial(ctx, request, func(inner context.Context, material map[string]secrets.Material) error {
		if privateKeyRef != "" {
			key, err := privateKey(privateKeyRef, material, privateKeyPart(effective.Effective, privateKeyRef))
			if err != nil {
				return err
			}
			defer clear(key)
			session.PrivateKey = key
		}
		host, err := s.hostKey(inner, selected, resolved, material, declaredMachines(effective.Effective))
		if err != nil {
			return err
		}
		session.HostKey = host
		if identity.Kind == machine.IdentityPassword && !options.AskPassword {
			s.advise(resolved, selected)
		}
		s.prepared(inner)
		code, err := s.options.Launcher.Run(inner, session, input, s.options.Streams.Out, s.options.Streams.Err)
		if err != nil {
			return err
		}
		result.ExitCode = code
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// advise names the reveal an operator performs themselves before the client
// asks for it. It is written to the operator's own error stream, before the
// connection, so it can never be mistaken for the session's output.
func (s Service) advise(selected target, contextName string) {
	if notice := passwordAdvisory(selected, contextName); notice != "" {
		advisory := diagnostics.Diagnostic{Severity: "warning", Code: "access.credential", Message: notice}
		s.warn(advisory.Code + ": " + advisory.Message)
	}
}

func (s Service) warn(notice string) {
	if s.options.Streams.Err == nil {
		return
	}
	_, _ = s.options.Streams.Err.Write([]byte("[WARN] " + notice + "\n"))
}

func (s Service) interactive() (bool, error) {
	if s.options.Terminal == nil {
		return false, nil
	}
	return s.options.Terminal()
}

func (s Service) now() string {
	clock := s.options.Clock
	if clock == nil {
		clock = time.Now
	}
	return clock().UTC().Format(time.RFC3339)
}

func (s Service) admitOptions(options machine.SSHOptions) error {
	if s.options.Authorize != nil && options.Borrowed() {
		return failure("access.denied", "delegated SSH sessions require the configured remote identity", "remove the SSH identity override")
	}
	return nil
}

func (s Service) authorize(ctx context.Context, contextName string, catalog api.Catalog, members []member, argv []string) error {
	if s.options.Authorize == nil {
		return nil
	}
	targets := make([]machine.SessionTarget, 0, len(members))
	for _, entry := range members {
		if entry.identity.Kind != machine.IdentityKey || entry.privateKeyRef == "" {
			return failure("access.denied", "delegated SSH access to Machine "+entry.target.name+" requires a key", "ask the context administrator to configure SSH key authentication")
		}
		targets = append(targets, machine.SessionTarget{Machine: entry.target.name, RemoteUser: entry.identity.User})
	}
	return s.options.Authorize(ctx, contextName, catalog, targets, argv)
}

func (s Service) prepared(ctx context.Context) {
	if s.options.Prepared != nil {
		s.options.Prepared(ctx)
	}
}
