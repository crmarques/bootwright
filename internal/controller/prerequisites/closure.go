package prerequisites

import (
	"context"
	"slices"
	"strings"

	"github.com/crmarques/bootwright/internal/controller"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

// StageNative is what one context's controller stage selects natively: the
// libvirt client, the hypervisor closure and the installer-media tooling, with
// the libvirt intent the client and the hypervisor share. The stage and
// preflight both read a context's closures through it, so they can never
// answer from two different selections.
type StageNative struct {
	LibvirtClient  bool
	Hypervisor     bool
	InstallerMedia bool
	Libvirt        string
}

// StageNativeOf is the selection exactly as the controller stage freezes it:
// the declared libvirt intent when the client or the hypervisor is selected,
// and the compiled default otherwise, because nothing else installs a release
// of it.
func StageNativeOf(selection controller.Selection) StageNative {
	native := StageNative{
		LibvirtClient: selection.LibvirtClient(), Hypervisor: selection.Hypervisor(), InstallerMedia: selection.InstallerMedia(),
		Libvirt: controller.DefaultDependencyVersions().Libvirt,
	}
	if native.LibvirtClient || native.Hypervisor {
		native.Libvirt = selection.Versions().Libvirt
	}
	return native
}

// selects reports whether this selection names any native closure.
func (n StageNative) selects() bool { return n.LibvirtClient || n.Hypervisor || n.InstallerMedia }

// solved is the part of this selection a native transaction installs on the
// platform. A RHEL controller's installer-media tooling is the operator's own,
// proved by presence, so no transaction ever solves it there.
func (n StageNative) solved(platform Platform) StageNative {
	if OperatorInstallerMedia(platform) {
		n.InstallerMedia = false
	}
	return n
}

// StageTransaction is the native transaction a stage of this selection solves
// on the platform, its closures beside the container runtime every stage
// resolution carries, and whether it solves anything at all: the libvirt
// client only when the selection names it, never because another closure is
// selected beside it, and never a RHEL controller's installer-media tooling.
func StageTransaction(platform Platform, want StageNative) (NativeRequirements, bool) {
	solved := want.solved(platform)
	return NativeRequirements{ContainerRuntime: true, LibvirtClient: solved.LibvirtClient, Hypervisor: solved.Hypervisor, InstallerMedia: solved.InstallerMedia}, solved.selects()
}

// OperatorInstallerMedia reports a platform whose installer-media tooling the
// operator installs from the host's own vendor-signed repositories (D106): no
// public source this executable resolves from carries lorax or xorriso there.
func OperatorInstallerMedia(platform Platform) bool { return platform.OS == "rhel" }

// installerMediaRoots is the root key of the installer-media tooling, whose
// package names the solver's table owns.
const installerMediaRoots = "installer-media"

// installerMediaStep is the operator's step for a RHEL controller's
// installer-media tooling, as a correction and as the command a report offers
// before the stage. Installing a name already installed changes nothing, so a
// package another key signed is removed first and installed again from the
// host's own repositories, which also installs a missing one.
func installerMediaStep(foreign []string) (correction, invocation string) {
	invocation = "dnf install lorax xorriso"
	if len(foreign) == 0 {
		return "Install lorax and xorriso from this host's enabled Red Hat repositories (" + invocation + ")", invocation
	}
	invocation = "dnf remove " + strings.Join(foreign, " ") + " && " + invocation
	return "Reinstall " + strings.Join(foreign, " and ") + " from this host's enabled Red Hat repositories (" + invocation + ")", invocation
}

// closureTable names each closure a stage may select, in report order: its
// check identity, the root key its packages carry and whether a selection
// names it.
var closureTable = []struct {
	id, key  string
	selected func(StageNative) bool
}{
	{"libvirt-client", "libvirt", func(n StageNative) bool { return n.LibvirtClient }},
	{"hypervisor", "hypervisor", func(n StageNative) bool { return n.Hypervisor }},
	{"installer-media", installerMediaRoots, func(n StageNative) bool { return n.InstallerMedia }},
}

// OperatorPresence is what of the named root packages the operator installed
// on a RHEL controller, read from the host's own package database: each
// installed instance's identity and its signer, never its files (D106).
// Missing names a package with no installed instance, and Foreign one with an
// instance no signature of the qualified vendor key covers. It is evidence for
// one inspection and is never persisted.
type OperatorPresence struct {
	Ready     bool
	Installed []NativeRootPresence
	Missing   []string
	Foreign   []string
}

// ClosurePresence is one selected native closure on this host, as the
// controller stage and preflight both read it. Installed names each of its
// root packages the host carries; Missing and Foreign are set only for a
// closure the operator provides.
type ClosurePresence struct {
	ID        string
	Ready     bool
	Installed []NativeRootPresence
	Missing   []string
	Foreign   []string
}

// StageAdmission refuses, before anything is read or resolved, a selection
// this platform cannot realize: on RHEL no approved source carries the libvirt
// client or the hypervisor closure until an entitled-source adapter exists
// (B330). It is pure.
func StageAdmission(platform Platform, want StageNative) error {
	if platform.OS == "rhel" && (want.LibvirtClient || want.Hypervisor) {
		return diagnostics.NewFailureWithRemediation("controller.unsupported", "RHEL libvirt requires an authenticated AppStream source adapter", "",
			"Use a Fedora controller for libvirt preparation until RHEL AppStream credential acquisition is configured.")
	}
	return nil
}

// LatestStageResolution is the one retained resolution a stage of this
// selection reads: the newest that is not setup's own, solved on the same
// platform for exactly the closures a transaction solves there, under the same
// libvirt intent. Another selection's resolution, or an older one of the same
// selection, is never read.
func LatestStageResolution(retained []Definition, platform Platform, want StageNative) *Definition {
	for index := len(retained) - 1; index >= 0; index-- {
		if stageResolutionOf(retained[index], platform, want) {
			definition := CloneDefinition(retained[index])
			return &definition
		}
	}
	return nil
}

// SupersededStageResolutions names every retained resolution of this
// selection but the one a stage now retains. Once that one is retained it is
// the latest, so no stage reads the others again, and a stage that solves
// again whenever a root is missing would otherwise fill the host's bound of
// retained resolutions. Setup's own and another selection's are not named.
func SupersededStageResolutions(retained []Definition, platform Platform, want StageNative, kept Definition) []string {
	var superseded []string
	for _, value := range retained {
		if stageResolutionOf(value, platform, want) && value.ResolutionDigest != kept.ResolutionDigest {
			superseded = append(superseded, value.ResolutionDigest)
		}
	}
	return superseded
}

func stageResolutionOf(value Definition, platform Platform, want StageNative) bool {
	solved := want.solved(platform)
	requirements := value.NativeRequirements
	return solved.selects() && value.Native != nil && !setupResolution(value) && value.Platform == platform &&
		requirements.LibvirtClient == solved.LibvirtClient && requirements.Hypervisor == solved.Hypervisor &&
		requirements.InstallerMedia == solved.InstallerMedia && value.Versions.Libvirt == solved.Libvirt
}

// StageClosures answers, for each closure the selection names, whether this
// host carries it: from the latest resolution of the selection, whose absence
// is a definite absence because nothing installed those roots, and on RHEL the
// installer-media tooling from the operator's own installation. The
// controller stage and preflight both ask it, so they share one answer.
func StageClosures(ctx context.Context, inspector NativeInspector, retained []Definition, platform Platform, want StageNative) ([]ClosurePresence, error) {
	return ResolutionClosures(ctx, inspector, LatestStageResolution(retained, platform, want), platform, want)
}

// ResolutionClosures is that answer over one named resolution, or none, such
// as the transaction a stage has just run. Without an inspector nothing can be
// observed, so every closure reads absent.
func ResolutionClosures(ctx context.Context, inspector NativeInspector, resolution *Definition, platform Platform, want StageNative) ([]ClosurePresence, error) {
	closures := []ClosurePresence{}
	if !want.selects() {
		return closures, nil
	}
	operator := OperatorPresence{}
	if want.InstallerMedia && OperatorInstallerMedia(platform) && inspector != nil {
		var err error
		if operator, err = inspector.OperatorRoots(ctx, platform, NativeRootNames()[installerMediaRoots]); err != nil {
			return nil, err
		}
	}
	presence := NativePresence{}
	if resolution != nil && resolution.Native != nil && inspector != nil {
		var err error
		if presence, err = inspector.Check(ctx, *resolution.Native); err != nil {
			return nil, err
		}
	}
	for _, closure := range closureTable {
		if !closure.selected(want) {
			continue
		}
		if closure.key == installerMediaRoots && OperatorInstallerMedia(platform) {
			closures = append(closures, ClosurePresence{
				ID: closure.id, Ready: operator.Ready && len(operator.Missing) == 0 && len(operator.Foreign) == 0,
				Installed: slices.Clone(operator.Installed), Missing: slices.Clone(operator.Missing), Foreign: slices.Clone(operator.Foreign),
			})
			continue
		}
		value := ClosurePresence{ID: closure.id, Installed: []NativeRootPresence{}}
		if resolution != nil && resolution.Native != nil {
			expected := 0
			for _, root := range resolution.Native.Roots {
				if root.Key == closure.key {
					expected++
				}
			}
			for _, root := range presence.Installed {
				if root.Key == closure.key {
					value.Installed = append(value.Installed, root)
				}
			}
			value.Ready = expected != 0 && len(value.Installed) == expected
		}
		closures = append(closures, value)
	}
	return closures, nil
}

// ClosuresReady reports whether every selected closure is present.
func ClosuresReady(closures []ClosurePresence) bool {
	for _, closure := range closures {
		if !closure.Ready {
			return false
		}
	}
	return true
}

// ClosureRoots is every root package the selected closures carry, which is
// what a stage's evidence names.
func ClosureRoots(closures []ClosurePresence) []NativeRootPresence {
	roots := []NativeRootPresence{}
	for _, closure := range closures {
		roots = append(roots, closure.Installed...)
	}
	return roots
}

// OperatorRefusal refuses a stage on a platform whose installer-media tooling
// the operator provides while that tooling is not present, before any
// publisher is contacted, naming what is missing or carries another signer.
func OperatorRefusal(platform Platform, closures []ClosurePresence) error {
	if !OperatorInstallerMedia(platform) {
		return nil
	}
	for _, closure := range closures {
		if closure.ID == "installer-media" && !closure.Ready {
			return InstallerMediaRefusal(closure.Missing, closure.Foreign)
		}
	}
	return nil
}

// InstallerMediaRefusal is the one refusal of a RHEL controller's
// installer-media tooling. Its correction is the operator's step, a
// reinstallation when another key signed a package, and the scope that meets
// it names the command that follows. With nothing missing or
// foreign it states the rule alone, as a resolver asked to solve the tooling
// there does.
func InstallerMediaRefusal(missing, foreign []string) error {
	var found []string
	if len(missing) != 0 {
		found = append(found, packagePhrase(missing, "is not installed", "are not installed"))
	}
	if len(foreign) != 0 {
		found = append(found, packagePhrase(foreign, "is not signed by the Red Hat release key this executable qualifies", "are not signed by the Red Hat release key this executable qualifies"))
	}
	if len(found) == 0 {
		found = []string{"no public source this executable resolves from carries them"}
	}
	correction, _ := installerMediaStep(foreign)
	return &ScopedFailure{
		Code:       "controller.unsupported",
		Message:    "a RHEL controller prepares Anaconda media with lorax and xorriso installed from this host's own Red Hat repositories; " + strings.Join(found, "; "),
		Correction: correction,
	}
}

func packagePhrase(names []string, one, several string) string {
	if len(names) == 1 {
		return names[0] + " " + one
	}
	return strings.Join(names, " and ") + " " + several
}
