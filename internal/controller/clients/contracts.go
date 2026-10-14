package clients

import (
	"context"

	"github.com/crmarques/bootwright/internal/controller"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
)

// ToolCatalog recovers, resolves and proves the target clients a context
// selects. Select is pure: it recovers exact retained identities without
// reading publisher metadata, so a prepared host proves its clients offline.
type ToolCatalog interface {
	Select([]controller.ToolRequest, []prerequisites.DependencySource) ([]prerequisites.ToolDefinition, bool, error)
	Resolve(context.Context, []controller.ToolRequest, prerequisites.SetupEgress) ([]prerequisites.ToolDefinition, error)
	Present(context.Context, prerequisites.BundleArea, []prerequisites.ToolDefinition) (bool, error)
}

// HostPackages opens the host's own package manager over the route the
// controller Machine selected, which dnf receives as its proxy.
type HostPackages func(prerequisites.SetupEgress) (prerequisites.HostPackages, error)

// SetupLock holds setup/lock for the whole host package effect, so setup and
// another context's stage never run dnf beside it.
type SetupLock interface {
	MutateSetup(context.Context, func(prerequisites.SetupTransaction) error) error
}

// Installer runs the fixed controller tool automation inside a runtime the
// caller already holds.
type Installer interface {
	Clients(context.Context, prerequisites.ClientInstallation) (prerequisites.ActionResult, error)
}
