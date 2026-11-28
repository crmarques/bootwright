package substrate

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/reconciliation"
)

// VSphereInventory names the inventory objects one Machine is created in.
type VSphereInventory struct {
	Datacenter     string `json:"datacenter"`
	ComputeCluster string `json:"computeCluster"`
	Datastore      string `json:"datastore"`
	ResourcePool   string `json:"resourcePool"`
	Folder         string `json:"folder"`
}

// VSphereMedia identifies the inventory and staging location used by the machine port.
type VSphereMedia struct {
	Datacenter string `json:"datacenter"`
	Datastore  string `json:"datastore"`
	Folder     string `json:"folder"`
	Name       string `json:"name"`
	Owner      string `json:"owner"`
}

func (m VSphereMedia) Volume() string {
	data, _ := json.Marshal(m)
	return string(data)
}

func VSpherePlacement(provider, machine api.Object, catalog api.Catalog) (VSphereInventory, api.Value, bool) {
	_, profile, ok := SelectedProfile(catalog, machine)
	arm := provider.Spec().Get("vsphere")
	p, found := arm.Get("placements").Named("name", profile.Get("placementRef").Text())
	server, connected := arm.Get("vcenters").Named("server", p.Get("server").Text())
	return VSphereInventory{Datacenter: p.Get("datacenter").Text(), ComputeCluster: p.Get("computeCluster").Text(), Datastore: p.Get("datastore").Text(), ResourcePool: p.Get("resourcePool").Text(), Folder: p.Get("folder").Text()}, server, ok && found && connected
}

func VSphereMediaFor(provider api.Object, inventory VSphereInventory, contextName, machine string) VSphereMedia {
	staging := provider.Spec().Get("vsphere", "isoStaging")
	datastore, folder := staging.Get("datastore").Text(), staging.Get("folder").Text()
	if datastore == "" {
		datastore = inventory.Datastore
	}
	if folder == "" {
		folder = "bootwright-vmedia"
	}
	owner := reconciliation.ContentDigestOf("bootwright.substrate.vsphere.owner-v1", contextName, machine)
	return VSphereMedia{Datacenter: inventory.Datacenter, Datastore: datastore, Folder: folder, Name: "bw-" + machine + "-" + owner[:16], Owner: owner}
}

func NICAddress(arm Arm, contextName string, machine api.Object, nic string) string {
	if arm != ArmVSphere {
		return InterfaceMAC(contextName, machine.Name(), nic)
	}
	if mac := MachineMAC(machine, nic); mac != "" {
		return strings.ToLower(mac)
	}
	digest := sha256.Sum256([]byte("bootwright.substrate.vsphere.nic-v1\x00" + contextName + "\x00" + machine.Name() + "\x00" + nic))
	return fmt.Sprintf("00:50:56:%02x:%02x:%02x", digest[0]&0x3f, digest[1], digest[2])
}

func vsphereTarget(catalog api.Catalog, provider, machine api.Object, contextName, controllerMachine string, target Target) (Target, error) {
	inventory, server, found := VSpherePlacement(provider, machine, catalog)
	if !found {
		return Target{}, refusal("api.reference", "the Machine profile has no resolved placement and vCenter", "correct the placementRef and server on "+provider.Identity())
	}
	host, ok := catalog.Find(api.Machine, controllerMachine)
	if !ok {
		return Target{}, refusal("api.reference", "the controller Machine is missing", "correct spec.controller.machineRef")
	}
	_, profile, _ := SelectedProfile(catalog, machine)
	target.TPM2 = profile.Has("tpm")
	target.PlacementMachine = host
	port := server.Get("port").Text()
	if port == "" {
		port = "443"
	}
	target.Controller = Controller{Endpoint: "https://" + bracketed(server.Get("server").Text()) + ":" + port, TLSVerify: true, CredentialsRef: server.Get("credentialsRef").Text(), TrustBundleRef: server.Get("trustBundleRef").Text(), MediaVolume: VSphereMediaFor(provider, inventory, contextName, machine.Name()).Volume()}
	target.Identity = Identity{Channel: ChannelDeliveredKey, HostKeyRef: machine.Spec().Get("os", "install", "hostKeyRef").Text()}
	target.RootDeviceHints = rootDeviceHints(machine)
	for _, nic := range MachineNICs(machine) {
		target.Interfaces = append(target.Interfaces, Interface{Name: nic, MACAddress: NICAddress(ArmVSphere, contextName, machine, nic)})
	}
	if len(target.Interfaces) == 0 {
		return Target{}, refusal("api.required", "the Machine declares no vNIC", "declare "+NICsPath+" on "+machine.Identity())
	}
	return target, nil
}
