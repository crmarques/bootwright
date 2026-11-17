package controller

import (
	"fmt"
	"net/netip"
	"slices"
	"strconv"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/containercluster"
	"github.com/crmarques/bootwright/internal/controller/evidence"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/infrastructureservices"
	machineref "github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/managedos"
	"github.com/crmarques/bootwright/internal/substrate"
)

// Selection contains only the controller prerequisites selected from effective
// desired state. It is not evidence of host identity or dependency readiness.
type Selection struct {
	environmentName  string
	machineName      string
	containerRuntime bool
	libvirtClient    bool
	hypervisor       bool
	imageMirror      bool
	installerMedia   bool
	iputils          bool
	nmstate          bool
	route            Route
	versions         DependencyVersions
}

func (s Selection) EnvironmentName() string      { return s.environmentName }
func (s Selection) MachineName() string          { return s.machineName }
func (s Selection) ContainerRuntime() bool       { return s.containerRuntime }
func (s Selection) LibvirtClient() bool          { return s.libvirtClient }
func (s Selection) Hypervisor() bool             { return s.hypervisor }
func (s Selection) ImageMirror() bool            { return s.imageMirror }
func (s Selection) IPUtils() bool                { return s.iputils }
func (s Selection) NMState() bool                { return s.nmstate }
func (s Selection) InstallerMedia() bool         { return s.installerMedia }
func (s Selection) Route() Route                 { return s.route }
func (s Selection) Versions() DependencyVersions { return s.versions }

// HasStage reports whether a context selecting this graph has a controller
// stage: only that stage installs a target client, the container runtime, the
// libvirt client, the hypervisor closure or the installer-media tooling, and a
// context selecting none of them has nothing for it to run.
func HasStage(selection Selection, tools []evidence.ToolRequest) bool {
	return len(tools) != 0 || selection.ContainerRuntime() || selection.LibvirtClient() || selection.Hypervisor() || selection.InstallerMedia() || selection.NMState() || selection.IPUtils() || selection.ImageMirror()
}

// Route contains explicit setup acquisition policy without Secret references or
// material. A route grants no authority to change the host's network settings.
type Route struct {
	direct     bool
	proxyName  string
	origin     string
	httpProxy  string
	httpsProxy string
	noProxy    []string
}

func (r Route) Direct() bool       { return r.direct }
func (r Route) ProxyName() string  { return r.proxyName }
func (r Route) HTTPProxy() string  { return r.httpProxy }
func (r Route) HTTPSProxy() string { return r.httpsProxy }
func (r Route) NoProxy() []string  { return slices.Clone(r.noProxy) }

// Baseline selects context-free prerequisites and direct acquisition. It never
// reads the invoking user's current context or ambient configuration.
func Baseline() Selection {
	return Selection{route: Route{direct: true}, versions: DefaultDependencyVersions()}
}

// WithAmbientRoute substitutes the acquisition route of a selection no context
// made. A selection that named a controller Machine keeps that Machine's proxy
// choice, so one context can never acquire over two different routes.
func (s Selection) WithAmbientRoute(route Route) Selection {
	if s.machineName != "" || !route.Configured() {
		return s
	}
	s.route = route
	return s
}

