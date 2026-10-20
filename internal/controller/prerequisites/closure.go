package prerequisites

import "github.com/crmarques/bootwright/internal/controller"

// StageNative is what one context's controller stage selects natively: the
// libvirt client, the hypervisor closure and the installer-media tooling. The
// stage and preflight both read a context's closures through it, so they can
// never answer from two different selections.
type StageNative struct {
	IPUtils        bool
	NMState        bool
	LibvirtClient  bool
	Hypervisor     bool
	InstallerMedia bool
}

func StageNativeOf(selection controller.Selection) StageNative {
	return StageNative{LibvirtClient: selection.LibvirtClient(), Hypervisor: selection.Hypervisor(), InstallerMedia: selection.InstallerMedia(), NMState: selection.NMState(), IPUtils: selection.IPUtils()}
}

// StageRequirements is what the stage asks the host's dnf for and preflight
// reads.
func StageRequirements(n StageNative) []string {
	return StageHostRequirements(NativeRequirements{LibvirtClient: n.LibvirtClient, Hypervisor: n.Hypervisor, InstallerMedia: n.InstallerMedia, NMState: n.NMState, IPUtils: n.IPUtils})
}

// closureTable names each closure a stage may select, in report order.
var closureTable = []struct {
	id       string
	selected func(StageNative) bool
}{
	{"libvirt-client", func(n StageNative) bool { return n.LibvirtClient }},
	{"hypervisor", func(n StageNative) bool { return n.Hypervisor }},
	{"installer-media", func(n StageNative) bool { return n.InstallerMedia }},
	{"nmstate", func(n StageNative) bool { return n.NMState }},
	{"iputils", func(n StageNative) bool { return n.IPUtils }},
}
