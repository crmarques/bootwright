package access

import (
	"context"
	"time"

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
	Terminal func() (bool, error)
	Clock    func() time.Time
}

type Service struct {
	state     EffectiveState
	selection machine.CurrentSelection
	options   Options
}

func New(state EffectiveState, selection machine.CurrentSelection, options Options) Service {
	return Service{state: state, selection: selection, options: options}
}

// Rsh opens one interactive session. It accepts no command tail: an
// interactive shell and a single command are different requests, and running
// one as the other would silently change what the operator asked for.
func (s Service) Rsh(ctx context.Context, request RshRequest) (*machine.SessionResult, error) {
	return s.open(ctx, request.ContextName, request.Name, request.SSH, nil)
}

// Exec runs one exact argument vector on the Machine and reports the status
// that command exited with.
func (s Service) Exec(ctx context.Context, request ExecRequest) (*machine.SessionResult, error) {
	if len(request.Command) == 0 {
		return nil, failure("cli.usage", "a non-empty command argument vector is required",
			"supply the command to run, after -- if it begins with a flag")
	}
	return s.open(ctx, request.ContextName, request.Name, request.SSH, request.Command)
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
	identity, privateKeyRef, err := resolveIdentity(ctx, resolved, options, s.options.Launcher)
	if err != nil {
		return nil, err
	}
	requested, err := command(words)
	if err != nil {
		return nil, err
	}
	session := machine.Session{
		Machine: resolved.name, Address: resolved.address, Port: resolved.port,
		Identity: identity, Command: requested,
	}
	result := &machine.SessionResult{Context: selected, Machine: resolved.name, Address: resolved.address}
	request := lifecycle.MaterialRequest{ContextName: selected, Secrets: references(resolved, privateKeyRef)}
	err = s.options.Lender.WithMaterial(ctx, request, func(inner context.Context, material map[string]secrets.Material) error {
		if privateKeyRef != "" {
			key, err := privateKey(privateKeyRef, material)
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
		s.advise(resolved, selected)
		code, err := s.options.Launcher.Run(inner, session, s.options.Streams.In, s.options.Streams.Out, s.options.Streams.Err)
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
