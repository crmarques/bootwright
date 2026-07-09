package cli

import (
	"github.com/crmarques/bootwright/internal/converge"
)

type scopeDryRunReport = converge.DryRunReport

var clustersScope = struct{ destroyPlaybook string }{destroyPlaybook: converge.ClustersScope.DestroyPlaybook}

const (
	infraDestroyArtifactServerPlaybook     = converge.InfraDestroyArtifactServerPlaybook
	infraComponentServiceScopeExtraVarName = converge.InfraComponentServiceScopeExtraVarName
)
