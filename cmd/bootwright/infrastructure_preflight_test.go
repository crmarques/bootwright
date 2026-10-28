package main

import (
	"context"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	environmentpreflight "github.com/crmarques/bootwright/internal/environment/preflight"
	"github.com/crmarques/bootwright/internal/substrate/capacity"
)

type staticGraph struct{ catalog api.Catalog }

func (g staticGraph) Effective(context.Context, string) (api.Catalog, error) { return g.catalog, nil }

type staticController struct{ report *prerequisites.Report }

func (c staticController) Check(context.Context, prerequisites.CheckRequest) (*prerequisites.Report, error) {
	return c.report, nil
}

type staticCapacity struct{}

func (staticCapacity) CheckCapacity(context.Context, api.Catalog, string, bool) ([]capacity.Check, error) {
	return nil, nil
}

func TestInfrastructurePreflightKeepsEveryControllerSocketAsItsOwnCheck(t *testing.T) {
	state, _ := compileAcceptance(t, exampleDirectory(t, "libvirt-3-nodes-ceph"))
	catalog := state.Effective()
	rows, err := firewallPreflight(&firewallProbe{status: "closed"})(context.Background(), catalog, "lab")
	if err != nil || len(rows) < 2 {
		t.Fatal(rows, err)
	}
	service := environmentpreflight.Service{
		Graph: staticGraph{catalog}, Selection: func(context.Context) (string, error) { return "lab", nil },
		Controller: staticController{&prerequisites.Report{Checks: rows}}, Capacity: staticCapacity{},
	}
	report, _ := service.PreflightInfrastructure(context.Background(), environmentpreflight.InfrastructurePreflightRequest{})
	var listeners int
	seen := map[string]bool{}
	for _, check := range report.Checks {
		if check.ID != "controller.listener-firewall" {
			continue
		}
		listeners++
		seen[check.Target] = true
		if check.Status != capacity.Failed || !strings.Contains(check.Target, ":") || !strings.Contains(check.Target, "/") || !strings.HasPrefix(check.Reason, "required ") {
			t.Fatalf("listener check %+v", check)
		}
	}
	if listeners != len(rows) || len(seen) != len(rows) {
		t.Fatalf("%d firewall rows became %d checks; sockets of one service must not merge", len(rows), listeners)
	}
}