// Select consumes an admitted, normalized effective catalog. Only the selected
// controller's requirements affect setup; unrelated lifecycle kinds add no
// prerequisites and do not establish or prevent their own readiness.
func Select(catalog api.Catalog) (Selection, error) {
	environments := catalog.OfKind(api.Environment)
	if len(environments) != 1 {
		return Selection{}, refuseSelection(api.Object{}, "api.invariant", "", "controller selection requires exactly one effective Environment",
			"declare exactly one Environment in the context input")
	}
	environment := environments[0]
	if !api.ValidLexical(api.LexicalName, environment.Name()) {
		return Selection{}, refuseSelection(api.Object{}, "api.value", "$.metadata.name", "effective Environment requires a valid name",
			"set metadata.name of the Environment to a lowercase DNS label")
	}
	ref := environment.Spec().Get("controller", "machineRef")
	if ref.Type() != api.String || !api.ValidLexical(api.LexicalName, ref.Text()) {
		return Selection{}, refuseSelection(environment, "api.reference", "$.spec.controller.machineRef", "controller requires an explicit scalar Machine name",
			"set spec.controller.machineRef on "+environment.Identity()+" to the name of the controller Machine")
	}
	machine, found := catalog.Find(api.Machine, ref.Text())
	if !found {
		return Selection{}, refuseSelection(environment, "api.reference", "$.spec.controller.machineRef", "controller reference must resolve to exactly one Machine",
			"declare the Machine spec.controller.machineRef names on "+environment.Identity()+", or name a declared one")
	}
	if !machineref.OSProvided(machine) {
		return Selection{}, refuseSelection(machine, "api.invariant", "$.spec.os.provided", "controller setup requires an OS-ready Machine with os.provided: true",
			"set spec.os.provided: true on "+machine.Identity())
	}
	if !machineref.ReachedOnlyLocally(machine) {
		return Selection{}, refuseSelection(machine, "api.invariant", "$.spec.access", "controller setup requires access.local: true and no SSH access",
			"set spec.access.local: true and remove spec.access.ssh on "+machine.Identity())
	}
	// The executable's own limits refuse before anything is selected, exactly
	// as the plan reports them, so both name the same field and remedy.
	if shapes := unsupported(machine, catalog); len(shapes) != 0 {
		shape := shapes[0]
		return Selection{}, refuseSelection(shape.Object, "controller.unsupported", shape.Field, shape.Reason, shape.Remediation)
	}
	route, err := selectRoute(machine, catalog)
	if err != nil {
		return Selection{}, err
	}
	versions, err := selectDependencyVersions(environment)
	if err != nil {
		return Selection{}, err
	}
	return Selection{
		environmentName: environment.Name(), machineName: machine.Name(),
		containerRuntime: requiresContainerRuntime(catalog, machine), libvirtClient: requiresLibvirtClient(catalog, machine),
		hypervisor: requiresHypervisor(catalog, machine), installerMedia: requiresInstallerMedia(catalog, machine), imageMirror: requiresImageMirror(catalog, machine),
		route: route, versions: versions, nmstate: requiresNMState(catalog), iputils: requiresIPUtils(catalog, machine.Name()),
	}, nil
}

// requiresContainerRuntime reports whether this Machine runs a container: a
// managed service placed here, or the emulated BMC a libvirt provider hosted
// here runs beside its guests. Nothing else on the controller needs podman.
func requiresContainerRuntime(catalog api.Catalog, controllerMachine api.Object) bool {
	return requiresHypervisor(catalog, controllerMachine) || infrastructureservices.HostsManagedService(catalog, controllerMachine.Name())
}

// requiresHypervisor reports whether a libvirt provider runs its guests on this
// Machine. The closure is the provider host's runtime, so only the Machine that
// hosts one selects it; a provider reached over SSH installs its own.
func requiresHypervisor(catalog api.Catalog, controllerMachine api.Object) bool {
	for _, provider := range catalog.OfKind(api.InfraProvider) {
		if substrate.HostMachine(provider) == controllerMachine.Name() {
			return true
		}
	}
	return false
}

// requiresInstallerMedia reports whether an Anaconda installation builds its
// image on this Machine. The tooling runs where the selected artifact server is
// placed, because that is where the installation publishes what it builds.
func requiresInstallerMedia(catalog api.Catalog, controllerMachine api.Object) bool {
	return slices.Contains(managedos.InstallerMediaHosts(catalog), controllerMachine.Name())
}

func requiresImageMirror(catalog api.Catalog, controllerMachine api.Object) bool {
	return infrastructureservices.ImageMirrorHost(catalog) == controllerMachine.Name()
}

// requiresLibvirtClient reports whether this Machine needs virsh. A provider
// hosted here selects it too: its host block proves the client with the
// hypervisor closure, and the daemon packages need not pull the client in.
func requiresLibvirtClient(catalog api.Catalog, controllerMachine api.Object) bool {
	if requiresHypervisor(catalog, controllerMachine) {
		return true
	}
	for _, machine := range catalog.Machines() {
		provider, found := catalog.Find(api.InfraProvider, machine.ProviderRef)
		if found && substrate.NeedsHypervisorClient(provider) {
			return true
		}
	}
	return false
}

