package main

import (
	"context"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/cli"
	"github.com/crmarques/bootwright/internal/desiredstate/inputfs"
	"github.com/crmarques/bootwright/internal/environment/access"
	"github.com/crmarques/bootwright/internal/machine"
	machineaccess "github.com/crmarques/bootwright/internal/machine/access"
	"github.com/crmarques/bootwright/internal/machine/sessionpolicy"
	"github.com/crmarques/bootwright/internal/workspace/contexts"
)

type policyCommands struct {
	service    sessionpolicy.Service
	account    accountResolver
	delegated  bool
	files      inputfs.Reader
	graph      effectiveGraph
	selection  func(context.Context) (string, error)
	repository interface {
		View(context.Context) (contexts.Registry, error)
	}
	hold      func(context.Context) (func(), error)
	publish   func(context.Context, contexts.Record, []byte) error
	remove    func(context.Context, contexts.Record, []byte) (bool, error)
	confirmer contexts.Confirmer
}

func (p policyCommands) selectedRecord(ctx context.Context, name string) (contexts.Record, error) {
	contextName, err := machine.SelectedContext(ctx, p.selection, name)
	if err != nil {
		return contexts.Record{}, err
	}
	registry, err := p.repository.View(ctx)
	if err != nil {
		return contexts.Record{}, err
	}
	for _, record := range registry.Contexts {
		if record.Name == contextName {
			return record, nil
		}
	}
	return contexts.Record{}, contexts.AbsentContext(contextName)
}

func (p policyCommands) Set(ctx context.Context, name, file string, check bool) (string, []sessionpolicy.Grant, error) {
	record, err := p.selectedRecord(ctx, name)
	if err != nil {
		return "", nil, err
	}
	catalog, err := p.graph.Effective(ctx, record.Name)
	if err != nil {
		return "", nil, err
	}
	data, err := p.files.ReadFile(ctx, file, sessionpolicy.MaxBytes)
	if err != nil {
		return "", nil, err
	}
	grants, err := p.service.Expand(ctx, record.Name, catalog, data)
	if err != nil {
		return "", nil, err
	}
	if check {
		return record.Name, grants, nil
	}
	return record.Name, grants, p.publish(ctx, record, data)
}

func (p policyCommands) Show(ctx context.Context, name string) (string, *api.SessionAccessPolicy, []byte, error) {
	record, err := p.selectedRecord(ctx, name)
	if err != nil {
		return "", nil, nil, err
	}
	policy, data, err := p.service.Installed(ctx, record.Name)
	return record.Name, policy, data, err
}

func (p policyCommands) Delete(ctx context.Context, name string, confirmed bool) (string, bool, error) {
	record, err := p.selectedRecord(ctx, name)
	if err != nil {
		return "", false, err
	}
	data, err := p.service.Store.ReadSessionPolicy(ctx, record.Name)
	if err != nil {
		return "", false, err
	}
	if len(data) == 0 {
		return record.Name, false, nil
	}
	if !confirmed {
		if err := p.confirmer.Confirm(ctx, "delete session policy", record.Name); err != nil {
			return "", false, err
		}
	}
	deleted, err := p.remove(ctx, record, data)
	return record.Name, deleted, err
}

func (p policyCommands) Permissions(ctx context.Context, contextName string) (string, []sessionpolicy.Grant, error) {
	selected, err := machine.SelectedContext(ctx, p.selection, contextName)
	if err != nil {
		return "", nil, err
	}
	principal, err := p.principal(ctx)
	if err != nil {
		return "", nil, err
	}
	release, err := p.hold(ctx)
	if err != nil {
		return "", nil, err
	}
	defer release()
	catalog, err := p.graph.Effective(ctx, selected)
	if err != nil {
		return "", nil, err
	}
	grants, err := p.service.Permissions(ctx, principal, selected, catalog)
	return selected, grants, err
}

func (p policyCommands) authorize(ctx context.Context, name string, catalog api.Catalog, targets []machine.SessionTarget, argv []string) error {
	principal, err := p.principal(ctx)
	if err != nil {
		return err
	}
	return p.service.Authorize(ctx, principal, name, catalog, targets, argv)
}

func (p policyCommands) principal(ctx context.Context) (sessionpolicy.Principal, error) {
	account, err := p.account.Resolve(ctx)
	if err != nil || p.delegated && (account.SudoParentPID == 0 || account.UID == 0) {
		return sessionpolicy.Principal{}, sessionpolicy.UnverifiedCaller()
	}
	return sessionpolicy.Principal{UID: uint32(account.UID), Groups: account.Groups}, nil
}

type delegatedSessions struct {
	hold      func(context.Context) (func(), error)
	selection func(context.Context) (string, error)
	direct    cli.MachineAccessService
	cluster   cli.EnvironmentAccessService
}

type admissionKey struct{}

func sessionPrepared(ctx context.Context) {
	if release, ok := ctx.Value(admissionKey{}).(func()); ok {
		release()
	}
}

func (d delegatedSessions) withAdmission(ctx context.Context, name string, invokeSession func(context.Context, string) (*machine.SessionResult, error)) (*machine.SessionResult, error) {
	selected, err := machine.SelectedContext(ctx, d.selection, name)
	if err != nil {
		return nil, err
	}
	release, err := d.hold(ctx)
	if err != nil {
		return nil, err
	}
	defer release()
	return invokeSession(context.WithValue(ctx, admissionKey{}, release), selected)
}

func (d delegatedSessions) Exec(ctx context.Context, request machineaccess.ExecRequest) (*machine.SessionResult, error) {
	return d.withAdmission(ctx, request.ContextName, func(ctx context.Context, selected string) (*machine.SessionResult, error) {
		request.ContextName = selected
		return d.direct.Exec(ctx, request)
	})
}

func (d delegatedSessions) Rsh(ctx context.Context, request machineaccess.RshRequest) (*machine.SessionResult, error) {
	return d.withAdmission(ctx, request.ContextName, func(ctx context.Context, selected string) (*machine.SessionResult, error) {
		request.ContextName = selected
		return d.direct.Rsh(ctx, request)
	})
}

func (d delegatedSessions) ClusterExec(ctx context.Context, request access.ClusterExecRequest) (*machine.SessionResult, error) {
	return d.withAdmission(ctx, request.ContextName, func(ctx context.Context, selected string) (*machine.SessionResult, error) {
		request.ContextName = selected
		return d.cluster.ClusterExec(ctx, request)
	})
}

func (d delegatedSessions) ClusterRsh(ctx context.Context, request access.ClusterRshRequest) (*machine.SessionResult, error) {
	return d.withAdmission(ctx, request.ContextName, func(ctx context.Context, selected string) (*machine.SessionResult, error) {
		request.ContextName = selected
		return d.cluster.ClusterRsh(ctx, request)
	})
}

func policyRoster() func(api.Object) ([]string, bool) {
	members := clusterMembers()
	return func(cluster api.Object) ([]string, bool) {
		roster, found := members(cluster)
		names := make([]string, 0, len(roster.Nodes))
		for _, node := range roster.Nodes {
			names = append(names, node.Machine)
		}
		return names, found
	}
}
