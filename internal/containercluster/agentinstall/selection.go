package agentinstall

import (
	"cmp"
	"net/netip"
	"slices"
	"strconv"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/containercluster/openshift"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/infrastructureservices/artifactserver"
	"github.com/crmarques/bootwright/internal/infrastructureservices/managedservice"
	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/substrate"
)

// unsupportedSelections are the cluster choices that carry effects this
// contract does not prove. A selected one refuses before registration rather
// than installing part of what was declared.
var unsupportedSelections = [][]string{
	{"install", "servingCertificates"},
}

// Requirements names the API objects one cluster's blocks wait for, so those
// blocks complete before the installer needs what they realize.
type Requirements struct {
	ArtifactServers []string
	DNSServers      []string
	Machines        []string
	NTPServers      []string
	Proxies         []string
}

// Refusals refuses every selected cluster this contract cannot install, with
// the reason and remedy its request derivation would give, so an operation
// refuses before registration saying why and what to change.
func Refusals(catalog api.Catalog) []lifecycle.Refusal {
	var found []lifecycle.Refusal
	for _, cluster := range catalog.OfKind(api.OpenShiftCluster) {
		if field, reason, remediation := refusedCluster(catalog, cluster); reason != "" {
			refused := lifecycle.RefusalOf(cluster, reason, remediation)
			refused.Field = field
			found = append(found, refused)
		}
	}
	return lifecycle.SortRefusals(found)
}

// refusedCluster says why one cluster is not installable and what an operator
// changes, or nothing when it is installable. Correcting the cluster is the
// remedy unless the reason names another object to change, so selection and
// derivation give the one remedy the refusal table states. The field is the
// refused declaration's path when one endpoint field is what it refuses.
func refusedCluster(catalog api.Catalog, cluster api.Object) (field, reason, remediation string) {
	reason, remediation = unsupportedReason(catalog, cluster)
	if reason == "" {
		field, reason, remediation = unusedEndpointField(cluster)
	}
	if reason != "" && remediation == "" {
		remediation = "correct " + cluster.Identity()
	}
	return field, reason, remediation
}

// nativeEndpoints are the port the agent installer serves each endpoint slot
// on, always over https. No installer input carries another port or protocol,
// so an authored value that differs would be declared and never honored.
var nativeEndpoints = []struct {
	slot string
	port int64
}{
	{"api", 6443},
	{"api-int", 6443},
	{"ingress", 443},
}

// unusedEndpointField refuses the first endpoint declaration the agent
// installer would not follow, in slot order: a port or scheme other than the
// native one, any prefix length, and a non-empty interface-network list.
func unusedEndpointField(cluster api.Object) (field, reason, remediation string) {
	for _, native := range nativeEndpoints {
		endpoint := cluster.Spec().Get("install", "endpoints", native.slot)
		path := "spec.install.endpoints." + native.slot
		port := strconv.FormatInt(native.port, 10)
		if declared, ok := endpoint.Get("port").Int64(); endpoint.Has("port") && (!ok || declared != native.port) {
			return "$." + path + ".port", "the agent installer serves endpoint " + native.slot + " on port " + port + " and has no input for another",
				"remove " + path + ".port from " + cluster.Identity() + ", or set it to " + port
		}
		if endpoint.Has("scheme") && endpoint.Get("scheme").Text() != "https" {
			return "$." + path + ".scheme", "the agent installer serves endpoint " + native.slot + " over https and has no input for another protocol",
				"remove " + path + ".scheme from " + cluster.Identity() + ", or set it to https"
		}
		if endpoint.Has("prefixLength") {
			return "$." + path + ".prefixLength", "no agent installer input consumes endpoint " + native.slot + "'s prefixLength",
				"remove " + path + ".prefixLength from " + cluster.Identity()
		}
		if endpoint.Get("interfaceNetworks").Len() > 0 {
			return "$." + path + ".interfaceNetworks", "no agent installer input consumes endpoint " + native.slot + "'s interfaceNetworks",
				"remove " + path + ".interfaceNetworks from " + cluster.Identity() + ", or leave it empty"
		}
	}
	return "", "", ""
}

