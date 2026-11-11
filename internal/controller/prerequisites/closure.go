package prerequisites

import "github.com/crmarques/bootwright/internal/controller"

// StageNative is what one context's controller stage selects natively: the
// container runtime and the host package closures. The
// stage and preflight both read a context's closures through it, so they can
// never answer from two different selections.
type StageNative struct {
	ContainerRuntime bool
	IPUtils          bool
	NMState          bool
	LibvirtClient    bool
	Hypervisor       bool
	InstallerMedia   bool
	ImageMirror      bool
}

func StageNativeOf(selection controller.Selection) StageNative {
	return StageNative{ContainerRuntime: selection.ContainerRuntime(), LibvirtClient: selection.LibvirtClient(), Hypervisor: selection.Hypervisor(), InstallerMedia: selection.InstallerMedia(), NMState: selection.NMState(), IPUtils: selection.IPUtils(), ImageMirror: selection.ImageMirror()}
}

// StageRequirements is what the stage asks the host's dnf for and preflight
// reads.
func StageRequirements(n StageNative) []string {
	return StageHostRequirements(NativeRequirements{ContainerRuntime: n.ContainerRuntime, LibvirtClient: n.LibvirtClient, Hypervisor: n.Hypervisor, InstallerMedia: n.InstallerMedia, NMState: n.NMState, IPUtils: n.IPUtils, ImageMirror: n.ImageMirror})
}

// closureTable names each closure a stage may select, in report order.
var closureTable = []struct {
	id       string
	selected func(StageNative) bool
}{
	{"container-runtime", func(n StageNative) bool { return n.ContainerRuntime }},
	{"libvirt-client", func(n StageNative) bool { return n.LibvirtClient }},
	{"hypervisor", func(n StageNative) bool { return n.Hypervisor }},
	{"installer-media", func(n StageNative) bool { return n.InstallerMedia }},
	{"image-mirror", func(n StageNative) bool { return n.ImageMirror }},
	{"nmstate", func(n StageNative) bool { return n.NMState }},
	{"iputils", func(n StageNative) bool { return n.IPUtils }},
}
