package diskdiscovery

import (
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/machine/diskselection"
	"github.com/crmarques/bootwright/internal/managedos/installation"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/substrate"
)

type AgentPreflight struct {
	Cluster string                     `json:"cluster"`
	Root    diskselection.DiskIdentity `json:"root"`
}

const agentProofLifetime = time.Hour

func agentRequest(catalog api.Catalog, m api.Object, input lifecycle.PlanInput, records map[string]installation.MediaRecord) (DiscoveryRequest, installation.Requirements, error) {
	needs := installation.Requirements{Machine: m.Name()}
	var cluster api.Object
	for _, candidate := range catalog.OfKind(api.OpenShiftCluster) {
		if candidate.Spec().Has("install", "agent") && slices.ContainsFunc(candidate.Spec().Get("nodes").Items(), func(node api.Value) bool {
			return node.Get("machineRef").Text() == m.Name()
		}) {
			if cluster.Name() != "" {
				return DiscoveryRequest{}, needs, discoveryUnsupported.Fail("the Linux preflight Machine belongs to multiple Agent clusters", "")
			}
			cluster = candidate
		}
	}
	if cluster.Name() == "" {
		return DiscoveryRequest{}, needs, discoveryUnsupported.Fail("the Linux preflight Machine belongs to no Agent cluster", "bind it to the nodes of one OpenShift Agent cluster")
	}
	if reason, remedy := machine.AgentRootDeviceRefusal(catalog, m); reason != "" {
		return DiscoveryRequest{}, needs, discoveryUnsupported.Fail(reason, remedy)
	}
	target, err := substrate.TargetFor(catalog, m, input.Context.Name, input.Controller)
	if err != nil {
		return DiscoveryRequest{}, needs, err
	}
	controller := target.Controller
	endpoint, err := url.Parse(controller.Endpoint)
	if !target.Physical || !controller.TLSVerify || controller.TLSVerification == "none" || err != nil || endpoint.Scheme != "https" {
		return DiscoveryRequest{}, needs, discoveryUnsupported.Fail("Linux Agent preflight requires a physical Machine with verified HTTPS BMC management", "use full or certificate verification for the BMC connection")
	}
	policy := substrate.MachineLinuxPreflight(m)
	boot, err := installation.PinInventoryImage(catalog, policy.Get("imageRef").Text(), m.Identity(), records)
	if err != nil {
		return DiscoveryRequest{}, needs, err
	}
	publication, placement, server, err := installation.InventoryPublication(catalog, cluster.Spec().Get("install", "agent", "redfishVirtualMedia", "artifactServerEndpoint"), input.Controller, input.Context.Name, m.Name(), cluster.Identity())
	if err != nil {
		return DiscoveryRequest{}, needs, err
	}
	if !placement.Local() || publication.CertificateRef == "" || !strings.HasPrefix(publication.URL, "https://") {
		return DiscoveryRequest{}, needs, discoveryUnsupported.Fail("Linux Agent preflight requires a controller-local private HTTPS artifact server", "choose a controller-local HTTPS server for the cluster's virtual media")
	}
	needs.ArtifactServers = []string{server}
	publication.Path += "/discovery"
	publication.URL += "/discovery"
	network, err := installation.InventoryNetworkFor(catalog, m, target, &needs)
	if err != nil {
		return DiscoveryRequest{}, needs, err
	}
	if _, err := installation.NetworkLine(network); err != nil {
		return DiscoveryRequest{}, needs, err
	}
	hints := target.RootDeviceHints
	size, err := strconv.ParseInt(hints.SizeBytes, 10, 64)
	if err != nil {
		return DiscoveryRequest{}, needs, err
	}
	port := int64(22)
	if specified, ok := policy.Get("sshPort").Int64(); ok {
		port = specified
	}
	r := DiscoveryRequest{
		Agent:     &AgentPreflight{Cluster: cluster.Name(), Root: diskselection.DiskIdentity{WWN: hints.WWN, SerialNumber: hints.SerialNumber, SizeBytes: size}},
		BootMedia: boot, DeadlineSeconds: 1800, Identity: installation.Identity{Block: DiscoveryBlockID(m.Name()), Context: input.Context.Name, Object: m.Name(), Profile: policy.Get("imageRef").Text()},
		Inventory: InventoryBoot, MachinePort: target.MachinePort(""), Placement: placement, Private: publication, SSH: DiscoverySSH{Port: int(port)}, Version: discoveryVersion,
		Controller: installation.Controller{Endpoint: controller.Endpoint, CredentialsRef: controller.CredentialsRef, TrustBundleRef: controller.TrustBundleRef,
			TLSVerification: controller.TLSVerification, TLSVerify: controller.TLSVerify, VirtualMedia: installation.VirtualMedia{
				Trust: controller.VirtualMedia.Trust, EnableVerification: controller.VirtualMedia.EnableVerification,
				RestoreVerification: controller.VirtualMedia.RestoreVerification, RemoveCertificate: controller.VirtualMedia.RemoveCertificate,
			}},
		Network: DiscoveryNetwork{Address: network.Address, Prefix: network.Prefix, Gateway: network.Gateway, Interface: network.Interface, Nameservers: network.Nameservers,
			Type: network.InterfaceType, VLANBase: network.VLANBase, VLANID: network.VLANID, BondPorts: network.BondPorts, BondOptions: network.BondOptions},
	}
	names := map[string]string{}
	for _, match := range machine.InterfaceMatches(m, catalog) {
		names[match.NIC] = match.Interface
	}
	for _, nic := range target.Interfaces {
		name := nic.Name
		if matched := names[nic.Name]; matched != "" {
			name = matched
		}
		r.Interfaces = append(r.Interfaces, installation.Interface{MACAddress: nic.MACAddress, Name: name})
	}
	return r, needs, nil
}