// unsupportedReason says why one cluster is not installable, or nothing when
// it is. It is the one place the supported shape is stated, so the refusal and
// the derivation can never disagree about what this contract installs. The
// remediation is empty when correcting the cluster itself is the remedy, and
// names the bound Machine when that Machine is what an operator changes.
func unsupportedReason(catalog api.Catalog, cluster api.Object) (reason, remediation string) {
	spec := cluster.Spec()
	if spec.Get("distribution", "type").Text() != "openshift" {
		return "this executable installs no OKD cluster", ""
	}
	if spec.Get("distribution", "release", "version").Text() == "" {
		return "a cluster pinned to a release image alone names no version for its installer to match", ""
	}
	if mode := spec.Get("install", "mode").Text(); mode != "" && mode != "connected" {
		return "this executable installs no disconnected cluster", ""
	}
	if reason, remediation := encryptionRefusal(catalog, cluster); reason != "" {
		return reason, remediation
	}
	if _, _, err := installationProxy(catalog, cluster); err != nil {
		return featureRefusal(err)
	}
	if _, err := registryMirror(catalog, cluster); err != nil {
		return featureRefusal(err)
	}
	for _, path := range unsupportedSelections {
		if spec.Has(path...) {
			return "this executable does not install " + strings.Join(path, "."), ""
		}
	}
	if spec.Get("nodes").Len() > 1 && !multiNodePlatform(openshift.Platform(spec)) {
		return "this executable installs no multi-node cluster on the declared platform", ""
	}
	if reason, remediation := pastTheCeiling(catalog, cluster); reason != "" {
		return reason, remediation
	}
	controllerMachine, _ := lifecycle.ControllerMachine(catalog) // Placement refuses an absent controller.
	server, serverHost, served := mediaServer(catalog, cluster)

	for _, node := range spec.Get("nodes").Items() {
		bound, found := catalog.Find(api.Machine, node.Get("machineRef").Text())
		if !found {
			continue
		}
		// A node Bootwright also installs an operating system on would have
		// its one disk written by two installations.
		if machine.Installed(bound) {
			return "a declared node selects an install profile, so two installations would write its disk",
				"remove spec.os.installProfileRef from " + bound.Identity() + " or drop it from " + cluster.Identity()
		}
		if native, errors := machine.ComposeNetwork(bound, catalog); len(errors) == 0 {
			if issues := openshift.NodeNetworkIssues(bound, catalog, native); len(issues) > 0 {
				return issues[0].Message, issues[0].Remediation
			}
		}
		target, err := substrate.TargetFor(catalog, bound, cluster.Name(), controllerMachine)
		if err != nil {
			reason, remedy := nodeTargetRefusal(catalog, bound, err)
			if len(diagnostics.Of(err)) > 0 && !strings.Contains(reason, "substrate this executable") {
				reason = "node " + node.Get("name").Text() + " (" + bound.Identity() + "): " + reason
			}
			return reason, remedy
		}
		if target.Physical {
			if reason, remedy := targetPrivateRefusal(bound.Name(), target.Controller); reason != "" {
				return reason, remedy
			}
		}
		if _, reason, remediation := installerRootDeviceHints(bound, target.RootDeviceHints); reason != "" {
			return reason, remediation
		}
		// A virtual node's controller is emulated on its provider host and
		// fetches the private image without verifying the server, so the token
		// is the image's only protection unless that fetch never leaves the
		// host. The server is therefore placed on that same host.
		if !target.Physical && served && target.PlacementMachine.Name() != serverHost.Name() {
			return "an emulated BMC fetches the boot image without verifying its server, so the server is placed on the provider host that BMC runs on",
				bound.Identity() + " is booted through a BMC on " + target.PlacementMachine.Identity() + " and " +
					server.Identity() + " is placed on " + serverHost.Identity() + "; place " +
					string(api.InfraProvider) + "/" + target.Provider + " and " + server.Identity() + " on the same Machine"
		}
	}
	if served && serverHost.Name() != controllerMachine {
		return "a cluster's boot image is built on the controller, so the server it is published through is placed there", "set spec.machineRef on " + server.Identity() + " to the controller Machine"
	}
	return "", ""
}

