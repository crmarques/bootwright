package lifecycle

import (
	"context"
	"slices"
	"strings"

	"github.com/crmarques/bootwright/internal/controller"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
)

type ProvedClient struct {
	Kind    string
	Version string
}

type OutdatedClient struct {
	Kind    string
	Release string
	Minimum string
	Apply   string
}

func (s Service) outdatedClients(ctx context.Context, store recordReader, contextName string, applied operationstore.Operation, plan reconciliation.Plan) ([]OutdatedClient, []diagnostics.Diagnostic) {
	if applied.ID == "" || applied.Verb != reconciliation.Apply || s.capabilities == nil {
		return nil, nil
	}
	var outdated []OutdatedClient
	var warnings []diagnostics.Diagnostic
	seen := map[string]bool{}
	for _, stage := range controllerStages(plan) {
		owner, clients := s.provedClients(ctx, store, applied, stage)
		for _, client := range clients {
			if seen[client.Kind] {
				continue
			}
			if found, warning := outdatedClient(contextName, owner, client); found != nil {
				seen[client.Kind] = true
				outdated, warnings = append(outdated, *found), append(warnings, warning)
			}
		}
	}
	return outdated, warnings
}

func (s Service) provedClients(ctx context.Context, store recordReader, applied operationstore.Operation, stage reconciliation.Block) (string, []ProvedClient) {
	capability, ok := s.capabilities.Resolve(stage.Kind, stage.Implementation)
	reader, reads := capability.(ProvedClientReader)
	if !ok || !reads {
		return "", nil
	}
	owner, err := ownerID(ctx, store, applied, stage.ID)
	if err != nil || owner == "" {
		return "", nil
	}
	proved, err := blockEvidence(ctx, store, owner, stage, reconciliation.Apply)
	if err != nil || proved.State != reconciliation.BlockDone {
		return "", nil
	}
	clients := reader.ProvedClients(stage, proved)
	slices.SortStableFunc(clients, func(x, y ProvedClient) int { return strings.Compare(x.Kind, y.Kind) })
	return owner, clients
}

func outdatedClient(contextName, owner string, client ProvedClient) (*OutdatedClient, diagnostics.Diagnostic) {
	pin, pinned, err := controller.PinOf(client.Kind)
	if err != nil || !pinned {
		return nil, diagnostics.Diagnostic{}
	}
	if order, comparable := controller.CompareReleases(client.Version, pin.Minimum); comparable && order >= 0 {
		return nil, diagnostics.Diagnostic{}
	}
	return &OutdatedClient{Kind: client.Kind, Release: client.Version, Minimum: pin.Minimum, Apply: owner}, diagnostics.Diagnostic{
		Severity: "warning", Code: "controller.outdated-client",
		Message: "apply " + owner + " proved " + client.Kind + " " + client.Version + " on this controller, below this build's minimum " + pin.Minimum + "; a settling apply and a removal keep running it",
		Remediation: "run " + contextCommand(contextName, string(reconciliation.Destroy)) + " and then " + contextCommand(contextName, string(reconciliation.Apply)) +
			" to install this build's default " + client.Kind + ", or keep the build that applied it",
	}
}

func (s Service) statusOutdatedClients(ctx context.Context, store recordReader, contextName string, operation operationstore.Operation, plan reconciliation.Plan, states map[string]reconciliation.BlockState, result *StatusResult) error {
	if operation.Verb != reconciliation.Apply {
		return nil
	}
	applied, _, err := appliedView(ctx, store, operation, plan, states)
	if err != nil {
		return err
	}
	var warned []diagnostics.Diagnostic
	result.OutdatedClients, warned = s.outdatedClients(ctx, store, contextName, operation, applied)
	result.Warnings = append(result.Warnings, warned...)
	return nil
}