func soleAgentRoot(report diskselection.Report, root diskselection.DiskIdentity) bool {
	if report.Source != diskselection.SourceLinux || len(report.Disks) != 1 {
		return false
	}
	disk := report.Disks[0]
	return disk.Whole && disk.Fixed && disk.Writable && !disk.Hidden && (disk.Transport == "scsi" || disk.Transport == "nvme") && disk.DiskIdentity == root
}

func ProveAgentRoot(e lifecycle.Execution, cluster, machine, uuid string, macs []string, root diskselection.DiskIdentity, now time.Time) (int64, error) {
	records := e.ApplyProved
	if len(records) == 0 {
		records = e.Proved
	}
	refuse := func() error {
		return discoveryRootUnprovable.Fail("Machine/"+machine+" has no fresh, matching single-disk Linux preflight for this Agent installation",
			"inspect the root identity and all attached storage; if the receipt expired, use scoped recovery to clear the interrupted installation before starting a fresh apply")
	}
	var deadline int64
	slices.Sort(macs)
	for _, record := range records {
		if record.Object != machine || record.Implementation != DiscoveryImplementation {
			continue
		}
		if deadline != 0 || record.Operation != e.ApplyOperation || record.Verb != reconciliation.Apply ||
			record.State != reconciliation.BlockDone || record.Block.ID != DiscoveryBlockID(machine) || record.Block.ContentDigest != DiscoveryContentDigest() {
			return 0, refuse()
		}
		request, err := DecodeDiscoveryRequest(record.Block.Request)
		if err != nil || request.Agent == nil || request.Agent.Cluster != cluster || request.Agent.Root != root ||
			request.Identity.Context != e.Context || request.Identity.Object != machine || request.Inventory != InventoryBoot {
			return 0, refuse()
		}
		evidence, report, err := discoveryEvidence(record.Evidence, record.Block.RequestDigest)
		if err != nil || !evidence.Completed || !evidence.Cleaned || !soleAgentRoot(report, root) ||
			evidence.BootStartedAt <= 0 || evidence.BootStartedAt > now.Unix() {
			return 0, refuse()
		}
		b := report.Binding
		if b.Context != e.Context || b.Incarnation != e.Incarnation || b.Machine != machine || b.Operation != e.ApplyOperation ||
			b.RequestDigest != record.Block.RequestDigest || b.ToolDigest != record.Block.ContentDigest || b.MediaSHA256 != request.BootMedia.SHA256 ||
			b.UUID != strings.ToLower(strings.TrimSpace(uuid)) || !slices.Equal(b.MACs, macs) || !slices.Equal(b.MACs, discoveryMACs(request)) {
			return 0, refuse()
		}
		deadline = evidence.BootStartedAt + int64(agentProofLifetime/time.Second)
	}
	if deadline == 0 {
		return 0, refuse()
	}
	return deadline, nil
}
