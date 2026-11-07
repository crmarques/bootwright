package kubevirt

import (
	"crypto/sha256"
	"encoding/hex"
	"slices"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/diagnostics"
	machineref "github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/substrate"
)

const (
	cudnGroup, cudnKind = "k8s.ovn.org", "ClusterUserDefinedNetwork"
	nadGroup, nadKind   = "k8s.cni.cncf.io", "NetworkAttachmentDefinition"
)

// Refusals refuses every selected KubeVirt object this arm cannot realize,
// before registration: a Machine an install profile would install, one whose
// profile declares no size, and an interface on a network kind the arm does
// not attach to.
func Refusals(catalog api.Catalog) []lifecycle.Refusal {
	var found []lifecycle.Refusal
	for _, provider := range providers(catalog) {
		if host, hosted := catalog.Find(api.OpenShiftCluster, substrate.HostCluster(provider)); hosted {
			for _, attachment := range provider.Spec().Get("networkAttachments").Items() {
				if !substrate.HostNetworkNamed(host, attachment) {
					name := attachment.Get("name").Text()
					found = append(found, lifecycle.RefusalOf(provider,
						"a KubeVirt provider hosted by "+host.Identity()+" attaches only to that cluster's secondary networks, whose selectors label the namespace its kubevirt instance owns, and attachment "+name+" names another network",
						"name a ClusterUserDefinedNetwork among spec.networking.secondaryNetworks of "+host.Identity()+" in attachment "+name+" of "+provider.Identity()))
				}
			}
		}
		for _, machine := range substrate.HostedMachines(catalog, provider.Name()) {
			if machine.Spec().Get("os", "installProfileRef").Text() != "" {
				found = append(found, lifecycle.RefusalOf(machine,
					"a KubeVirt Machine is realized for an installer that supplies its operating system, and this one names an install profile",
					"remove spec.os.installProfileRef from "+machine.Identity()+", or host it on a baremetal or libvirt provider"))
			}
			if _, profile, selected := substrate.SelectedProfile(catalog, machine); selected && !sized(profile) {
				name := profile.Get("name").Text()
				found = append(found, lifecycle.RefusalOf(machine,
					"a KubeVirt machine is created with the CPU count, memory and root disk its profile declares, and its profile "+name+" declares one as zero",
					"set cpu, memoryMiB and diskGiB above zero on profile "+name+" of "+provider.Identity()))
			}
			for _, attachment := range unrealizedAttachments(provider, machine) {
				found = append(found, lifecycle.RefusalOf(machine,
					"a KubeVirt NIC attaches to a Localnet ClusterUserDefinedNetwork or a bridge NetworkAttachmentDefinition, and attachment "+attachment+" names another kind",
					"select a ClusterUserDefinedNetwork or NetworkAttachmentDefinition attachment for every NIC of "+machine.Identity()))
			}
		}
	}
	return lifecycle.SortRefusals(found)
}

func spreadOf(catalog api.Catalog, contextName, machine string) string {
	for _, cluster := range catalog.OfKind(api.OpenShiftCluster) {
		for _, node := range cluster.Spec().Get("nodes").Items() {
			if node.Get("machineRef").Text() == machine {
				digest := sha256.Sum256([]byte(strings.Join([]string{"bootwright.substrate.kubevirt.spread-v1", contextName, cluster.Name(), node.Get("role").Text()}, "\x00")))
				return hex.EncodeToString(digest[:8])
			}
		}
	}
	return ""
}

// providers lists the KubeVirt providers this arm realizes.
func providers(catalog api.Catalog) []api.Object {
	var found []api.Object
	for _, provider := range substrate.ProvidersOn(catalog, substrate.ArmKubeVirt) {
		if substrate.IsRealized(provider) {
			found = append(found, provider)
		}
	}
	return found
}

func sized(profile api.Value) bool {
	for _, field := range []string{"cpu", "memoryMiB", "diskGiB"} {
		if value, ok := profile.Get(field).Int64(); !ok || value <= 0 {
			return false
		}
	}
	return true
}

func unrealizedAttachments(provider, machine api.Object) []string {
	var refused []string
	for _, nic := range substrate.MachineNICs(machine) {
		reference := substrate.NICAttachment(provider, machine, nic)
		attachment, found := findNamed(provider.Spec().Get("networkAttachments"), "name", reference)
		if !found {
			continue
		}
		if _, ok := networkOf(provider, attachment); !ok && !slices.Contains(refused, reference) {
			refused = append(refused, reference)
		}
	}
	return refused
}