// selectRoute builds the controller Machine's route and proves it in the one
// grammar every acquisition route shares, naming the field that breaks it. The
// executable's own limits on that route are its unsupported shapes, which
// Select refuses before it gets here.
func selectRoute(machine api.Object, catalog api.Catalog) (Route, error) {
	choice := machineref.ProxyChoice(machine)
	if choice.Type() != api.Mapping {
		return Route{}, refuseSelection(machine, "api.value", "$.spec.proxy", "controller setup requires a normalized proxy choice",
			"set spec.proxy on "+machine.Identity()+" to direct: {} or a proxyRef")
	}
	if issues := infrastructureservices.ValidateProxy(choice, catalog, "$.spec.proxy", false); len(issues) != 0 {
		issue := issues[0]
		remediation := issue.Remediation
		if remediation == "" {
			remediation = "correct " + strings.TrimPrefix(issue.Field, "$.") + " on " + machine.Identity()
		}
		return Route{}, refuseSelection(machine, issue.Code, issue.Field, issue.Message, remediation)
	}
	if direct := choice.Get("direct"); direct.Present() {
		if direct.Type() != api.Mapping || direct.Len() != 0 {
			return Route{}, refuseSelection(machine, "api.value", "$.spec.proxy.direct", "direct acquisition requires an empty object",
				"write spec.proxy.direct on "+machine.Identity()+" as {}")
		}
		return Route{direct: true}, nil
	}
	ref := choice.Get("proxyRef")
	if ref.Type() != api.String || !api.ValidLexical(api.LexicalName, ref.Text()) {
		return Route{}, refuseSelection(machine, "api.reference", "$.spec.proxy.proxyRef", "proxy reference requires an explicit scalar Proxy name",
			"set spec.proxy.proxyRef on "+machine.Identity()+" to the name of a Proxy")
	}
	proxy, found := catalog.Find(api.Proxy, ref.Text())
	if !found {
		return Route{}, refuseSelection(machine, "api.reference", "$.spec.proxy.proxyRef", "proxy reference must resolve to exactly one Proxy",
			"declare the Proxy spec.proxy.proxyRef names on "+machine.Identity()+", or name a declared one")
	}
	facts := infrastructureservices.ProxyFactsOf(proxy)
	route := Route{proxyName: proxy.Name()}
	for _, entry := range []struct {
		field  string
		value  api.Value
		target *string
	}{{"httpProxy", facts.HTTPProxy, &route.httpProxy}, {"httpsProxy", facts.HTTPSProxy, &route.httpsProxy}} {
		value := entry.value
		if !value.Present() {
			continue
		}
		if value.Type() != api.String || !api.ValidLexical(api.LexicalHTTPURL, value.Text()) || !api.ValidLexical(api.LexicalProxyEndpoint, value.Text()) {
			return Route{}, refuseSelection(proxy, "api.value", "$.spec.connection."+entry.field,
				"the controller route requires spec.connection."+entry.field+" to be a bare HTTP or HTTPS proxy endpoint",
				"set spec.connection."+entry.field+" on "+proxy.Identity()+" to "+api.ProxyEndpointForm)
		}
		*entry.target = value.Text()
	}
	if route.httpProxy == "" && route.httpsProxy == "" {
		return Route{}, refuseSelection(proxy, "api.value", "$.spec.connection", "external Proxy requires at least one HTTP(S) proxy URL",
			"set spec.connection.httpsProxy on "+proxy.Identity()+" to the proxy endpoint")
	}
	if noProxy := choice.Get("noProxy"); noProxy.Present() {
		if noProxy.Type() != api.Sequence {
			return Route{}, refuseSelection(machine, "api.value", "$.spec.proxy.noProxy", "proxy bypass entries must be a sequence",
				"write spec.proxy.noProxy on "+machine.Identity()+" as a list of bypass entries")
		}
		seen := map[string]bool{}
		for index, value := range noProxy.Items() {
			field := fmt.Sprintf("$.spec.proxy.noProxy[%d]", index)
			if value.Type() != api.String || value.Text() == "" || seen[value.Text()] {
				return Route{}, refuseSelection(machine, "api.value", field, "proxy bypass entries must be distinct nonempty strings",
					"remove the empty or repeated "+strings.TrimPrefix(field, "$.")+" from "+machine.Identity())
			}
			if !api.ValidLexical(api.LexicalProxyBypass, value.Text()) {
				return Route{}, refuseSelection(machine, "api.value", field, "the controller route requires each spec.proxy.noProxy entry to be a proxy bypass entry",
					"write "+strings.TrimPrefix(field, "$.")+" on "+machine.Identity()+" as "+api.ProxyBypassForms)
			}
			seen[value.Text()] = true
			route.noProxy = append(route.noProxy, value.Text())
		}
	}
	// Every field is proved above, so the selector this route builds is the
	// backstop that the proofs and the route's own grammar never disagree.
	if _, err := route.Selector(); err != nil {
		return Route{}, refuseSelection(machine, "api.invariant", "$.spec.proxy", "the controller route could not be built from its proved proxy choice",
			"use a compatible executable")
	}
	return route, nil
}