// nodeTargetRefusal says why a node's realized target does not derive. A node
// on a provider whose substrate this executable does not realize is refused as
// the cluster's own; every other reason is the substrate's own, with the
// remedy naming the object to change. A reason the error does not carry still
// refuses, because an empty reason would admit the cluster.
func nodeTargetRefusal(catalog api.Catalog, bound api.Object, err error) (reason, remediation string) {
	provider, found := catalog.Find(api.InfraProvider, bound.Spec().Get("substrate", "providerRef").Text())
	if found && !slices.Contains(substrate.Realized(), substrate.Variant(provider)) {
		return "a declared node is on a substrate this executable does not realize", ""
	}
	if refused := diagnostics.Of(err); len(refused) != 0 && refused[0].Message != "" {
		return refused[0].Message, refused[0].Remediation
	}
	return "a declared node's realized target does not derive", ""
}

// pastTheCeiling says why a cluster's installation could not finish within the
// deadline its runs are held to, or nothing when it can. The deadline grows with
// rounds of nodes the installation boots, reads and releases, and the runner cuts
// every run short at its ceiling rather than honoring a longer one, so a
// cluster too large for it refuses here instead of being killed mid-install.
func pastTheCeiling(catalog api.Catalog, cluster api.Object) (reason, remediation string) {
	nodes := cluster.Spec().Get("nodes").Len()
	arms, enforced := clusterArms(catalog, cluster), enforcedArms(catalog, cluster)
	deadline := installDeadline(installBudgets(nodes, enforced, arms...), nodes)
	if deadline <= lifecycle.MaxDeadline {
		return "", ""
	}
	return "installing " + strconv.Itoa(nodes) + " nodes needs a run deadline of " + deadline.String() +
			", past the " + lifecycle.MaxDeadline.String() + " every adapter run is held to",
		"declare at most " + strconv.Itoa(largestCluster(enforced, arms...)) + " nodes on " + cluster.Identity()
}

// largestCluster is the most nodes whose installation deadline fits within the
// ceiling. The plan bounds the declared Machine set at 512.
func largestCluster(enforced []string, arms ...string) int {
	for nodes := 512; nodes > 0; nodes-- {
		if installDeadline(installBudgets(nodes, enforced, arms...), nodes) <= lifecycle.MaxDeadline {
			return nodes
		}
	}
	return 0
}

// mediaServer is the artifact server the cluster's boot image is published
// through and the Machine it is placed on, which is where every node's
// controller fetches that image from. It is absent while the selection does
// not resolve, which derivation then refuses naming the selection itself.
func mediaServer(catalog api.Catalog, cluster api.Object) (server, host api.Object, found bool) {
	selection := cluster.Spec().Get("install", "agent", "redfishVirtualMedia", "artifactServerEndpoint")
	server, err := artifactserver.Selected(catalog, selection, cluster.Identity())
	if err != nil {
		return api.Object{}, api.Object{}, false
	}
	host, found = catalog.Find(api.Machine, server.Spec().Get("machineRef").Text())
	return server, host, found
}

// Requests derives one media and one install request per selected cluster, in
// canonical name order. It reads no host, endpoint or Secret material.
func Requests(catalog api.Catalog, controllerMachine, contextName string) ([]MediaRequest, []InstallRequest, []Requirements, error) {
	var media []MediaRequest
	var installs []InstallRequest
	var requirements []Requirements
	clusters := slices.Clone(catalog.OfKind(api.OpenShiftCluster))
	slices.SortFunc(clusters, func(x, y api.Object) int { return strings.Compare(x.Name(), y.Name()) })
	for _, cluster := range clusters {
		mediaRequest, installRequest, needs, err := requestFor(catalog, cluster, controllerMachine, contextName)
		if err != nil {
			return nil, nil, nil, err
		}
		media = append(media, mediaRequest)
		installs = append(installs, installRequest)
		requirements = append(requirements, needs)
	}
	return media, installs, requirements, nil
}