// HostRequests derives one frozen proof request per KubeVirt provider, in
// canonical object order. It reads no host, endpoint or Secret material.
func HostRequests(catalog api.Catalog, controllerMachine, contextName string) ([]HostRequest, error) {
	if !api.ValidLexical("name", contextName) {
		return nil, refusal("lifecycle.state", "the lifecycle context identity is invalid", "")
	}
	placement, err := placementFor(catalog, controllerMachine)
	if err != nil {
		return nil, err
	}
	var requests []HostRequest
	owners := map[string]string{}
	for _, provider := range providers(catalog) {
		arm := provider.Spec().Get(substrate.ArmKubeVirt)
		request := HostRequest{
			Access:        slices.Clone(verbs),
			HostCluster:   substrate.HostCluster(provider),
			Identity:      Identity{Block: HostBlockID(provider.Name()), Context: contextName, Object: provider.Name()},
			KubeconfigRef: arm.Get("kubeconfigRef").Text(),
			MACAddresses:  []string{},
			Namespace:     arm.Get("namespace").Text(),
			Networks:      []Network{},
			Placement:     placement,
			StorageClass:  arm.Get("storageClassRef").Text(),
			Version:       hostRequestVersion,
		}
		if request.HostCluster != "" {
			request.ServiceAccount = substrate.HostAccount
		}
		for _, attachment := range provider.Spec().Get("networkAttachments").Items() {
			if network, ok := networkOf(provider, attachment); ok {
				request.Networks = append(request.Networks, network)
			}
		}
		for _, machine := range substrate.HostedMachines(catalog, provider.Name()) {
			target, err := substrate.TargetFor(catalog, machine, contextName, controllerMachine)
			if err != nil {
				return nil, err
			}
			for _, iface := range target.Interfaces {
				owner := machine.Identity() + " NIC " + iface.Name
				if held, found := owners[iface.MACAddress]; found {
					first, second := held, owner
					if first > second {
						first, second = second, first
					}
					remedy := "rename a NIC in spec.hardware.nics of one of the two Machines, or rename that Machine"
					if strings.HasPrefix(held, machine.Identity()+" NIC ") {
						remedy = "rename one of the two NICs in spec.hardware.nics of " + machine.Identity()
					}
					return nil, refusal("lifecycle.unsupported", "a KubeVirt NIC's MAC address is derived from the context, Machine and NIC names, and "+first+" and "+second+" derive the same address, "+iface.MACAddress,
						remedy)
				}
				owners[iface.MACAddress] = owner
				request.MACAddresses = append(request.MACAddresses, iface.MACAddress)
			}
		}
		slices.Sort(request.MACAddresses)
		request.MACAddresses = slices.Compact(request.MACAddresses)
		requests = append(requests, request)
	}
	return requests, nil
}

// MachineRequests derives one frozen request per Machine a KubeVirt provider
// realizes, in canonical object order.
func MachineRequests(catalog api.Catalog, controllerMachine, contextName string) ([]MachineRequest, error) {
	if !api.ValidLexical("name", contextName) {
		return nil, refusal("lifecycle.state", "the lifecycle context identity is invalid", "")
	}
	var requests []MachineRequest
	for _, provider := range providers(catalog) {
		for _, machine := range substrate.HostedMachines(catalog, provider.Name()) {
			request, err := machineRequestFor(catalog, provider, machine, controllerMachine, contextName)
			if err != nil {
				return nil, err
			}
			requests = append(requests, request)
		}
	}
	slices.SortFunc(requests, func(x, y MachineRequest) int { return strings.Compare(x.Identity.Object, y.Identity.Object) })
	return requests, nil
}