// UnsupportedShape is one controller shape the API admits but this executable
// cannot realize: the object and field that declare it, why, and what the
// operator changes.
type UnsupportedShape struct {
	Object      api.Object
	Field       string
	Reason      string
	Remediation string
}

// Unsupported lists every controller shape of a selected graph this
// executable cannot realize, in the order Select meets them. It is pure and
// reads no host, so a plan refuses them before registration, status reports
// them, and Select refuses the first of them, all with the same words.
func Unsupported(catalog api.Catalog) []UnsupportedShape {
	environments := catalog.OfKind(api.Environment)
	if len(environments) != 1 {
		return nil
	}
	ref := environments[0].Spec().Get("controller", "machineRef")
	machine, found := catalog.Find(api.Machine, ref.Text())
	if ref.Type() != api.String || !found {
		return nil
	}
	return unsupported(machine, catalog)
}

func unsupported(machine api.Object, catalog api.Catalog) []UnsupportedShape {
	shapes := []UnsupportedShape{}
	if environments := catalog.OfKind(api.Environment); len(environments) == 1 && requiresLibvirtClient(catalog, machine) {
		if versions, err := selectDependencyVersions(environments[0]); err == nil && versions.Libvirt != DefaultDependencyVersions().Libvirt {
			shapes = append(shapes, UnsupportedShape{Object: environments[0], Field: "$.spec.dependencyVersions.libvirt",
				Reason:      "this executable's controller stage installs the libvirt release the host's repositories provide, and " + environments[0].Identity() + " pins spec.dependencyVersions.libvirt",
				Remediation: "remove spec.dependencyVersions.libvirt from " + environments[0].Identity()})
		}
	}
	if environments := catalog.OfKind(api.Environment); len(environments) == 1 {
		shapes = append(shapes, releasesBelowMinimum(environments[0], catalog)...)
		shapes = append(shapes, mirrorShapes(environments[0], catalog)...)
	}
	choice := machineref.ProxyChoice(machine)
	if bypass := choice.Get("noProxy"); bypass.Type() == api.Sequence && bypass.Len() > maxProxyBypassEntries {
		shapes = append(shapes, UnsupportedShape{Object: machine, Field: "$.spec.proxy.noProxy",
			Reason:      "this executable's controller route carries at most " + strconv.Itoa(maxProxyBypassEntries) + " bypass entries, and " + machine.Identity() + " declares more in spec.proxy.noProxy",
			Remediation: "keep at most " + strconv.Itoa(maxProxyBypassEntries) + " entries in spec.proxy.noProxy on " + machine.Identity()})
	}
	ref := choice.Get("proxyRef")
	proxy, found := catalog.Find(api.Proxy, ref.Text())
	if ref.Type() != api.String || !found {
		return shapes
	}
	facts := infrastructureservices.ProxyFactsOf(proxy)
	if facts.Management != "external" {
		return append(shapes, UnsupportedShape{Object: machine, Field: "$.spec.proxy.proxyRef",
			Reason:      "this executable's controller stage acquires only directly or through an external Proxy that is already ready, and " + machine.Identity() + " selects the managed " + proxy.Identity(),
			Remediation: "select direct: {} or an external Proxy in spec.proxy on " + machine.Identity()})
	}
	if facts.Authenticated {
		shapes = append(shapes, UnsupportedShape{Object: proxy, Field: "$.spec.connection.auth.proxyAuthRef",
			Reason:      "this executable's controller stage acquires through no authenticated proxy, and " + proxy.Identity() + " sets spec.connection.auth.proxyAuthRef",
			Remediation: "select direct: {} or an external Proxy that needs no authentication in spec.proxy on " + machine.Identity()})
	}
	if facts.PrivateTrusted {
		shapes = append(shapes, UnsupportedShape{Object: proxy, Field: "$.spec.connection.trustBundleRef",
			Reason:      "this executable's controller stage trusts only the host's system trust store, and " + proxy.Identity() + " sets spec.connection.trustBundleRef",
			Remediation: "select direct: {} or an external Proxy without spec.connection.trustBundleRef that the host's system trust store verifies in spec.proxy on " + machine.Identity()})
	}
	if facts.HTTPProxy.Present() && !facts.HTTPSProxy.Present() {
		shapes = append(shapes, UnsupportedShape{Object: proxy, Field: "$.spec.connection.httpsProxy",
			Reason:      "every dependency source is HTTPS, so the spec.connection.httpProxy of " + proxy.Identity() + " alone selects no controller acquisition route",
			Remediation: "set connection.httpsProxy on " + proxy.Identity() + " to the proxy endpoint; every dependency source is HTTPS"})
	}
	return shapes
}

