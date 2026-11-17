package physicalerase

import (
	"slices"
	"strconv"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
)

func refusal(code, message, remediation string) error {
	return diagnostics.NewFailureWithRemediation(code, message, "", remediation)
}

const (
	eraseMachineRunning     = "machine-running"
	eraseIdentityMismatch   = "identity-mismatch"
	eraseHardwareMismatch   = "hardware-mismatch"
	eraseTargetUnanswered   = "target-unanswered"
	eraseControllerRefused  = "controller-refused"
	eraseControllerDeadline = "controller-deadline"
	erasePowerOffUnproved   = "poweroff-unproved"
	eraseWorkerFailed       = "worker-failed"
	eraseWorkerUnreachable  = "worker-unreachable"
	eraseForeignWorker      = "foreign-worker"
	eraseReceiptTimeout     = "receipt-timeout"
	eraseForeignCustody     = "foreign-custody"
	eraseCustodyDrift       = "custody-drift"
	eraseBootMediaMismatch  = "boot-media-mismatch"
	eraseImageBuildFailed   = "image-build-failed"
)

const eraseCustodyRoot = "/var/lib/bootwright-hosted"

func eraseCustody(contextName, machine, applyOperation string) string {
	return eraseCustodyRoot + "/" + contextName + "/machines/" + machine + "/erase/" + applyOperation
}

func erasePublication(request EraseRequest) string {
	if request.Install.Private == nil {
		return "its private erase publication directory"
	}
	return request.Install.Private.Path
}

func eraseRefusals(contextName, continuation, machine, endpoint, address, custody, publication string) map[string]error {
	identity := string(api.Machine) + "/" + machine
	refused := func(message, remediation string) error {
		return &diagnostics.Failure{Diagnostics: []diagnostics.Diagnostic{{
			Severity: "error", Code: "lifecycle.state", Message: message, Remediation: remediation,
			Object: &diagnostics.ObjectIdentity{APIVersion: api.APIVersion, Kind: string(api.Machine), Name: machine},
		}}}
	}
	next := continuation
	if next == "" {
		next = "bootwright destroy --context " + contextName + " again"
	}
	again := "then run " + next
	stop := "stop it with bootwright machine stop --context " + contextName + " --name " + machine + " --force, "
	repeats := ", which boots the erase again and repeats every target's write and readback"
	retain := "; or, to release it without erasing its disks, run bootwright destroy --context " + contextName + " --authorize erase=" + machine + ":retain"
	cause := "read the controller's line in the attempt's retained output and correct its cause through the BMC; "
	stopped := ", so the physical erase stopped before it acted through it"
	return map[string]error{
		eraseMachineRunning: refused(identity+" is running, and its offline erase boots it from erase media, so no media was inserted into it and it was not booted",
			stop+again),
		eraseIdentityMismatch: refused("the management controller at "+endpoint+" answers as another system than the one the apply of "+identity+" proved"+stopped,
			"correct the controller at "+endpoint+" so it answers for "+identity+", "+again+retain),
		eraseHardwareMismatch: refused("the machine answering at "+endpoint+" does not report exactly the hardware addresses "+identity+
			" declares, or its inventory could not be read in full"+stopped,
			"once the machine reports the hardware "+identity+" declares in full, "+again),
		eraseTargetUnanswered: refused("the management controller at "+endpoint+" did not answer the system read for "+identity+stopped,
			cause+"restore its access, "+again),
		eraseControllerRefused: refused("the management controller at "+endpoint+" refused a virtual-media, boot or power request of the physical erase of "+identity,
			cause+"when the machine was not proved off, "+stop+again+repeats),
		eraseControllerDeadline: refused("the management controller at "+endpoint+" did not complete a phase of the physical erase of "+identity+" within its time bound",
			"restore the controller's responsiveness; when the machine was not proved off, "+stop+again+repeats),
		erasePowerOffUnproved: refused(identity+" did not power off after its erase receipt was acknowledged, so its erase media was not ejected",
			stop+again+repeats),
		eraseWorkerFailed: refused("the RAM erase worker of "+identity+" stopped without a complete receipt, so no target is proved erased",
			stop+again+repeats),
		eraseWorkerUnreachable: refused("the controller did not reach the RAM erase worker of "+identity+" at "+address+
			" within 20 minutes of the wait's start, or for 10 minutes after its last answer, so no target is proved erased",
			"check that the controller reaches "+address+" on port 22 (the address "+identity+" was installed with) and look at its BMC console; then "+
				stop+again+repeats+retain),
		eraseForeignWorker: refused("the host answering at "+address+" for "+identity+" is not this removal's RAM erase worker: it belongs to another request or removal attempt, or it answered with another host key, as the installed system does when the machine did not boot its erase media, so its receipt was not accepted",
			"look at the BMC console and boot order of "+identity+" to see what booted; "+stop+again+repeats+retain),
		eraseReceiptTimeout: refused("the RAM erase worker of "+identity+" did not report a complete receipt within the erase deadline",
			stop+again+repeats),
		eraseForeignCustody: refused("the private erase custody of "+identity+" on the execution host belongs to another request, so it was not used",
			"restore the original apply and removal records, "+again+retain),
		eraseCustodyDrift: refused("the private erase custody of "+identity+" on the execution host differs from what its removal recorded or cannot be read, so it was not used",
			"inspect "+custody+" on the execution host as root: first remove the private erase image directory its custody.json names under "+publication+
				" (when it names none you can read, remove there only the erase images of "+identity+"), then remove "+custody+" whole, "+again),
		eraseBootMediaMismatch: refused("the installer image the apply of "+identity+" pinned is missing from the controller's media store or no longer matches its size and SHA-256, so no erase image was built and the machine was not booted",
			"restore that image with bootwright media add and check it with bootwright media list --checksums, "+again),
		eraseImageBuildFailed: refused("the erase image of "+identity+" could not be built from its pinned installer image within its time bound, so the machine was not booted",
			"check the execution host's free space and its mkksiso and xorriso installation, "+again),
	}
}

var eraseSessionCauses = []string{"timeout", "refused", "no-route", "auth", "exit"}

func eraseGroupIDs(block reconciliation.Block) []string {
	ids := make([]string, 0, len(block.Groups))
	for _, group := range block.Groups {
		ids = append(ids, group.ID)
	}
	return ids
}

func eraseGroupNote(address string) func(string, string) (string, bool) {
	return func(_, detail string) (string, bool) {
		parts := strings.Split(detail, ":")
		if len(parts) != 3 || parts[0] != "unanswered" || !slices.Contains(eraseSessionCauses, parts[1]) {
			return "", false
		}
		minutes, err := strconv.Atoi(strings.TrimSuffix(parts[2], "min"))
		if err != nil || minutes < 1 || parts[2] != strconv.Itoa(minutes)+"min" {
			return "", false
		}
		return "waiting for the erase worker at " + address + ", no answer for " + strconv.Itoa(minutes) + " min (" + parts[1] + ")", true
	}
}
