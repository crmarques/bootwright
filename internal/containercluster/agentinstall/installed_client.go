package agentinstall

import (
	"github.com/crmarques/bootwright/internal/controller/evidence"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

func InstalledClient(records []lifecycle.BlockEvidence, contextName, cluster, executable string) (evidence.InstalledTool, bool, error) {
	for _, record := range records {
		if record.Implementation != InstallImplementation || record.Verb != reconciliation.Apply || record.State != reconciliation.BlockDone {
			continue
		}
		request, err := DecodeInstallRequest(record.Block.Request)
		if err != nil {
			return evidence.InstalledTool{}, false, err
		}
		if request.Identity.Cluster != cluster || request.Identity.Context != contextName {
			continue
		}
		return evidence.InstalledTool{Kind: "openshift-clients", Compatibility: request.Release.Distribution, Version: request.Release.Version, Executable: executable}, true, nil
	}
	return evidence.InstalledTool{}, false, nil
}
