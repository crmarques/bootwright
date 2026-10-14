package power

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

// target is one resolved Machine: its frozen request and its pin, if any.
type target struct {
	frozen Request
	pin    machine.HardwareIdentity
}

// normalized trims, drops empty and repeated names, and sorts them bytewise.
func normalized(names []string) ([]string, error) {
	var members []string
	for _, name := range names {
		if name = strings.TrimSpace(name); name != "" {
			members = append(members, name)
		}
	}
	slices.Sort(members)
	members = slices.Compact(members)
	if len(members) == 0 {
		return nil, &diagnostics.Failure{Usage: true, Diagnostics: []diagnostics.Diagnostic{{
			Severity: "error", Code: "cli.usage", Message: "--name names no machine",
			Remediation: "name each machine to act on with --name, separated by commas",
		}}}
	}
	return members, nil
}

// resolve freezes every member and reads its pin before anything is asked or
// run; any other refusal than an undeclared name is the first Machine's.
func (s Service) resolve(ctx context.Context, catalog api.Catalog, contextName string, names []string, request PowerRequest) ([]target, error) {
	if err := undeclared(catalog, contextName, names); err != nil {
		return nil, err
	}
	command := batchCommand(contextName, request.Verb, names, request.Force)
	targets := make([]target, 0, len(names))
	for _, name := range names {
		resolved, err := s.resolveOne(ctx, catalog, contextName, name, command, request)
		if err != nil {
			if len(names) > 1 && len(diagnostics.Of(err)) != 0 {
				return nil, attributed(err, name)
			}
			return nil, err
		}
		targets = append(targets, resolved)
	}
	return targets, nil
}

func (s Service) resolveOne(ctx context.Context, catalog api.Catalog, contextName, name, command string, request PowerRequest) (target, error) {
	frozen, physical, err := requestFor(catalog, contextName, name, request.Verb, request.Force, s.realized(ctx, contextName))
	if err != nil {
		return target{}, err
	}
	if err := refuseTemplateValues(contextName, command, frozen); err != nil {
		return target{}, err
	}
	resolved := target{frozen: frozen}
	if physical {
		proved, found, err := s.identities.ProvedIdentity(ctx, contextName, frozen.Identity.Object)
		if err != nil {
			return target{}, err
		}
		if found {
			resolved.pin = proved
		}
	}
	return resolved, nil
}

// confirm asks once, for every Machine, before a stop or restart.
func (s Service) confirm(ctx context.Context, contextName string, request PowerRequest, targets []target) error {
	if request.Verb == Start || request.SkipConfirmation {
		return nil
	}
	names := make([]string, 0, len(targets))
	for _, resolved := range targets {
		names = append(names, resolved.frozen.Identity.Object)
	}
	if s.confirmer == nil {
		return failure("machine.power", "this operation requires confirmation",
			"repeat "+batchCommand(contextName, request.Verb, names, request.Force)+" with --yes")
	}
	action := request.Verb + " machine"
	if len(names) > 1 {
		action += "s"
	}
	if request.Force {
		action = "force " + action
	}
	object := strings.Join(names, ", ")
	var err error
	if confirmer, ok := s.confirmer.(ContextConfirmer); ok {
		err = confirmer.ConfirmIn(ctx, action, object, contextName)
	} else {
		err = s.confirmer.Confirm(ctx, action, object)
	}
	if err != nil && ctx.Err() != nil {
		return ctx.Err()
	}
	return err
}

// executeAll runs at most lifecycle.MaxRunningBlocks Machines at once inside
// one invocation scope, or one at a time without a scope.
func (s Service) executeAll(ctx context.Context, contextName, verb string, targets []target) (*Results, error) {
	results := &Results{Context: contextName, Verb: verb}
	if len(targets) == 1 {
		row, err := s.execute(ctx, contextName, targets[0], 1, 1)
		if row != nil {
			results.Machines = []Result{*row}
		}
		return results, err
	}
	scoped, limit := ctx, 1
	if s.begin != nil {
		var release func()
		scoped, release = s.begin(ctx)
		defer release()
		limit = lifecycle.MaxRunningBlocks
	}
	rows, errs := make([]*Result, len(targets)), make([]error, len(targets))
	slots := make(chan struct{}, limit)
	var workers sync.WaitGroup
	for index, resolved := range targets {
		select {
		case slots <- struct{}{}:
		case <-scoped.Done():
		}
		if scoped.Err() != nil {
			break
		}
		workers.Go(func() {
			defer func() { <-slots }()
			rows[index], errs[index] = s.execute(scoped, contextName, resolved, index+1, len(targets))
		})
	}
	workers.Wait()
	for _, row := range rows {
		if row != nil {
			results.Machines = append(results.Machines, *row)
		}
	}
	if err := ctx.Err(); err != nil {
		return results, err
	}
	return results, aggregate(targets, errs)
}

// aggregate names every failed Machine in one failure, one run's own deadline included.
func aggregate(targets []target, errs []error) error {
	var reported []diagnostics.Diagnostic
	for index, err := range errs {
		if err == nil {
			continue
		}
		reported = append(reported, diagnostics.Of(attributed(err, targets[index].frozen.Identity.Object))...)
	}
	if len(reported) == 0 {
		return nil
	}
	return &diagnostics.Failure{Diagnostics: reported}
}

// attributed names the Machine on each diagnostic that names no object.
func attributed(err error, name string) error {
	identity := diagnostics.ObjectIdentity{APIVersion: api.APIVersion, Kind: string(api.Machine), Name: name}
	reported := diagnostics.Of(err)
	if len(reported) == 0 {
		reported = []diagnostics.Diagnostic{undiagnosed(err)}
	}
	for index := range reported {
		if reported[index].Object == nil {
			object := identity
			reported[index].Object = &object
		}
	}
	return &diagnostics.Failure{Diagnostics: reported, Usage: diagnostics.IsUsage(err)}
}

func undiagnosed(err error) diagnostics.Diagnostic {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return diagnostics.Diagnostic{Severity: "error", Code: "runtime.deadline", Message: "the power run of this Machine exceeded its deadline"}
	case errors.Is(err, context.Canceled):
		return diagnostics.Diagnostic{Severity: "error", Code: "runtime.canceled", Message: "the power run of this Machine was canceled"}
	}
	return diagnostics.Diagnostic{Severity: "error", Code: "runtime.internal", Message: "the power run of this Machine failed without a diagnostic"}
}