// selectionFailure is a refusal from a selection helper that names no remedy
// of its own: the field it names is the one to correct, and a refusal no
// declared field settles is an invariant of this executable.
func selectionFailure(object api.Object, code, field, message string) error {
	remediation := "use a compatible executable"
	if object.Kind() != "" && field != "" {
		remediation = "correct " + strings.TrimPrefix(field, "$.") + " on " + object.Identity()
	}
	return refuseSelection(object, code, field, message, remediation)
}

// refuseSelection is a selection refusal on the object and field that declare
// it, with the action that settles it. It never echoes an authored value.
func refuseSelection(object api.Object, code, field, message, remediation string) error {
	diagnostic := diagnostics.Diagnostic{Severity: "error", Code: code, Field: field, Message: message, Remediation: remediation}
	if object.Kind() != "" {
		diagnostic.Object = &diagnostics.ObjectIdentity{APIVersion: api.APIVersion, Kind: string(object.Kind()), Name: object.Name()}
	}
	return &diagnostics.Failure{Diagnostics: []diagnostics.Diagnostic{diagnostic}}
}

func requiresNMState(catalog api.Catalog) bool {
	for _, cluster := range catalog.OfCategory(api.ContainerClusterCategory) {
		if !api.ClusterInstallsByAgent(cluster) {
			continue
		}
		for _, name := range api.ClusterNodeMachines(cluster) {
			if _, found := catalog.Find(api.Machine, name); found {
				return true
			}
		}
	}
	return false
}

func requiresIPUtils(catalog api.Catalog, controllerMachine string) bool {
	for _, cluster := range catalog.OfCategory(api.ContainerClusterCategory) {
		if !api.ClusterInstallsByAgent(cluster) {
			continue
		}
		facts, _ := containercluster.FactsOf(cluster.Kind())
		for _, vip := range facts.VirtualIPs(catalog, cluster, controllerMachine) {
			address, err := netip.ParseAddr(vip.Address)
			if err == nil && address.Is4() && vip.Bridge != "" {
				return true
			}
		}
	}
	return false
}

func releasesBelowMinimum(environment api.Object, catalog api.Catalog) []UnsupportedShape {
	tools, err := SelectTools(catalog)
	if err != nil {
		return nil
	}
	shapes, seen := []UnsupportedShape{}, map[string]bool{}
	for _, tool := range tools {
		pin, found, err := PinOf(tool.Kind)
		if err != nil || !found || seen[tool.Kind] {
			continue
		}
		seen[tool.Kind] = true
		order, comparable := CompareReleases(tool.Version, pin.Minimum)
		if comparable && order >= 0 {
			continue
		}
		field := "spec.dependencyVersions." + tool.Kind
		declared := environment.Spec().Get("dependencyVersions", tool.Kind).Text()
		reason := "this executable accepts " + tool.Kind + " " + pin.Minimum + " or newer, and " + environment.Identity() + " declares " + declared + " in " + field
		if !comparable {
			reason = "this executable holds " + tool.Kind + " to " + pin.Minimum + " or newer by its MAJOR.MINOR.PATCH, and " + environment.Identity() + " declares " + declared + " in " + field + ", which names none"
		}
		shapes = append(shapes, UnsupportedShape{Object: environment, Field: "$." + field, Reason: reason,
			Remediation: "declare " + tool.Kind + " " + pin.Minimum + " or newer in " + field + " of " + environment.Identity() + ", or remove it for this build's default"})
	}
	return shapes
}

func mirrorShapes(environment api.Object, catalog api.Catalog) []UnsupportedShape {
	shapes := []UnsupportedShape{}
	for _, mirror := range infrastructureservices.ManagedMirrors(catalog) {
		shapes = append(shapes, managedMirrorShapes(environment, catalog, mirror)...)
	}
	if !environment.Spec().Get("mirror", "artifacts").Present() {
		return shapes
	}
	tools, err := SelectTools(catalog)
	if err != nil {
		return shapes
	}
	seen := map[string]bool{}
	for _, tool := range tools {
		key := tool.Kind
		if tool.Compatibility == "okd" {
			key = "okd"
		}
		if !PublicOrigin(tool) || tool.SHA256 != "" || seen[key] {
			continue
		}
		seen[key] = true
		reason := "with spec.mirror.artifacts, this executable acquires a public-origin client only at a release its tool table pins, and " + environment.Identity() + " selects " + tool.Kind + " " + strings.TrimPrefix(tool.Version, "v") + ", whose checksum only its publisher holds"
		remedy := "declare no " + tool.Kind + " release in spec.dependencyVersions of " + environment.Identity() + " so it takes this build's pinned default, or remove spec.mirror.artifacts"
		if tool.Compatibility == "okd" {
			remedy = "select an openshift distribution, whose clients come from their vendor, or remove spec.mirror.artifacts from " + environment.Identity()
		}
		shapes = append(shapes, UnsupportedShape{Object: environment, Field: "$.spec.mirror.artifacts", Reason: reason, Remediation: remedy})
	}
	return shapes
}

