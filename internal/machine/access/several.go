package access

import (
	"context"
	"slices"
	"strconv"
	"strings"
	"sync"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/availability"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets"
)

const launchFailedStatus = 255

type member struct {
	target        target
	identity      machine.Identity
	privateKeyRef string
}

type outcome struct {
	code int
	err  error
}

func memberNames(requested []string) []string {
	var names []string
	for _, name := range requested {
		name = strings.TrimSpace(name)
		if name != "" && !slices.Contains(names, name) {
			names = append(names, name)
		}
	}
	return names
}

func (s Service) several(ctx context.Context, contextName string, names []string, options machine.SSHOptions,
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
	members, err := s.members(ctx, effective.Effective, selected, names, options)
	if err != nil {
		return nil, err
	}
	requested, err := command(words)
	if err != nil {
		return nil, err
	}
	var outcomes []outcome
	request := lifecycle.MaterialRequest{ContextName: selected, Secrets: memberReferences(members)}
	err = s.options.Lender.WithMaterial(ctx, request, func(inner context.Context, material map[string]secrets.Material) error {
		sessions, err := s.prove(inner, selected, members, requested, material, declaredMachines(effective.Effective))
		defer func() {
			for _, session := range sessions {
				clear(session.PrivateKey)
			}
		}()
		if err != nil {
			return err
		}
		if err := inner.Err(); err != nil {
			return err
		}
		outcomes = s.launch(inner, sessions)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.settle(selected, members, outcomes), nil
}

func (s Service) members(ctx context.Context, catalog api.Catalog, contextName string, names []string,
	options machine.SSHOptions) ([]member, error) {
	names = slices.Sorted(slices.Values(names))
	var unknown []string
	for _, name := range names {
		if _, ok := catalog.Find(api.Machine, name); !ok {
			unknown = append(unknown, name)
		}
	}
	if len(unknown) > 0 {
		subject := "no Machine named "
		if len(unknown) > 1 {
			subject = "no Machines named "
		}
		return nil, failure("access.target", "the selected context declares "+subject+strings.Join(unknown, ", "),
			"list the Machines this context selects with bootwright machine list --context "+contextName)
	}
	members := make([]member, 0, len(names))
	offered, proved := "", false
	for _, name := range names {
		resolved, err := resolveTarget(catalog, name, contextName)
		if err != nil {
			return nil, err
		}
		if !proved {
			if offered, err = s.options.Launcher.IdentityFile(ctx, options.IdentityFile); err != nil {
				return nil, err
			}
			proved = true
		}
		identity, privateKeyRef, err := identityFor(resolved, options, offered)
		if err != nil {
			return nil, err
		}
		if identity.Kind == machine.IdentityPassword {
			return nil, failure("access.unavailable",
				resolved.object.Identity()+" authenticates with a password the client asks for on the terminal, "+
					"which concurrent sessions cannot share",
				"run bootwright machine exec --context "+contextName+" --name "+resolved.name+" -- <command> on it alone")
		}
		members = append(members, member{target: resolved, identity: identity, privateKeyRef: privateKeyRef})
	}
	return members, nil
}

func (s Service) prove(ctx context.Context, contextName string, members []member, words []string,
	material map[string]secrets.Material, machines declaredTrust) ([]machine.Session, error) {
	sessions := make([]machine.Session, 0, len(members))
	for _, selected := range members {
		session := machine.Session{
			Machine: selected.target.name, Address: selected.target.address, Port: selected.target.port,
			Identity: selected.identity, Command: words, Batch: true,
		}
		if selected.privateKeyRef != "" {
			key, err := privateKey(selected.privateKeyRef, material)
			if err != nil {
				return sessions, err
			}
			session.PrivateKey = key
		}
		host, err := s.hostKey(ctx, contextName, selected.target, material, machines)
		if err != nil {
			clear(session.PrivateKey)
			return sessions, err
		}
		session.HostKey = host
		sessions = append(sessions, session)
	}
	return sessions, nil
}

func (s Service) launch(ctx context.Context, sessions []machine.Session) []outcome {
	out, errOut := newDestination(s.options.Streams.Out), newDestination(s.options.Streams.Err)
	outcomes := make([]outcome, len(sessions))
	var group sync.WaitGroup
	for i, session := range sessions {
		group.Go(func() {
			stdout, stderr := out.session(session.Machine), errOut.session(session.Machine)
			code, err := s.options.Launcher.Run(ctx, session, nil, stdout, stderr)
			stdout.Close()
			stderr.Close()
			outcomes[i] = outcome{code: code, err: err}
		})
	}
	group.Wait()
	return outcomes
}

func (s Service) settle(contextName string, members []member, outcomes []outcome) *machine.SessionResult {
	result := &machine.SessionResult{Context: contextName}
	for i, ended := range outcomes {
		name := members[i].target.name
		code := ended.code
		switch {
		case ended.err != nil:
			code = launchFailedStatus
			s.exited(name + " could not start (status " + strconv.Itoa(code) + "): " + reason(ended.err))
		case code != 0:
			s.exited(name + " exited with status " + strconv.Itoa(code))
		}
		result.ExitCode = max(result.ExitCode, code)
		result.Sessions = append(result.Sessions, machine.SessionResult{
			Context: contextName, Machine: name, Address: members[i].target.address, ExitCode: code,
		})
	}
	return result
}

func (s Service) exited(message string) {
	notice := diagnostics.Diagnostic{Severity: "warning", Code: "access.exit", Message: message}
	s.warn(notice.Code + ": " + notice.Message)
}

func memberReferences(members []member) []string {
	var names []string
	for _, selected := range members {
		for _, name := range references(selected.target, selected.privateKeyRef) {
			if !slices.Contains(names, name) {
				names = append(names, name)
			}
		}
	}
	return names
}

func reason(err error) string {
	message := err.Error()
	if reported := diagnostics.Of(err); len(reported) > 0 {
		message = reported[0].Message
	}
	return strings.Map(func(r rune) rune {
		if unsafeRune(r) {
			return '?'
		}
		return r
	}, message)
}
