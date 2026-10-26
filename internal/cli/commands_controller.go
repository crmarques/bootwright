package cli

import (
	"context"
	"errors"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
)

func controllerPreflightCommand() commandSpec {
	return available(commandSpec{path: "preflight controller", short: "Check controller prerequisites", long: "Verify local controller prerequisites without installing or changing state. Omit --context for the host baseline; only an explicit nonempty --context adds that context's own target tools, the native closures its controller stage selects (the libvirt client, the hypervisor and the installer-media tooling) and its host binding."})
}

func controllerSetupCommand() commandSpec {
	return available(commandSpec{path: "setup", short: "Set up context-independent controller prerequisites", flags: []flagSpec{dryRunFlag(), confirmationFlag(), directoryFlag("dest-dir", "Write the locked wheels, the pip wheel and lock.json into this directory (requires --download-only)"), boolFlag("download-only", "Write the locked files for an offline host and change nothing else; never elevates"), stringFlag("wheel-mirror", "Read each locked wheel as <mirror>/<filename>: file:///<directory> or https://<host>/<base>; its bytes must still match the lock")}, long: "Prepare this host's context-independent controller prerequisites: the baseline host packages, installed through the host's own dnf, and the controller runtime, a venv on the OS Python holding exactly the wheels this build locks. Setup selects no context; the prerequisites a context adds are installed by the controller stage of apply. A ready host makes no request and no change. Otherwise the plan shows dnf's own preview before confirmation. --dry-run never elevates: it names the host package requirements without a preview and makes no changes, and --dry-run --output json or --output yaml also lists every locked wheel's file, URL and SHA-256. --wheel-mirror reads each locked wheel as <mirror>/<filename> from a file:/// directory or an HTTPS base, as untrusted transport: the lock's SHA-256 alone decides whether the bytes are used. --download-only --dest-dir DIR never elevates and changes only DIR: on any connected host it writes the locked wheels of every Python ABI, the pip wheel and a copy of lock.json, so DIR serves an offline host as a --wheel-mirror. Review the plan before confirming; --yes skips ordinary confirmation."})
}

func controllerRebindCommand() commandSpec {
	return available(commandSpec{path: "controller rebind", short: "Rebind the controller record to this host's changed identity", flags: []flagSpec{
		confirmationFlag(),
		undisclosedFlag("machine-id", "Name this host's machine-id; required with --yes"),
		undisclosedFlag("product-uuid", "Name this host's DMI product UUID; required with --yes"),
		boolFlag("recorded-host-retired", "Affirm the recorded host no longer runs this store; required with --yes"),
		undisclosedFlag("root-uuid", "Name this host's root filesystem UUID; required with --yes"),
	}, long: "Rewrite the host identity the controller record holds to this host's, once, when this host keeps the same state store under a changed machine-id, DMI product UUID or root filesystem UUID. It refuses a store whose directories are not the ones its registry attributed and an identity that did not change, and keeps every binding and reservation. A byte-for-byte clone, such as a disk image, snapshot or cloned VM, keeps those directories, so it cannot tell a clone from the store: the prompt names the recorded and the new identity and asks you to affirm that the recorded host no longer runs this store; --yes rebinds only with --recorded-host-retired and this host's three values named, which no output repeats."})
}

type ControllerService interface {
	Check(context.Context, prerequisites.CheckRequest) (*prerequisites.Report, error)
	Setup(context.Context, prerequisites.SetupRequest) (*prerequisites.Report, error)
	Rebind(context.Context, prerequisites.RebindRequest) (*prerequisites.RebindResult, error)
}

func (s Services) invokeController(ctx context.Context, path string, values *requestValues, args []string) (commandResult, error) {
	if s.Controller == nil {
		return commandResult{}, errMissingService
	}
	var result commandResult
	var err error
	switch path {
	case "preflight controller":
		result.controller, err = invokeResult(ctx, values, prerequisites.CheckRequest{ContextName: values.text("context")}, s.Controller.Check)
	case "setup":
		result.controller, err = invokeResult(ctx, values, prerequisites.SetupRequest{
			DryRun:           values.boolean("dry-run"),
			SkipConfirmation: values.boolean("yes"),
			WheelMirror:      values.text("wheel-mirror"),
			DownloadOnly:     values.boolean("download-only"),
			Destination:      values.text("dest-dir"),
		}, s.Controller.Setup)
	case "controller rebind":
		result.rebind, err = invokeResult(ctx, values, prerequisites.RebindRequest{
			SkipConfirmation: values.boolean("yes"),
			MachineID:        values.text("machine-id"),
			ProductUUID:      values.text("product-uuid"),
			FilesystemUUID:   values.text("root-uuid"),
			RecordedRetired:  values.boolean("recorded-host-retired"),
		}, s.Controller.Rebind)
	default:
		return commandResult{}, errors.New("command has no application dispatch")
	}
	return result, err
}