func machineRequestFor(catalog api.Catalog, provider, machine api.Object, controllerMachine, contextName string) (MachineRequest, error) {
	name := machine.Name()
	if !substrate.SafeSegment(name) {
		return MachineRequest{}, refusal("lifecycle.state", "the Machine name is not a safe host identifier", "rename "+machine.Identity())
	}
	target, err := substrate.TargetFor(catalog, machine, contextName, controllerMachine)
	if err != nil {
		return MachineRequest{}, err
	}
	placement, err := machineref.PlacementFor(target.PlacementMachine, controllerMachine)
	if err != nil {
		return MachineRequest{}, err
	}
	_, profile, found := substrate.SelectedProfile(catalog, machine)
	if !found || !sized(profile) {
		return MachineRequest{}, refusal("api.reference", "the Machine's KubeVirt profile does not resolve to a sized profile",
			"correct "+substrate.MachineProfilePath(machine)+" on "+machine.Identity())
	}
	arm := provider.Spec().Get(substrate.ArmKubeVirt)
	vm := substrate.KubeVirtName(contextName, name)
	cpu, _ := profile.Get("cpu").Int64()
	memory, _ := profile.Get("memoryMiB").Int64()
	disk, _ := profile.Get("diskGiB").Int64()
	request := MachineRequest{
		CPU: int(cpu), DiskGiB: int(disk), MemoryMiB: int(memory),
		HostCluster:   substrate.HostCluster(provider),
		Identity:      Identity{Block: MachineBlockID(name), Context: contextName, Object: name},
		KubeconfigRef: arm.Get("kubeconfigRef").Text(),
		MediaVolume:   vm + "-media",
		Name:          vm,
		Namespace:     arm.Get("namespace").Text(),
		Placement:     placement,
		Provider:      provider.Name(),
		RootVolume:    vm + "-root",
		Spread:        spreadOf(catalog, contextName, name),
		StorageClass:  arm.Get("storageClassRef").Text(),
		Version:       machineRequestVersion,
	}
	if request.HostCluster != "" {
		request.ServiceAccount = substrate.HostAccount
	}
	if profile.Has("tpm") {
		request.TPM = &TPM{Persistent: substrate.PersistentTPM(profile)}
	}
	for _, iface := range target.Interfaces {
		reference := substrate.NICAttachment(provider, machine, iface.Name)
		attachment, found := findNamed(provider.Spec().Get("networkAttachments"), "name", reference)
		if !found {
			return MachineRequest{}, refusal("api.reference", "NIC "+iface.Name+" of the Machine selects no attachment of its provider",
				"add a spec.substrate.kubevirt.nics entry for NIC "+iface.Name+" naming an attachment of "+provider.Identity()+" on "+machine.Identity())
		}
		network, ok := networkOf(provider, attachment)
		if !ok {
			return MachineRequest{}, refusal("lifecycle.unsupported", "a KubeVirt NIC attaches to a Localnet ClusterUserDefinedNetwork or a bridge NetworkAttachmentDefinition, and attachment "+reference+" names another kind",
				"select a ClusterUserDefinedNetwork or NetworkAttachmentDefinition attachment for every NIC of "+machine.Identity())
		}
		multus := network.Name
		if network.Namespace != request.Namespace {
			multus = network.Namespace + "/" + network.Name
		}
		request.Interfaces = append(request.Interfaces, Interface{MACAddress: iface.MACAddress, Name: iface.Name, Network: multus})
	}
	return request, nil
}

// networkOf resolves one attachment to the NetworkAttachmentDefinition the
// VirtualMachine names: a ClusterUserDefinedNetwork's derived one of its own
// name in the provider's namespace, or the declared one. Any other kind is
// not realized.
func networkOf(provider api.Object, attachment api.Value) (Network, bool) {
	ref := attachment.Get(substrate.ArmKubeVirt, "networkRef")
	namespace := provider.Spec().Get(substrate.ArmKubeVirt, "namespace").Text()
	group, kind, name := ref.Get("apiGroup").Text(), ref.Get("kind").Text(), ref.Get("name").Text()
	network := Network{Attachment: attachment.Get("name").Text(), Name: name, Namespace: namespace}
	switch {
	case group == cudnGroup && kind == cudnKind:
		network.Derived = name
		return network, true
	case group == nadGroup && kind == nadKind:
		if declared := ref.Get("namespace").Text(); declared != "" {
			network.Namespace = declared
		}
		return network, true
	}
	return Network{}, false
}

// placementFor runs every effect on the controller, which holds the lent
// kubeconfig and the controller's virtctl.
func placementFor(catalog api.Catalog, controllerMachine string) (machineref.Placement, error) {
	host, found := catalog.Find(api.Machine, controllerMachine)
	if !found {
		return machineref.Placement{}, refusal("api.reference", "the Environment's controller Machine is not in the selected graph",
			"declare it or correct spec.controller.machineRef")
	}
	return machineref.PlacementFor(host, controllerMachine)
}

func findNamed(values api.Value, key, name string) (api.Value, bool) {
	if name == "" {
		return api.Value{}, false
	}
	for _, value := range values.Items() {
		if value.Get(key).Text() == name {
			return value, true
		}
	}
	return api.Value{}, false
}

func refusal(code, message, remediation string) error {
	return diagnostics.NewFailureWithRemediation(code, message, "", remediation)
}
