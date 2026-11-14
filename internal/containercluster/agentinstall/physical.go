package agentinstall

import (
	"strings"

	"github.com/crmarques/bootwright/internal/substrate"
)

func privateControllerRefusal(machine string, controller Controller) (reason, remedy string) {
	if !strings.HasPrefix(controller.Endpoint, "https://") {
		return "a physical node's private boot image requires an HTTPS management controller",
			"set spec.substrate.baremetal.bmc.address on Machine/" + machine + " to an HTTPS ComputerSystem endpoint"
	}
	if !controller.TLSVerify {
		return "a physical node's private boot image requires a BMC connection that verifies its certificate",
			"enable spec.substrate.baremetal.bmc.tls.verify on Machine/" + machine + " and declare its trust bundle when needed"
	}
	return "", ""
}

func targetPrivateRefusal(machine string, controller substrate.Controller) (reason, remedy string) {
	return privateControllerRefusal(machine, Controller{
		Endpoint: controller.Endpoint, TLSVerify: controller.TLSVerify,
		VirtualMedia: VirtualMedia{Trust: controller.VirtualMedia.Trust},
	})
}
