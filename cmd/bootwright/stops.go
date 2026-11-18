package main

import (
	"slices"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/managedos/hostfirewall"
	"github.com/crmarques/bootwright/internal/reconciliation/ansiblerunner"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

func stopCatalog() []diagnostics.Stop {
	return slices.Concat(ansiblerunner.Stops(), lifecycle.Stops(), hostfirewall.Stops())
}
