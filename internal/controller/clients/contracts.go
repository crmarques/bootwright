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

// NativeResolver solves the native client transaction against the host's
// current inventory. It applies nothing.
type NativeResolver interface {
	Resolve(context.Context, prerequisites.Platform, prerequisites.NativeRequirements, controller.DependencyVersions, prerequisites.SetupEgress) (prerequisites.NativeResolvedPlan, error)
}

// NativeInspector reports whether the selected native root packages are
// installed by name, without repository metadata or installed-state change.
type NativeInspector interface {
	Check(context.Context, prerequisites.NativeResolvedPlan) (prerequisites.NativePresence, error)
}

// Installer runs the fixed controller dependency automation inside an
// execution foundation the caller already holds.
type Installer interface {
	Clients(context.Context, prerequisites.ClientInstallation) (prerequisites.ActionResult, error)
}
