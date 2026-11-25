package agentinstall

import (
	"path/filepath"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/reconciliation"
)

const installerLogFile = ".openshift_install.log"
const installerLogNote = "installer-log"

type installerLogPlacement struct {
	Identity  Identity          `json:"identity"`
	Placement machine.Placement `json:"placement"`
	WorkRoot  string            `json:"workRoot"`
}

func (p installerLogPlacement) location() string {
	return installerTool + " log on the controller: " + filepath.Join(p.WorkRoot, installerLogFile)
}

func installerGroup(implementation, group string) bool {
	switch implementation {
	case MediaImplementation:
		return group == "build-image"
	case InstallImplementation:
		return group == "wait-bootstrap" || group == "wait-install"
	default:
		return false
	}
}

func installerLogGroupNote(operation, implementation string, work string) func(string, string) (string, bool) {
	if operation != "apply" {
		return nil
	}
	p := installerLogPlacement{WorkRoot: work}
	return func(group, detail string) (string, bool) {
		if detail != installerLogNote || !installerGroup(implementation, group) {
			return "", false
		}
		return p.location(), true
	}
}

// InstallerLog finds only the controller-local file of a frozen installer
// block. An event can activate it, but cannot supply a pathname.
type InstallerLog struct {
	placement      installerLogPlacement
	implementation string
}

func InstallerLogForBlock(contextName string, block reconciliation.Block) (InstallerLog, bool) {
	var placement installerLogPlacement
	switch block.Implementation {
	case MediaImplementation:
		request, err := reconciliation.ThawVersion[MediaRequest](block.Request, "cluster media", mediaRequestVersion)
		if err != nil {
			return InstallerLog{}, false
		}
		placement = installerLogPlacement{Identity: request.Identity, Placement: request.Placement, WorkRoot: request.WorkRoot}
	case InstallImplementation:
		request, err := reconciliation.ThawVersion[InstallRequest](block.Request, "cluster install", installRequestVersion)
		if err != nil {
			return InstallerLog{}, false
		}
		placement = installerLogPlacement{Identity: request.Identity, Placement: request.Placement, WorkRoot: request.WorkRoot}
	default:
		return InstallerLog{}, false
	}
	if onTheController(placement.Placement, "installer") != nil ||
		!api.ValidLexical(api.LexicalName, contextName) || !api.ValidLexical(api.LexicalName, block.Object) ||
		placement.Identity != (Identity{Block: block.ID, Context: contextName, Cluster: block.Object}) ||
		placement.WorkRoot != WorkRoot(contextName, block.Object) {
		return InstallerLog{}, false
	}
	return InstallerLog{placement: placement, implementation: block.Implementation}, true
}

func (l InstallerLog) Path() string {
	return filepath.Join(l.placement.WorkRoot, installerLogFile)
}

func (l InstallerLog) Active(group, detail string) bool {
	return installerGroup(l.implementation, group) && detail == "running "+l.placement.location()
}