func requestFor(catalog api.Catalog, cluster api.Object, controllerMachine, contextName string) (MediaRequest, InstallRequest, Requirements, error) {
	name := cluster.Name()
	empty := func(err error) (MediaRequest, InstallRequest, Requirements, error) {
		reported := diagnostics.Of(err)
		for i := range reported {
			reported[i].Object = &diagnostics.ObjectIdentity{APIVersion: api.APIVersion, Kind: string(cluster.Kind()), Name: cluster.Name()}
		}
		if len(reported) != 0 {
			err = &diagnostics.Failure{Diagnostics: reported}
		}
		return MediaRequest{}, InstallRequest{}, Requirements{}, err
	}
	if !substrate.SafeSegment(name) {
		return empty(refusalAt("lifecycle.state", "$.metadata.name", "the cluster name is not a safe host identifier", "rename "+cluster.Identity()))
	}
	if field, reason, remediation := refusedCluster(catalog, cluster); reason != "" {
		return empty(refusalAt("lifecycle.unsupported", field, reason, remediation))
	}
	needs := Requirements{}
	nodes, err := nodeProjections(catalog, cluster, contextName, controllerMachine, &needs)
	if err != nil {
		return empty(err)
	}
	delivery, image, certificate, placement, err := mediaLocation(catalog, cluster, controllerMachine, contextName, &needs)
	if err != nil {
		return empty(err)
	}

	timeSources, err := timeAddresses(catalog, cluster, &needs)
	if err != nil {
		return empty(err)
	}
	install, err := installConfig(catalog, cluster, nodes)
	if err != nil {
		return empty(err)
	}
	agent, err := agentConfig(cluster, nodes, timeSources)
	if err != nil {
		return empty(err)
	}
	release := Release{
		Distribution: cluster.Spec().Get("distribution", "type").Text(),
		Version:      cluster.Spec().Get("distribution", "release", "version").Text(),
	}
	tool := Tool{Compatibility: release.Distribution, Kind: installerTool, Version: release.Version}
	if cluster.Spec().Get("security", "fips", "enabled").Bool() {
		tool.Kind = "openshift-install-fips"
	}
	identity := func(block string) Identity {
		return Identity{Block: block, Cluster: name, Context: contextName}
	}
	pullSecret := cluster.Spec().Get("install", "pullSecretRef").Text()
	if pullSecret == "" {
		return empty(refusalAt("api.required", "$.spec.install.pullSecretRef", "the cluster names no pull secret", "set spec.install.pullSecretRef on "+cluster.Identity()))
	}
	sshKey, err := clusterKeyRef(cluster)
	if err != nil {
		return empty(err)
	}
	needs.ArtifactServers = sortedUnique(needs.ArtifactServers)
	needs.DNSServers, needs.NTPServers = sortedUnique(needs.DNSServers), sortedUnique(needs.NTPServers)
	needs.Machines = sortedUnique(needs.Machines)
	mediaRequest := MediaRequest{
		AgentConfig: agent, Budgets: mediaBudgets, DeliveryKind: delivery, Identity: identity(MediaBlockID(name)), Image: image,
		InstallConfig: install, Manifests: installManifests(cluster), Placement: placement, PullSecretRef: pullSecret,
		Release: release, SSHKeyRef: sshKey, TLSCertificateRef: certificate, Tool: tool,
		TrustBundleRefs: cluster.Spec().Get("install", "additionalTrustBundleRefs").Strings(),
		Version:         mediaRequestVersion, WorkRoot: WorkRoot(contextName, name),
	}
	if err := projectMediaFeatures(catalog, cluster, controllerMachine, &mediaRequest, &needs); err != nil {
		return empty(err)
	}
	installRequest := InstallRequest{
		DeliveryKind: delivery, Rendezvous: agent["rendezvousIP"].(string), PublicationRoot: image, TLSCertificateRef: certificate, VIPs: projectedVIPs(catalog, cluster),
		Budgets: installBudgets(len(nodes), enforcedArms(catalog, cluster), clusterArms(catalog, cluster)...), Endpoints: endpointNames(catalog, cluster),
		Identity: identity(InstallBlockID(name)), Image: image, Nodes: frozenNodes(nodes),
		Placement: placement, Release: release, Tool: tool,
		Version: installRequestVersion, WorkRoot: WorkRoot(contextName, name),
	}
	if err := bindPushedMedia(mediaRequest, needs, &installRequest); err != nil {
		return empty(err)
	}
	if installRequest.PortTools, err = portTools(catalog, nodes); err != nil {
		return empty(err)
	}

	if len(installRequest.Endpoints) == 0 {
		return empty(refusalAt("api.required", "$.spec.install.endpoints", "the cluster resolves no endpoint address for its controller to reach",
			"declare spec.install.endpoints on "+cluster.Identity()))
	}
	return mediaRequest, installRequest, needs, nil
}

