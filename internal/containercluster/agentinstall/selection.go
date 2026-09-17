package agentinstall

import (
	"net/netip"
	"slices"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/infrastructureservices/artifactserver"
	"github.com/crmarques/bootwright/internal/infrastructureservices/managedservice"
	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/substrate"
)

// unsupportedSelections are the cluster choices that carry effects this
// contract does not prove. A selected one refuses before registration rather
// than installing part of what was declared.
var unsupportedSelections = [][]string{
	{"security", "diskEncryption"},
	{"install", "servingCertificates"},
	{"install", "registries"},
}

// Requirements names the API objects one cluster's blocks wait for, so those
// blocks complete before the installer needs what they realize.
type Requirements struct {
	ArtifactServers []string
	DNSServers      []string
	Machines        []string
	NTPServers      []string
}

// Unsupported lists every selected cluster this contract cannot install, in
// canonical order, so an operation refuses before it registers anything.
func Unsupported(catalog api.Catalog) []string {
	var found []string
	for _, cluster := range catalog.OfKind(api.ContainerCluster) {
		if reason := unsupportedReason(catalog, cluster); reason != "" {
			found = append(found, cluster.Identity())
		}
	}
	slices.Sort(found)
	return slices.Compact(found)
}

// unsupportedReason says why one cluster is not installable, or nothing when
// it is. It is the one place the supported shape is stated, so the refusal and
// the derivation can never disagree about what this contract installs.
func unsupportedReason(catalog api.Catalog, cluster api.Object) string {
	spec := cluster.Spec()
	if spec.Get("distribution", "type").Text() != "openshift" {
		return "this executable installs no OKD cluster"
	}
	if spec.Get("distribution", "release", "version").Text() == "" {
		return "a cluster pinned to a release image alone names no version for its installer to match"
	}
	if method := spec.Get("install", "method").Text(); method != "" && method != "agent" {
		return "this executable installs no cluster by the declared method"
	}
	if mode := spec.Get("install", "mode").Text(); mode != "" && mode != "connected" {
		return "this executable installs no disconnected cluster"
	}
	if spec.Get("security", "fips", "enabled").Bool() {
		return "a FIPS cluster needs an installer this executable does not publish"
	}
	if !spec.Get("install", "proxy").Has("direct") {
		return "this executable installs no cluster through a proxy"
	}
	for _, path := range unsupportedSelections {
		if spec.Has(path...) {
			return "this executable does not install " + strings.Join(path, ".")
		}
	}
	controllerMachine := controllerMachineName(catalog)
	for _, node := range spec.Get("nodes").Items() {
		bound, found := catalog.Find(api.Machine, node.Get("machineRef").Text())
		if !found {
			continue
		}
		target, err := substrate.TargetFor(catalog, bound, cluster.Name(), controllerMachine)
		if err != nil {
			return "a declared node is on a substrate this executable does not realize"
		}
		if target.Controller.VirtualMedia.Trust == substrate.TrustImportCertificate {
			return "importing a certificate into a management controller is not implemented"
		}
	}
	return ""
}

// controllerMachineName is the Machine this Environment selects as its
// controller. Refusal reads it from the graph, because it decides what it can
// realize before an operation hands it that name.
func controllerMachineName(catalog api.Catalog) string {
	environments := catalog.OfKind(api.Environment)
	if len(environments) != 1 {
		return ""
	}
	return environments[0].Spec().Get("controller", "machineRef").Text()
}

