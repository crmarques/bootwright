package substrate

type MachinePort struct {
	CAVariable           string `json:"caVariable,omitempty"`
	Endpoint             string `json:"endpoint"`
	MediaVolume          string `json:"mediaVolume,omitempty"`
	PasswordVariable     string `json:"passwordVariable,omitempty"`
	PinnedSerialVariable string `json:"pinnedSerialVariable,omitempty"`
	PinnedUUIDVariable   string `json:"pinnedUUIDVariable,omitempty"`
	UserVariable         string `json:"userVariable"`
	Verification         string `json:"verification,omitempty"`
	Verify               bool   `json:"verify"`
}

func (t Target) MachinePort(suffix string) MachinePort {
	return NewMachinePort(t.Port, t.Controller, suffix)
}

func NewMachinePort(arm string, controller Controller, suffix string) MachinePort {
	port := MachinePort{
		Endpoint: controller.Endpoint, Verify: controller.TLSVerify, Verification: controller.TLSVerification,
		MediaVolume:  controller.MediaVolume,
		UserVariable: "controllerUser" + suffix, PinnedUUIDVariable: "pinnedUUIDBase64" + suffix, PinnedSerialVariable: "pinnedSerialBase64" + suffix,
	}
	if _, password := ControllerCredential(arm); password != "" {
		port.PasswordVariable = "controllerPassword" + suffix
	}
	if controller.TrustBundleRef != "" {
		port.CAVariable = "controllerCA" + suffix
	}
	return port
}

func KubeVirtMediaVolume(name string) string { return name + "-media" }