// projectMediaFeatures freezes optional installation features and their dependencies.
func projectMediaFeatures(catalog api.Catalog, cluster api.Object, controllerMachine string, request *MediaRequest, needs *Requirements) error {
	mirror, err := registryMirror(catalog, cluster)
	if err != nil {
		return err
	}

	proxy, managedProxy, err := installationProxy(catalog, cluster)
	if err != nil {
		return err
	}
	if managedProxy != "" {
		needs.Proxies = append(needs.Proxies, managedProxy)
	}
	host, _ := catalog.Find(api.Machine, controllerMachine)
	egress, err := managedservice.EgressFor(catalog, host)
	if err != nil {
		return err
	}
	buildEgress := map[string]string(nil)
	if egress.HTTPProxy != "" || egress.HTTPSProxy != "" {
		if egress.HTTPProxy == "" {
			egress.HTTPProxy = egress.HTTPSProxy
		}
		if egress.HTTPSProxy == "" {
			egress.HTTPSProxy = egress.HTTPProxy
		}
		buildEgress = map[string]string{"HTTP_PROXY": egress.HTTPProxy, "HTTPS_PROXY": egress.HTTPSProxy, "NO_PROXY": strings.Join(egress.NoProxy, ",")}
	}
	request.RegistryMirror, request.InstallProxy = mirror, proxy
	request.BuildEgress, request.DiskEncryption = buildEgress, diskEncryption(cluster)
	return nil
}

// clusterKeyRef is the cluster administration key the installation authorizes
// on every node. Only its public half ever leaves the binding.
func clusterKeyRef(cluster api.Object) (string, error) {
	ssh := cluster.Spec().Get("install", "nodeSSH")
	for _, key := range []string{"keyPairRef", "publicKeyRef"} {
		if reference := ssh.Get(key).Text(); reference != "" {
			return reference, nil
		}
	}
	return "", refusalAt("api.required", "$.spec.install.nodeSSH", "the cluster names no administration key for its nodes",
		"set spec.install.nodeSSH on "+cluster.Identity())
}

// nodeProjection is one declared node with everything both installer inputs
// and the boot itself need, derived once.
type nodeProjection struct {
	address         string
	interfaces      []substrate.Interface
	machine         api.Object
	name            string
	network         map[string]any
	role            string
	labels          map[string]string
	taints          []Taint
	rootDeviceHints map[string]any
	requireTPM      bool
	target          substrate.Target
}

// maxInstallerRootDeviceGigabytes is the largest minSizeGigabytes the frozen
// media request carries exactly, because it is read back as a float64.
const maxInstallerRootDeviceGigabytes = 1<<53 - 1