// Requests derives one media and one install request per selected cluster, in
// canonical name order. It reads no host, endpoint or Secret material.
func Requests(catalog api.Catalog, controllerMachine, contextName string) ([]MediaRequest, []InstallRequest, []Requirements, error) {
	var media []MediaRequest
	var installs []InstallRequest
	var requirements []Requirements
	clusters := slices.Clone(catalog.OfKind(api.ContainerCluster))
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
		return MediaRequest{}, InstallRequest{}, Requirements{}, err
	}
	if !substrate.SafeSegment(name) {
		return empty(refusal("lifecycle.state", "the cluster name is not a safe host identifier", "rename "+cluster.Identity()))
	}
	if reason := unsupportedReason(catalog, cluster); reason != "" {
		return empty(refusal("lifecycle.state", reason, "correct "+cluster.Identity()))
	}
	needs := Requirements{}
	nodes, err := nodeProjections(catalog, cluster, contextName, controllerMachine, &needs)
	if err != nil {
		return empty(err)
	}
	selection := cluster.Spec().Get("install", "agent", "redfishVirtualMedia", "artifactServerEndpoint")
	server, err := artifactserver.Selected(catalog, selection, cluster.Identity())
	if err != nil {
		return empty(err)
	}
	published, _, err := artifactserver.PrivatePath(catalog, server, selection, contextName, consumerPrefix, name, cluster.Identity())
	if err != nil {
		return empty(err)
	}
	placement, err := artifactserver.PlacementFor(catalog, server, controllerMachine)
	if err != nil {
		return empty(err)
	}
	// The image is built by the installer the controller stage published, which
	// that stage installs on the controller alone. A server placed on another
	// host would have its image built where no installer exists, so the
	// placement is refused here rather than discovered at execution.
	if !placement.Local() {
		return empty(refusal("lifecycle.state",
			"a cluster's boot image is built on the controller, so the server it is published through is placed there",
			"place "+server.Identity()+" on the controller Machine"))
	}
	needs.ArtifactServers = append(needs.ArtifactServers, server.Name())
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
	image := Publication{Path: published.Path, URL: published.URL}
	identity := func(block string) Identity {
		return Identity{Block: block, Cluster: name, Context: contextName}
	}
	pullSecret := cluster.Spec().Get("install", "pullSecretRef").Text()
	if pullSecret == "" {
		return empty(refusal("api.required", "the cluster names no pull secret", "set spec.install.pullSecretRef on "+cluster.Identity()))
	}
	sshKey, err := clusterKeyRef(cluster)
	if err != nil {
		return empty(err)
	}
	needs.ArtifactServers = sortedUnique(needs.ArtifactServers)
	needs.DNSServers, needs.NTPServers = sortedUnique(needs.DNSServers), sortedUnique(needs.NTPServers)
	needs.Machines = sortedUnique(needs.Machines)
	mediaRequest := MediaRequest{
		AgentConfig: agent, Identity: identity(MediaBlockID(name)), Image: image,
		InstallConfig: install, Placement: placement, PullSecretRef: pullSecret,
		Release: release, SSHKeyRef: sshKey, Tool: tool,
		TrustBundleRefs: cluster.Spec().Get("install", "additionalTrustBundleRefs").Strings(),
		Version:         mediaRequestVersion, WorkRoot: WorkRoot(contextName, name),
	}
	installRequest := InstallRequest{
		Budgets: DefaultBudgets(), Endpoints: endpointNames(catalog, cluster),
		Identity: identity(InstallBlockID(name)), Image: image, Nodes: frozenNodes(nodes),
		Placement: placement, Release: release, Tool: tool,
		Version: installRequestVersion, WorkRoot: WorkRoot(contextName, name),
	}
	if len(installRequest.Endpoints) == 0 {
		return empty(refusal("api.required", "the cluster resolves no endpoint address for its controller to reach",
			"declare spec.install.endpoints on "+cluster.Identity()))
	}
	return mediaRequest, installRequest, needs, nil
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
	return "", refusal("api.required", "the cluster names no administration key for its nodes",
		"set spec.install.nodeSSH on "+cluster.Identity())
}

// nodeProjection is one declared node with everything both installer inputs
// and the boot itself need, derived once.
type nodeProjection struct {
	address    string
	interfaces []substrate.Interface
	machine    api.Object
	name       string
	network    map[string]any
	role       string
	rootDevice string
	target     substrate.Target
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
			rootDevice: target.RootDevice, target: target,
		})
	}
	if len(projections) == 0 {
		return nil, refusal("api.required", "the cluster declares no node", "declare spec.nodes on "+cluster.Identity())
	}
	return projections, nil
}

// frozenNodes is what the install block freezes about each node: the machine
// to boot, the controller to boot it through, and the address it answers at.
func frozenNodes(nodes []nodeProjection) []Node {
	frozen := make([]Node, 0, len(nodes))
	for _, node := range nodes {
		frozen = append(frozen, Node{
			Address: node.address,
			Controller: Controller{
				CredentialsRef: node.target.Controller.CredentialsRef,
				Endpoint:       node.target.Controller.Endpoint,
				TLSVerify:      node.target.Controller.TLSVerify,
				VirtualMedia: VirtualMedia{
					RemoveCertificate:   node.target.Controller.VirtualMedia.RemoveCertificate,
					RestoreVerification: node.target.Controller.VirtualMedia.RestoreVerification,
					Trust:               node.target.Controller.VirtualMedia.Trust,
				},
			},
			Machine: node.machine.Name(), Name: node.name,
			Physical: node.target.Physical, Substrate: node.target.Substrate,
		})
	}
	return frozen
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
// own network configuration's selections, and records each as a requirement.
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
		needs.DNSServers = append(needs.DNSServers, name)
		if !slices.Contains(addresses, address) {
			addresses = append(addresses, address)
		}
	}
	return addresses, nil
}

// timeAddresses derives the time sources the cluster installs with, from its
// own selections, and records each as a requirement.
func timeAddresses(catalog api.Catalog, cluster api.Object, needs *Requirements) ([]string, error) {
	var addresses []string
	for _, selection := range cluster.Spec().Get("install", "ntp").Items() {
		address, name, err := managedservice.ServiceEndpoint(catalog, api.NTPServer, selection, cluster.Identity())
		if err != nil {
			return nil, err
		}
		needs.NTPServers = append(needs.NTPServers, name)
		if !slices.Contains(addresses, address) {
			addresses = append(addresses, address)
		}
	}
	return addresses, nil
}

func refusal(code, message, remediation string) error {
	return diagnostics.NewFailureWithRemediation(code, message, "", remediation)
}
