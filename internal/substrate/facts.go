package substrate

import (
	"slices"
	"strconv"
	"time"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/secrets"
)

type facts struct {
	delivery         string
	accessCredential bool
	bridgedGuests    bool
	tools            []string
	credential       [2]secrets.Part
	portPinned       bool
}

var account = [2]secrets.Part{secrets.UsernamePart, secrets.PasswordPart}

var armFacts = map[string]facts{
	ArmBaremetal: {delivery: DeliveryFetch, accessCredential: true, credential: account},
	ArmLibvirt:   {delivery: DeliveryFetch, bridgedGuests: true, credential: account},
	ArmVSphere:   {delivery: DeliveryFetch, tools: []string{"govc"}, credential: account},
	ArmKubeVirt:  {delivery: DeliveryPush, accessCredential: true, tools: []string{"virtctl"}, credential: [2]secrets.Part{secrets.ValuePart}, portPinned: true},
}

func PortPinned(arm string) bool { return armFacts[arm].portPinned }

func ControllerCredential(arm string) (user, password secrets.Part) {
	credential, ok := armFacts[arm]
	if !ok {
		return secrets.UsernamePart, secrets.PasswordPart
	}
	return credential.credential[0], credential.credential[1]
}

func DeliveryOf(arm string) string {
	if fact, ok := armFacts[arm]; ok {
		return fact.delivery
	}
	return DeliveryFetch
}

func ControllerCredentialIsAccess(arm string) bool { return armFacts[arm].accessCredential }

func BridgedGuests(arm string) bool { return armFacts[arm].bridgedGuests }

func ProviderTools(provider api.Object) []string { return armFacts[Variant(provider)].tools }

func IsRealized(provider api.Object) bool {
	return slices.Contains(Realized(), Variant(provider))
}

func HostMachine(provider api.Object) string {
	return provider.Spec().Get(ArmLibvirt, "machineRef").Text()
}

func NeedsHypervisorClient(provider api.Object) bool { return provider.Spec().Has(ArmLibvirt) }

func NamesHostCluster(provider api.Object) bool { return HostCluster(provider) != "" }

func HostCluster(provider api.Object) string {
	if Variant(provider) != ArmKubeVirt {
		return ""
	}
	return provider.Spec().Get(ArmKubeVirt, "hostClusterRef").Text()
}

type ManagedNetwork struct{ Prefix, Bridge string }

func ManagedAttachment(attachment api.Value) (ManagedNetwork, bool) {
	arm := attachment.Get(ArmLibvirt)
	if arm.Get("management").Text() != "managed" {
		return ManagedNetwork{}, false
	}
	return ManagedNetwork{Prefix: arm.Get("address").Text(), Bridge: arm.Get("bridge").Text()}, true
}

func AttachmentMTU(attachment api.Value) (int64, bool) {
	return attachment.Get(ArmLibvirt, "mtu").Int64()
}

type AddressClaim struct{ Path, Address string }

func AddressClaims(provider api.Object) []AddressClaim {
	var claims []AddressClaim
	for index, entry := range provider.Spec().Get("networkAttachments").Items() {
		if network, managed := ManagedAttachment(entry); managed {
			claims = append(claims, AddressClaim{Path: "spec.networkAttachments[" + strconv.Itoa(index) + "]." + ArmLibvirt + ".address", Address: network.Prefix})
		}
	}
	return append(claims, AddressClaim{Path: "spec." + ArmLibvirt + ".bmcEmulation.bindAddress",
		Address: provider.Spec().Get(ArmLibvirt, "bmcEmulation", "bindAddress").Text()})
}

func SelectedProfile(catalog api.Catalog, machine api.Object) (api.Object, api.Value, bool) {
	provider, found := catalog.Find(api.InfraProvider, machine.Spec().Get("substrate", "providerRef").Text())
	arm := Variant(provider)
	if !found || arm == "" {
		return provider, api.Value{}, false
	}
	for _, profile := range provider.Spec().Get(arm, "machineProfiles").Items() {
		if profile.Get("name").Text() == MachineProfileRef(machine) {
			return provider, profile, true
		}
	}
	return provider, api.Value{}, false
}

func ClusterBounds(arms ...string) (fanout int, allowance, margin time.Duration) {
	if len(arms) == 0 {
		arms = []string{ArmLibvirt}
	}
	fanout = 16
	for _, arm := range arms {
		bounds, ok := MachineBoundsFor(arm)
		if !ok {
			continue
		}
		fanout = min(fanout, bounds.Fanout)
		allowance = max(allowance, bounds.BootAllowance)
		margin = max(margin, bounds.NodeMargin)
	}
	return fanout, allowance, margin
}

func MachineArm(machine api.Object) string {
	for _, arm := range Arms() {
		if machine.Spec().Has("substrate", arm) {
			return arm
		}
	}
	return ""
}

func MachineProfileRef(machine api.Object) string {
	return machine.Spec().Get("substrate", MachineArm(machine), "profileRef").Text()
}

func MachineProfilePath(machine api.Object) string {
	arm := MachineArm(machine)
	if arm == "" {
		return "spec.substrate"
	}
	return "spec.substrate." + arm + ".profileRef"
}

const BMCPath = "spec.substrate." + ArmBaremetal + ".bmc"

func MachineBMC(machine api.Object) api.Value {
	return machine.Spec().Get("substrate", ArmBaremetal, "bmc")
}

func MachineMACs(machine api.Object) api.Value {
	return machine.Spec().Get("substrate", MachineArm(machine), "nics")
}

func MachineMAC(machine api.Object, nic string) string {
	for _, entry := range MachineMACs(machine).Items() {
		if entry.Get("nicRef").Text() == nic {
			return entry.Get("macAddress").Text()
		}
	}
	return ""
}

func MachineInterfaceAttachments(machine api.Object) api.Value {
	return machine.Spec().Get("substrate", MachineArm(machine), "interfaceAttachments")
}