// installerRootDeviceHints renders every root-device hint a node declares
// under its admitted name and with its declared type, or nothing when it
// declares none. A hint the agent installer or the frozen request cannot carry
// refuses instead, naming the bound Machine, because that is what an operator
// changes; the rule on deviceName is the installer's own for agent hosts.
func installerRootDeviceHints(bound api.Object, hints substrate.RootDeviceHints) (rendered map[string]any, reason, remediation string) {
	if len(hints.Names()) == 0 {
		return nil, "", ""
	}
	remainder := strings.TrimPrefix(strings.TrimPrefix(hints.DeviceName, "/dev/"), "disk/by-path/")
	if strings.Contains(remainder, "/") {
		return nil, "the agent installer names a root device only as /dev/<name> or /dev/disk/by-path/<name>",
			"set spec.hardware.rootDevice.deviceName on " + bound.Identity() + " to such a path"
	}
	rendered = map[string]any{}
	if hints.MinSizeGigabytes != "" {
		size, err := strconv.ParseInt(hints.MinSizeGigabytes, 10, 64)
		if err != nil || size > maxInstallerRootDeviceGigabytes {
			return nil, "a node's minSizeGigabytes is larger than the frozen installer input carries exactly",
				"declare spec.hardware.rootDevice.minSizeGigabytes on " + bound.Identity() + " as at most " +
					strconv.FormatInt(maxInstallerRootDeviceGigabytes, 10)
		}
		rendered["minSizeGigabytes"] = size
	}
	for name, value := range map[string]string{
		"deviceName": hints.DeviceName, "hctl": hints.HCTL, "model": hints.Model, "vendor": hints.Vendor,
		"serialNumber": hints.SerialNumber, "wwn": hints.WWN,
	} {
		if value != "" {
			rendered[name] = value
		}
	}
	if hints.Rotational != nil {
		rendered["rotational"] = *hints.Rotational
	}
	return rendered, "", ""
}

// nodeProjections derives every declared node in node-name order, which is the
// order the installer's own rendezvous selection follows.
func nodeProjections(catalog api.Catalog, cluster api.Object, contextName, controllerMachine string, needs *Requirements) ([]nodeProjection, error) {
	declared := slices.Clone(cluster.Spec().Get("nodes").Items())
	slices.SortFunc(declared, func(x, y api.Value) int {
		return strings.Compare(x.Get("name").Text(), y.Get("name").Text())
	})
	projections := make([]nodeProjection, 0, len(declared))
	for _, node := range declared {
		reference := node.Get("machineRef").Text()
		bound, found := catalog.Find(api.Machine, reference)
		if !found {
			return nil, refusal("api.reference", "a cluster node's Machine is not in the selected graph",
				"declare "+reference+" or correct spec.nodes[].machineRef on "+cluster.Identity())
		}
		target, err := substrate.TargetFor(catalog, bound, contextName, controllerMachine)
		if err != nil {
			return nil, err
		}
		hints, reason, remediation := installerRootDeviceHints(bound, target.RootDeviceHints)
		if reason != "" {
			return nil, refusal("lifecycle.unsupported", reason, remediation)
		}
		address, err := installAddress(catalog, bound)
		if err != nil {
			return nil, err
		}
		resolvers, err := resolverAddresses(catalog, bound, needs)
		if err != nil {
			return nil, err
		}
		network, err := networkConfig(catalog, bound, target.Interfaces, resolvers)
		if err != nil {
			return nil, err
		}
		if len(target.Interfaces) == 0 {
			return nil, refusal("api.required", "a cluster node reports no interface for the installer to match it by",
				"declare its hardware or its network interfaces on "+bound.Identity())
		}
		needs.Machines = append(needs.Machines, bound.Name())
		projections = append(projections, nodeProjection{
			address: address, interfaces: target.Interfaces, machine: bound,
			name: node.Get("name").Text(), network: network, role: node.Get("role").Text(),
			labels: nodeLabels(node), taints: nodeTaints(node),
			rootDeviceHints: hints, target: target, requireTPM: encryptedRole(cluster, node.Get("role").Text()),
		})
	}
	if len(projections) == 0 {
		return nil, refusalAt("api.required", "$.spec.nodes", "the cluster declares no node", "declare spec.nodes on "+cluster.Identity())
	}
	return projections, nil
}