type mirrorWords struct {
	noun, fill, sources, waitsOn string
	providerRemedy               func(service, environment string) string
}

func mirrorWordsOf(kind api.Kind) mirrorWords {
	if kind == api.Registry {
		return mirrorWords{noun: "image mirror", fill: "the services that pull from it", sources: "origins", waitsOn: "the emulated BMCs that pull from it",
			providerRemedy: func(service, environment string) string {
				return "place " + service + " on a Machine no InfraProvider hosts or bridges, or select an external Registry in spec.mirror.registry.registryRef of " + environment
			}}
	}
	return mirrorWords{noun: "artifact mirror", fill: "any managed service runs", sources: "publishers", waitsOn: "the controller stage",
		providerRemedy: func(service, environment string) string {
			return "place " + service + " on a Machine that hosts no InfraProvider and whose address no provider bridges, or select an artifact mirror the operator fills in spec.mirror.artifacts.url of " + environment
		}}
}

func managedMirrorShapes(environment api.Object, catalog api.Catalog, mirror infrastructureservices.ManagedMirror) []UnsupportedShape {
	refused := func(refusal infrastructureservices.MirrorRefusal) []UnsupportedShape {
		return []UnsupportedShape{{Object: environment, Field: mirror.Field, Reason: refusal.Reason, Remediation: refusal.Remediation}}
	}
	if mirror.Conflict != nil {
		return refused(*mirror.Conflict)
	}
	words, service, host := mirrorWordsOf(mirror.Service.Kind()), mirror.Service.Identity(), mirror.Host
	controllerName := environment.Spec().Get("controller", "machineRef").Text()
	machine, found := catalog.Find(api.Machine, host)
	if host != controllerName && found && machineref.NamesControllerHost(machine, catalog, controllerName) {
		return refused(infrastructureservices.MirrorRefusal{
			Reason:      "this executable places a managed " + words.noun + " on the controller host only through its controller Machine, and " + service + " is placed on Machine/" + host + ", another name for that host",
			Remediation: "place " + service + " on Machine/" + controllerName + " or on another Machine reached over SSH"})
	}
	if mirror.Unlocated != nil {
		return refused(*mirror.Unlocated)
	}
	if !found {
		return nil
	}
	if proxy, found := catalog.Find(api.Proxy, machineref.ProxyChoice(machine).Get("proxyRef").Text()); found && infrastructureservices.ProxyFactsOf(proxy).Management == "managed" {
		return []UnsupportedShape{{Object: machine, Field: "$.spec.proxy.proxyRef",
			Reason:      "the " + words.noun + " on " + machine.Identity() + " is filled before " + words.fill + ", and " + machine.Identity() + " reaches the " + words.sources + " through the managed " + proxy.Identity(),
			Remediation: "select direct: {} or an external Proxy in spec.proxy on " + machine.Identity()}}
	}
	if providers := mirrorProviders(catalog, mirror, host == controllerName); len(providers) != 0 {
		return refused(infrastructureservices.MirrorRefusal{
			Reason: "the " + words.noun + " is filled before " + words.waitsOn + ", and " + service + " on " + machine.Identity() +
				" waits for InfraProvider/" + providers[0] + ", which that Machine hosts or whose bridge carries its address",
			Remediation: words.providerRemedy(service, environment.Identity())})
	}
	return nil
}

func mirrorProviders(catalog api.Catalog, mirror infrastructureservices.ManagedMirror, local bool) []string {
	var providers []string
	if !local {
		providers = substrate.ProvidersHostedOn(catalog, mirror.Host)
	}
	for _, address := range mirror.Addresses {
		providers = append(providers, substrate.BridgeProviders(catalog, mirror.Host, address)...)
	}
	slices.Sort(providers)
	return slices.Compact(providers)
}
