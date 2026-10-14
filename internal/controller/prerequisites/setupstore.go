package prerequisites

import (
	"errors"

	"github.com/crmarques/bootwright/internal/diagnostics"
)

const SetupStateFormat = "bootwright.setup-state-v1"

const PresentBeforeBootwright = "present before bootwright"

const setupStateRemedy = "run bootwright setup"

var ErrRuntimeUnavailable = errors.New("the controller runtime is not prepared")

type SetupState struct {
	Format   string        `json:"format"`
	Pending  *PendingSetup `json:"pending,omitempty"`
	Receipt  *HostReceipt  `json:"receipt,omitempty"`
	Runtimes SetupRuntimes `json:"runtimes"`
}

type PendingSetup struct {
	ID         string   `json:"id"`
	PlanDigest string   `json:"planDigest"`
	Actions    []string `json:"actions"`
	StartedAt  string   `json:"startedAt"`
}

type HostReceipt struct {
	Platform ReceiptPlatform  `json:"platform"`
	Lock     string           `json:"lock"`
	Runtime  ReceiptRuntime   `json:"runtime"`
	Closure  string           `json:"closure"`
	Packages []ReceiptPackage `json:"packages"`
}

type ReceiptPlatform struct {
	ID           string   `json:"id"`
	VersionID    string   `json:"versionId"`
	Like         []string `json:"like"`
	Architecture string   `json:"architecture"`
	Glibc        string   `json:"glibc"`
	Tested       bool     `json:"tested"`
}

type ReceiptRuntime struct {
	Digest      string `json:"digest"`
	Interpreter string `json:"interpreter"`
	Python      string `json:"python"`
	ABI         string `json:"abi"`
	AnsibleCore string `json:"ansibleCore"`
}

type ReceiptPackage struct {
	Requirement string   `json:"requirement"`
	RequiredBy  []string `json:"requiredBy"`
	Provider    string   `json:"provider"`
	Repository  string   `json:"repository"`
	InstalledBy string   `json:"installedBy"`
}

type SetupRuntimes struct {
	Current  string `json:"current,omitempty"`
	Previous string `json:"previous,omitempty"`
}

type SetupView struct {
	Exists  bool
	State   SetupState
	Foreign string
}

func SetupLaunchRefusal(v SetupView) error {
	switch {
	case v.Foreign != "":
		return diagnostics.NewFailureWithRemediation("controller.state", "the controller setup record was written by another Bootwright build", "", setupStateRemedy)
	case v.State.Pending != nil:
		return diagnostics.NewFailureWithRemediation("controller.state", "a controller setup ("+v.State.Pending.ID+") was interrupted before it finished", "", setupStateRemedy)
	}
	return nil
}

func DiscardForeignSetupAction(format string) string {
	return "Discard setup state another Bootwright build wrote (" + format + ")"
}

func InterruptedSetupNotice(id, step string) string {
	return "An earlier setup (" + id + ") was interrupted during " + step + "; this run re-inspects the host and continues"
}
