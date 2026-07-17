package cli

import (
	"github.com/crmarques/bootwright/internal/converge"
)

type scopeDryRunReport = converge.DryRunReport

const (
	infraDestroyArtifactServerPlaybook     = converge.InfraDestroyArtifactServerPlaybook
	infraComponentServiceScopeExtraVarName = converge.InfraComponentServiceScopeExtraVarName
)
