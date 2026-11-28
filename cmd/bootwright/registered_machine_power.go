package main

import (
	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/machine/power"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/substrate"
	"github.com/crmarques/bootwright/internal/substrate/baremetal"
	"github.com/crmarques/bootwright/internal/substrate/kubevirt"
	"github.com/crmarques/bootwright/internal/substrate/libvirt"
	"github.com/crmarques/bootwright/internal/substrate/vsphere"
)

// registeredMachineRequest binds a power transition to the original realized
// machine and its placement, even if current desired state names another host.
func registeredMachineRequest(proof lifecycle.BlockEvidence, verb power.Verb) (power.Request, machine.HardwareIdentity, error) {
	var request power.Request
	var pin machine.HardwareIdentity
	invalid := power.UnprovedRegisteredMachine()
	block := proof.Block
	if proof.Verb != reconciliation.Apply || proof.State != reconciliation.BlockDone || proof.Operation == "" ||
		proof.Kind != string(api.Machine) || block.Kind != proof.Kind || block.Object != proof.Object ||
		proof.Implementation != block.Implementation || (verb != power.Start && verb != power.Stop) {
		return request, pin, invalid
	}
	var (
		startContext, object, blockID string
		controller                    substrate.Controller
	)
	switch proof.Implementation {
	case baremetal.Implementation:
		frozen, err := baremetal.DecodeRequest(block.Request)
		if err != nil {
			return request, pin, err
		}
		if err = baremetal.ValidatePresence(proof.Evidence, frozen, block.RequestDigest); err != nil {
			return request, pin, err
		}
		pin, err = baremetal.ProvedIdentity(proof.Evidence)
		if err != nil {
			return request, pin, err
		}
		request.Arm, request.Placement = substrate.ArmBaremetal, frozen.Placement
		startContext, object, blockID = frozen.Identity.Context, frozen.Identity.Object, frozen.Identity.Block
		controller = substrate.Controller{Endpoint: frozen.Controller.Endpoint, CredentialsRef: frozen.Controller.CredentialsRef,
			TrustBundleRef: frozen.Controller.TrustBundleRef, TLSVerify: frozen.Controller.TLSVerify, TLSVerification: frozen.Controller.TLSVerification}
	case libvirt.MachineImplementation:
		frozen, err := libvirt.DecodeMachineRequest(block.Request)
		if err != nil {
			return request, pin, err
		}
		if err = libvirt.ValidateMachinePresence(proof.Evidence, frozen, block.RequestDigest); err != nil {
			return request, pin, err
		}
		request.Arm, request.Placement = substrate.ArmLibvirt, frozen.Placement
		startContext, object, blockID = frozen.Identity.Context, frozen.Identity.Object, frozen.Identity.Block
		controller = substrate.Controller{Endpoint: frozen.Controller.Endpoint, CredentialsRef: frozen.Controller.CredentialsRef,
			TrustBundleRef: frozen.Controller.CertificateRef, TLSVerify: true}
		pin = machine.HardwareIdentity{UUID: frozen.UUID}
	case vsphere.Implementation:
		frozen, err := vsphere.DecodeRequest(block.Request)
		if err != nil {
			return request, pin, err
		}
		var found bool
		pin, found, err = vsphere.PinnedIdentity(proof.Object, []lifecycle.BlockEvidence{proof})
		if err != nil {
			return request, pin, err
		}
		if !found {
			return request, pin, invalid
		}
		request.Arm, request.Placement = substrate.ArmVSphere, frozen.Placement
		startContext, object, blockID = frozen.Identity.Context, frozen.Identity.Object, frozen.Identity.Block
		controller = frozen.Controller()
	case kubevirt.MachineImplementation:
		frozen, err := kubevirt.DecodeMachineRequest(block.Request)
		if err != nil {
			return request, pin, err
		}
		if frozen.HostCluster != "" || frozen.KubeconfigRef == "" {
			return request, pin, invalid
		}
		if err = kubevirt.ValidateMachinePresence(proof.Evidence, frozen, block.RequestDigest); err != nil {
			return request, pin, err
		}
		var found bool
		pin, found, err = kubevirt.PinnedIdentity(proof.Object, []lifecycle.BlockEvidence{proof})
		if err != nil {
			return request, pin, err
		}
		if !found {
			return request, pin, invalid
		}
		request.Arm, request.Placement = substrate.ArmKubeVirt, frozen.Placement
		startContext, object, blockID = frozen.Identity.Context, frozen.Identity.Object, frozen.Identity.Block
		controller = substrate.Controller{Endpoint: substrate.KubeVirtEndpoint(frozen.Namespace, frozen.Name), CredentialsRef: frozen.KubeconfigRef,
			MediaVolume: frozen.MediaVolume, TLSVerify: true}
	default:
		return request, pin, invalid
	}
	contextName, valid := api.ParseContextName(startContext)
	if !valid || object != block.Object || blockID != block.ID || !pin.Present() {
		return power.Request{}, machine.HardwareIdentity{}, invalid
	}
	request.Controller = power.Access{Endpoint: controller.Endpoint, CredentialsRef: controller.CredentialsRef,
		TLSVerification: controller.TLSVerification, TLSVerify: controller.TLSVerify, TrustBundleRef: controller.TrustBundleRef}
	request.MachinePort = substrate.NewMachinePort(request.Arm, controller, "")
	request.Identity = power.Identity{Context: contextName, Object: object}
	request.Version, request.Verb = power.RequestVersion, verb
	return request, pin, nil
}
