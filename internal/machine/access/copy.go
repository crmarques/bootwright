package access

import (
	"context"
	"errors"
	"sync"

	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets"
)

type CopyRequest struct {
	ContextName    string
	Names          []string
	SSH            machine.SSHOptions
	Source         machine.CopyPath
	Destination    machine.CopyPath
	Overwrite      bool
	NonInteractive bool
}

type CopyAccount struct {
	UID, GID int
	Groups   []uint32
}

type CopyProgress struct {
	Files, Bytes int64
	Unknown      bool
}

type CopyRow struct {
	Machine, Destination, Status string
	Files, Bytes                 int64
}

type CopyResult struct {
	Context  string
	Source   machine.CopyPath
	Machines []CopyRow
}

func (s Service) Copy(ctx context.Context, request CopyRequest) (*CopyResult, error) {
	if request.NonInteractive {
		s.options.Terminal = func() (bool, error) { return false, nil }
	}
	names := memberNames(request.Names)
	if len(names) == 0 || request.Source.Path == "" || request.Destination.Path == "" || request.Source.Remote == request.Destination.Remote {
		return nil, failure("cli.usage", "copy requires machine names and one local and one remote path", "use machine cp --help")
	}
	if s.options.Copier == nil || s.state == nil || s.options.Lender == nil {
		return nil, failure("access.unavailable", "file copying is not configured", "use a build with machine cp support")
	}
	selected, err := machine.SelectedContext(ctx, s.selection, request.ContextName)
	if err != nil {
		return nil, err
	}
	effective, err := s.state.RenderEffective(ctx, compilation.EffectiveRequest{ContextName: selected})
	if err != nil {
		return nil, err
	}
	var members []member
	if len(names) > 1 {
		members, err = s.members(ctx, effective.Effective, selected, names, request.SSH, "cp")
	} else {
		var resolved target
		resolved, err = resolveTarget(effective.Effective, names[0], selected)
		if err == nil {
			var identity machine.Identity
			var ref string
			identity, ref, err = s.identity(ctx, effective.Effective, resolved, request.SSH)
			members = []member{{target: resolved, identity: identity, privateKeyRef: ref}}
		}
	}
	if err != nil {
		return nil, err
	}
	result := &CopyResult{Context: selected, Source: request.Source}
	err = s.options.Lender.WithMaterial(ctx, lifecycle.MaterialRequest{ContextName: selected, Secrets: memberReferences(members)}, func(inner context.Context, material map[string]secrets.Material) error {
		sessions, err := s.prove(inner, selected, members, nil, material, declaredMachines(effective.Effective))
		defer func() {
			for _, session := range sessions {
				clear(session.PrivateKey)
			}
		}()
		if err != nil {
			return err
		}
		interactive, err := s.interactive()
		if err != nil {
			return err
		}
		seen := map[string]bool{}
		for i := range sessions {
			sessions[i].Batch = len(names) > 1 || request.NonInteractive || !interactive
			if sessions[i].Batch && sessions[i].Kind == machine.IdentityPassword {
				return failure("access.unavailable", "Machine "+sessions[i].Machine+" needs interactive password authentication", "copy this Machine alone from an interactive terminal")
			}
			key := sessions[i].HostKey.Fingerprint() + "/" + sessions[i].User
			if !request.Source.Remote && seen[key] {
				return failure("access.target", "several selected Machines share an upload destination identity", "select one Machine per SSH host identity and account")
			}
			seen[key] = true
		}
		if len(sessions) == 1 && sessions[0].Kind == machine.IdentityPassword {
			s.advise(members[0].target, selected)
		}
		return s.copySessions(inner, request, sessions, result)
	})
	return result, err
}

func (s Service) copySessions(ctx context.Context, request CopyRequest, sessions []machine.Session, result *CopyResult) error {
	prepared := make([]PreparedCopy, len(sessions))
	defer func() {
		for _, p := range prepared {
			if p != nil {
				_ = p.Close()
			}
		}
	}()
	for i, session := range sessions {
		p, err := s.options.Copier.Prepare(ctx, session, request, request.Source.Remote && len(sessions) > 1)
		if err != nil {
			result.Machines = nil
			return failure("access.copy", "Machine "+session.Machine+": copy preflight failed: "+err.Error(), "correct the paths, permissions or Python 3 availability, then retry")
		}
		prepared[i] = p
		result.Machines = append(result.Machines, CopyRow{Machine: session.Machine, Destination: p.Destination(), Status: "skipped"})
	}
	var group sync.WaitGroup
	slots := make(chan struct{}, maxConcurrentSessions)
	errorsByMachine := make([]error, len(sessions))
	for i, p := range prepared {
		select {
		case slots <- struct{}{}:
		case <-ctx.Done():
		}
		if ctx.Err() != nil {
			break
		}
		group.Go(func() {
			defer func() { <-slots }()
			progress, err := p.Run(ctx)
			row := &result.Machines[i]
			row.Files, row.Bytes, row.Status = progress.Files, progress.Bytes, "copied"
			if err != nil {
				row.Status = "failed"
				if errors.Is(err, context.Canceled) || ctx.Err() != nil {
					row.Status = "canceled"
				}
				if progress.Unknown {
					row.Status = "unknown"
				}
				errorsByMachine[i] = failure("access.copy", "Machine "+row.Machine+": copy "+row.Status+": "+err.Error(), "inspect the destination before retrying; completed files remain")
			}
		})
	}
	group.Wait()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	reported := []diagnostics.Diagnostic{}
	for _, err := range errorsByMachine {
		reported = append(reported, diagnostics.Of(err)...)
	}
	if len(reported) != 0 {
		return &diagnostics.Failure{Diagnostics: reported}
	}
	return nil
}
