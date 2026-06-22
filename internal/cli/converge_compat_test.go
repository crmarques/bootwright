package cli

import (
	"github.com/crmarques/bootwright/internal/converge"
)

// cli_test.go compatibility aliases over symbols that moved to the converge
// bounded-context root. Production code uses the converge package directly.
type scopeDryRunReport = converge.DryRunReport

// clustersScope mirrors the field cli_test.go reads from the old scope table.
var clustersScope = struct{ destroyPlaybook string }{destroyPlaybook: converge.ClustersScope.DestroyPlaybook}

const (
	infraDestroyArtifactServerPlaybook     = converge.InfraDestroyArtifactServerPlaybook
	infraComponentServiceScopeExtraVarName = converge.InfraComponentServiceScopeExtraVarName
)
