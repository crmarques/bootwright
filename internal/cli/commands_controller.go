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
	return available(commandSpec{path: "setup", short: "Set up context-independent controller prerequisites", flags: []flagSpec{dryRunFlag(), confirmationFlag(), outputFlag(), directoryFlag("dest-dir", "Write the locked wheels, the pip wheel and lock.json into this directory (requires --download-only)"), boolFlag("download-only", "Write the locked files for an offline host and change nothing else; never elevates"), stringFlag("wheel-mirror", "Read each locked wheel as <mirror>/<filename>: file:///<directory> or https://<host>/<base>; its bytes must still match the lock")}, long: "Prepare this host's context-independent controller prerequisites: the baseline host packages, installed through the host's own dnf, and the controller runtime, a venv on the OS Python holding exactly the wheels this build locks. Setup selects no context; the prerequisites a context adds are installed by the controller stage of apply. A ready host makes no request and no change. Otherwise the plan shows dnf's own preview before confirmation. --dry-run never elevates: it names the host package requirements without a preview and makes no changes, and --output json, accepted only with --dry-run, also lists every locked wheel's file, URL and SHA-256. --wheel-mirror reads each locked wheel as <mirror>/<filename> from a file:/// directory or an HTTPS base, as untrusted transport: the lock's SHA-256 alone decides whether the bytes are used. --download-only --dest-dir DIR never elevates and changes only DIR: on any connected host it writes the locked wheels of every Python ABI, the pip wheel and a copy of lock.json, so DIR serves an offline host as a --wheel-mirror. Review the plan before confirming; --yes skips ordinary confirmation."})
}

type ControllerService interface {
	Check(context.Context, prerequisites.CheckRequest) (*prerequisites.Report, error)
	Setup(context.Context, prerequisites.SetupRequest) (*prerequisites.Report, error)
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
	default:
		return commandResult{}, errors.New("command has no application dispatch")
	}
	return result, err
}