// frozenNodes is what the install block freezes about each node: the machine
// to boot, the controller to boot it through, the address it answers at, and
// for a physical node the NICs its pre-boot proof requires.
func frozenNodes(nodes []nodeProjection) []Node {
	frozen := make([]Node, 0, len(nodes))
	for _, node := range nodes {
		frozen = append(frozen, Node{
			Address: node.address, ControllerKind: controllerKind(node.target.Substrate), Firmware: nodeFirmware(node.target.Firmware),
			Controller: Controller{
				CredentialsRef: node.target.Controller.CredentialsRef,
				Endpoint:       node.target.Controller.Endpoint,
				HostCluster:    node.target.Controller.HostCluster,
				TLSVerify:      node.target.Controller.TLSVerify,
				TrustBundleRef: node.target.Controller.TrustBundleRef,
				VirtualMedia: VirtualMedia{
					RemoveCertificate:   node.target.Controller.VirtualMedia.RemoveCertificate,
					RestoreVerification: node.target.Controller.VirtualMedia.RestoreVerification,
					Trust:               node.target.Controller.VirtualMedia.Trust,
				},
			},
			Hardware: frozenHardware(node.target), RequireTPM: node.requireTPM,
			Labels: node.labels, Machine: node.machine.Name(), Name: node.name, Taints: node.taints,
			StateReadSeconds: stateReadSeconds(node.target.Substrate), Physical: node.target.Physical, Substrate: node.target.Substrate,
			PortPinned: substrate.PortPinned(node.target.Substrate),
		})
	}
	return frozen
}

// InfraRoleLabel is the label an infra node carries unless it authors the key.
const InfraRoleLabel = "node-role.kubernetes.io/infra"

func nodeLabels(node api.Value) map[string]string {
	var labels map[string]string
	if node.Get("role").Text() == "infra" {
		labels = map[string]string{InfraRoleLabel: ""}
	}
	for _, label := range node.Get("labels").Fields() {
		if labels == nil {
			labels = map[string]string{}
		}
		labels[label.Name] = label.Value.Text()
	}
	return labels
}

// nodeTaints keeps the first authored taint of each key and effect, sorted.
func nodeTaints(node api.Value) []Taint {
	var taints []Taint
	for _, item := range node.Get("taints").Items() {
		taint := Taint{Effect: item.Get("effect").Text(), Key: item.Get("key").Text(), Value: item.Get("value").Text()}
		if !slices.ContainsFunc(taints, func(seen Taint) bool { return seen.Key == taint.Key && seen.Effect == taint.Effect }) {
			taints = append(taints, taint)
		}
	}
	slices.SortFunc(taints, func(x, y Taint) int {
		return cmp.Or(strings.Compare(x.Key, y.Key), strings.Compare(x.Effect, y.Effect))
	})
	return taints
}

func nodeFirmware(declared *substrate.Firmware) *Firmware {
	if declared == nil {
		return nil
	}
	firmware := &Firmware{BootMode: declared.BootMode, Mode: declared.Mode, SecureBoot: declared.SecureBoot}
	for _, attribute := range declared.BIOSAttributes {
		firmware.BIOSAttributes = append(firmware.BIOSAttributes, BIOSAttribute(attribute))
	}
	return firmware
}

// frozenHardware is the hardware a physical node is proved against: the NICs
// its Machine declares, in declared order. A node its substrate created is
// proved by its own controller answering, so it freezes none.
func frozenHardware(target substrate.Target) *Hardware {
	if !target.Physical {
		return nil
	}
	hardware := Hardware{Interfaces: make([]Interface, 0, len(target.Interfaces))}
	for _, declared := range target.Interfaces {
		hardware.Interfaces = append(hardware.Interfaces, Interface{MACAddress: declared.MACAddress, Name: declared.Name})
	}
	return &hardware
}

