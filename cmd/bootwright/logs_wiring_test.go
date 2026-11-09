package main

import (
	"testing"

	"github.com/crmarques/bootwright/internal/reconciliation/ansiblerunner"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/reconciliation/logview"
	"github.com/crmarques/bootwright/internal/workspace/contextfs"
)

// Logs and status --paths read through the one context store lifecycle
// writes, so neither answers from another root.
func TestLogsAndContextPathsUseTheContextStore(t *testing.T) {
	deps, release := localServiceDependencies(processDependencies{})
	defer release()
	store, ok := deps.Lifecycle.Workspace.(*contextfs.Store)
	if !ok || deps.Lifecycle.Paths != lifecycle.ContextPaths(store) || deps.Lifecycle.Logs != logview.Workspace(store) {
		t.Fatalf("lifecycle reads %T, paths %T and logs %T, want one context store", deps.Lifecycle.Workspace, deps.Lifecycle.Paths, deps.Lifecycle.Logs)
	}
	if options := lifecycleOptions(deps.Lifecycle, systemClock{}, ansiblerunner.Runner{}); options.Paths != deps.Lifecycle.Paths {
		t.Fatal("status is wired with no context paths")
	}
	if _, ok := assembleServices(deps).Logs.(logview.Service); !ok {
		t.Fatal("bootwright logs is wired with no log view")
	}
}
