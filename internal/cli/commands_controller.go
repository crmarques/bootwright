package cli

import (
	"context"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/spf13/cobra"
)

func controllerSetupCommand() commandSpec {
	return available(commandSpec{
		path: "install", results: []jsonResult{controllerPresentation{}, setupPlanResult{}, installCheckResult{}},
		render: renderController(), controllerReport: true, validate: validateInstall, classify: classifyRules{ambient: installAmbient},
		example: "--dry-run", contextFree: true, invoke: Services.invokeControllerInstall,
		short: "Install context-independent controller prerequisites",
		flags: []flagSpec{
			boolFlag("dry-run", "Show what install would change without running dnf"),
			boolFlag("check", "Verify the existing installation and exit 1 if it is not ready"),
			confirmationFlag(),
		},
		long: "Prepare this host's baseline packages and controller Python runtime from the locked wheels carried by this build. This command selects no context. Use --dry-run to show the planned changes, with the locked wheel inventory in JSON or YAML. Use --check to verify host readiness, including the packages, runtime, setup receipt and state root. Both flags use local reads only. A ready host needs no setup. When installation is needed, install shows dnf's own preview before confirmation. --yes skips ordinary confirmation during execution.",
	})
}

func validateInstall(command *cobra.Command) string {
	flags := command.Flags()
	if dryRunSelected(flags) && boolValue(flags, "check") {
		return "--dry-run conflicts with --check"
	}
	return ""
}

func controllerRebindCommand() commandSpec {
	return available(commandSpec{path: "rebind-host", results: []jsonResult{rebindPresentation{}}, render: renderRebind(), example: "", contextFree: true, invoke: Services.invokeControllerRebind, short: "Rebind the controller record to this host's changed identity", flags: []flagSpec{
		confirmationFlag(),
		undisclosedFlag("machine-id", "Name this host's machine-id; required with --yes"),
		undisclosedFlag("product-uuid", "Name this host's DMI product UUID; required with --yes"),
		boolFlag("recorded-host-retired", "Affirm the recorded host no longer runs this store; required with --yes"),
		undisclosedFlag("root-uuid", "Name this host's root filesystem UUID; required with --yes"),
	}, long: "Rewrite the host identity the controller record holds to this host's, once, when this host keeps the same state store under a changed machine-id, DMI product UUID or root filesystem UUID. It refuses a store whose directories are not the ones its registry attributed and an identity that did not change, and keeps every binding and reservation. A byte-for-byte clone, such as a disk image, snapshot or cloned VM, keeps those directories, so it cannot tell a clone from the store: the prompt names the recorded and the new identity and asks you to affirm that the recorded host no longer runs this store; --yes rebinds only with --recorded-host-retired and this host's three values named, which no output repeats."})
}

func controllerUninstallCommand() commandSpec {
	return available(commandSpec{path: "uninstall", results: []jsonResult{uninstallPresentation{}}, render: renderUninstall(), validate: validateUninstall, classify: classifyRules{ambient: uninstallAmbient}, example: "", contextFree: true, invoke: Services.invokeControllerUninstall, short: "Remove the Bootwright host installation", flags: []flagSpec{
		dryRunFlag(),
		confirmationFlag(),
		boolFlag("discard-unreadable", "Discard what this build cannot read or attribute: a store another build wrote, or guest disks no removal took back; never removes packages"),
		boolFlag("keep-packages", "Leave every host package installed and remove only Bootwright's state"),
	}, long: "Remove, on a host that holds no context and no installer media, the host packages install recorded as its own, through the host's own dnf, then every root Bootwright keeps on this host, whole: the store /var/lib/bootwright, the staging directory /var/lib/bootwright-staging, the log root /var/log/bootwright, the hosted root /var/lib/bootwright-hosted with any physical-erase receipts, and the libvirt image prefix /var/lib/libvirt/images/bootwright when it is empty. It never removes a package present before Bootwright, a package a controller stage installed, this executable, or what a context realized on an SSH placement host. It refuses before any effect while a context or an image remains, naming the commands that remove each, while any libvirt domain, network or pool, systemd unit, quadlet source or podman container Bootwright owns remains, naming each, while another command holds the store, while guest disks remain under the libvirt image prefix, and, without --keep-packages, when the install record cannot say what install added or another installed package requires one of them. --discard-unreadable is the exit for a store this build cannot read, such as one an earlier build wrote, and for guest disks no removal took back: after the same proof that no owned object remains, it removes every root and the libvirt image prefix whole, and never a package. --dry-run checks ownership and package dependencies and lists what would be removed and kept. Execution shows dnf's own removal preview before confirmation. A host with nothing to remove prints Nothing to do. Review the plan before confirming; --yes skips ordinary confirmation."})
}

type ControllerService interface {
	CheckHost(context.Context) (*prerequisites.Report, error)
	Setup(context.Context, prerequisites.SetupRequest) (*prerequisites.Report, error)
	Rebind(context.Context, prerequisites.RebindRequest) (*prerequisites.RebindResult, error)
	Uninstall(context.Context, prerequisites.UninstallRequest) (*prerequisites.UninstallResult, error)
}

func (s Services) invokeControllerInstall(ctx context.Context, values *requestValues, _ []string) (commandResult, error) {
	if s.Controller == nil {
		return commandResult{}, errMissingService
	}
	if values.boolean("check") {
		result, err := s.Controller.CheckHost(ctx)
		return commandResult{controller: result}, err
	}
	result, err := invokeResult(ctx, values, prerequisites.SetupRequest{
		DryRun:           values.boolean("dry-run"),
		SkipConfirmation: values.boolean("yes"),
	}, s.Controller.Setup)
	return commandResult{controller: result}, err
}

func (s Services) invokeControllerRebind(ctx context.Context, values *requestValues, _ []string) (commandResult, error) {
	if s.Controller == nil {
		return commandResult{}, errMissingService
	}
	result, err := invokeResult(ctx, values, prerequisites.RebindRequest{
		SkipConfirmation: values.boolean("yes"),
		MachineID:        values.text("machine-id"),
		ProductUUID:      values.text("product-uuid"),
		FilesystemUUID:   values.text("root-uuid"),
		RecordedRetired:  values.boolean("recorded-host-retired"),
	}, s.Controller.Rebind)
	return commandResult{rebind: result}, err
}

func (s Services) invokeControllerUninstall(ctx context.Context, values *requestValues, _ []string) (commandResult, error) {
	if s.Controller == nil {
		return commandResult{}, errMissingService
	}
	result, err := invokeResult(ctx, values, prerequisites.UninstallRequest{
		DryRun:            values.boolean("dry-run"),
		SkipConfirmation:  values.boolean("yes"),
		KeepPackages:      values.boolean("keep-packages"),
		DiscardUnreadable: values.boolean("discard-unreadable"),
	}, s.Controller.Uninstall)
	return commandResult{uninstall: result}, err
}