// installAddress is the address a node answers at once it is installed, which
// is the one static assignment its own configuration selects.
func installAddress(catalog api.Catalog, node api.Object) (string, error) {
	selected, issues := machine.InstallAddress(node, catalog)
	if len(issues) != 0 || !selected.Present() {
		return "", refusal("api.required", "a cluster node resolves no static installation address",
			"set spec.network.installAddressRef on "+node.Identity())
	}
	prefix, err := netip.ParsePrefix(selected.Get("address").Text())
	if err != nil {
		return "", refusal("api.value", "a cluster node's installation address carries no prefix",
			"declare it as an IP with its prefix on "+node.Identity())
	}
	return prefix.Addr().String(), nil
}

// resolverAddresses derives the name servers a node resolves through, from its
// own network configuration's selections, and records each managed one as a
// requirement; an external one is used at its declared address.
func resolverAddresses(catalog api.Catalog, node api.Object, needs *Requirements) ([]string, error) {
	spec, err := substrate.NetworkSpec(catalog, node)
	if err != nil {
		return nil, err
	}
	var addresses []string
	for _, selection := range spec.Get("dns").Items() {
		address, name, err := managedservice.ServiceEndpoint(catalog, api.DNSServer, selection, node.Identity())
		if err != nil {
			return nil, err
		}
		if name != "" {
			needs.DNSServers = append(needs.DNSServers, name)
		}
		if !slices.Contains(addresses, address) {
			addresses = append(addresses, address)
		}
	}
	return addresses, nil
}

// timeAddresses derives the time sources the cluster installs with, from its
// own selections, and records each managed one as a requirement; an external
// one is used at its declared address.
func timeAddresses(catalog api.Catalog, cluster api.Object, needs *Requirements) ([]string, error) {
	var addresses []string
	for _, selection := range cluster.Spec().Get("install", "ntp").Items() {
		address, name, err := managedservice.ServiceEndpoint(catalog, api.NTPServer, selection, cluster.Identity())
		if err != nil {
			return nil, err
		}
		if name != "" {
			needs.NTPServers = append(needs.NTPServers, name)
		}
		if !slices.Contains(addresses, address) {
			addresses = append(addresses, address)
		}
	}
	return addresses, nil
}

func refusal(code, message, remediation string) error {
	return diagnostics.NewFailureWithRemediation(code, message, "", remediation)
}

func refusalAt(code, field, message, remediation string) error {
	return &diagnostics.Failure{Diagnostics: []diagnostics.Diagnostic{{Severity: "error", Code: code, Field: field, Message: message, Remediation: remediation}}}
}

func clusterArms(catalog api.Catalog, cluster api.Object) []string {
	arms, _ := nodeArms(catalog, cluster)
	return arms
}

func enforcedArms(catalog api.Catalog, cluster api.Object) []string {
	_, enforced := nodeArms(catalog, cluster)
	return enforced
}

func nodeArms(catalog api.Catalog, cluster api.Object) (arms, enforced []string) {
	for _, node := range cluster.Spec().Get("nodes").Items() {
		bound, ok := catalog.Find(api.Machine, node.Get("machineRef").Text())
		if !ok {
			continue
		}
		provider, ok := catalog.Find(api.InfraProvider, bound.Spec().Get("substrate", "providerRef").Text())
		if !ok {
			continue
		}
		arms = append(arms, substrate.Variant(provider))
		if nodeFirmware(substrate.MachineFirmware(bound)).enforced() {
			enforced = append(enforced, substrate.Variant(provider))
		}
	}
	return arms, enforced
}

func controllerKind(arm string) string {
	bounds, _ := substrate.MachineBoundsFor(arm)
	return bounds.ControllerKind
}

func featureRefusal(err error) (string, string) {
	diagnostics := diagnostics.Of(err)
	if len(diagnostics) != 0 {
		return diagnostics[0].Message, diagnostics[0].Remediation
	}
	return "the installation feature could not be derived", "correct the cluster installation selection"
}

func stateReadSeconds(arm string) int {
	bounds, _ := substrate.MachineBoundsFor(arm)
	return int(bounds.StateRead.Seconds())
}
