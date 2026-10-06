package controller

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/infrastructureservices"
)

// Selection contains only the controller prerequisites selected from effective
// desired state. It is not evidence of host identity or dependency readiness.
type Selection struct {
	environmentName  string
	machineName      string
	containerRuntime bool
	libvirtClient    bool
	hypervisor       bool
	installerMedia   bool
	route            Route
	versions         DependencyVersions
}

func (s Selection) EnvironmentName() string      { return s.environmentName }
func (s Selection) MachineName() string          { return s.machineName }
func (s Selection) ContainerRuntime() bool       { return s.containerRuntime }
func (s Selection) LibvirtClient() bool          { return s.libvirtClient }
func (s Selection) Hypervisor() bool             { return s.hypervisor }
func (s Selection) InstallerMedia() bool         { return s.installerMedia }
func (s Selection) Route() Route                 { return s.route }
func (s Selection) Versions() DependencyVersions { return s.versions }

// HasStage reports whether a context selecting this graph has a controller
// stage: only that stage installs a target client, the libvirt client, the
// hypervisor closure or the installer-media tooling, and a context selecting
// none of them has nothing for it to run.
func HasStage(selection Selection, tools []ToolRequest) bool {
	return len(tools) != 0 || selection.LibvirtClient() || selection.Hypervisor() || selection.InstallerMedia()
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
	return Selection{containerRuntime: true, route: Route{direct: true}, versions: DefaultDependencyVersions()}
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
	if !api.ValidLexical("name", environment.Name()) {
		return Selection{}, refuseSelection(api.Object{}, "api.value", "$.metadata.name", "effective Environment requires a valid name",
			"set metadata.name of the Environment to a lowercase DNS label")
	}
	ref := environment.Spec().Get("controller", "machineRef")
	if ref.Type() != api.String || !api.ValidLexical("name", ref.Text()) {
		return Selection{}, refuseSelection(environment, "api.reference", "$.spec.controller.machineRef", "controller requires an explicit scalar Machine name",
			"set spec.controller.machineRef on "+environment.Identity()+" to the name of the controller Machine")
	}
	machine, found := catalog.Find(api.Machine, ref.Text())
	if !found {
		return Selection{}, refuseSelection(environment, "api.reference", "$.spec.controller.machineRef", "controller reference must resolve to exactly one Machine",
			"declare the Machine spec.controller.machineRef names on "+environment.Identity()+", or name a declared one")
	}
	if provided := machine.Spec().Get("os", "provided"); provided.Type() != api.Boolean || !provided.Bool() {
		return Selection{}, refuseSelection(machine, "api.invariant", "$.spec.os.provided", "controller setup requires an OS-ready Machine with os.provided: true",
			"set spec.os.provided: true on "+machine.Identity())
	}
	if local := machine.Spec().Get("access", "local"); local.Type() != api.Boolean || !local.Bool() || machine.Spec().Has("access", "ssh") {
		return Selection{}, refuseSelection(machine, "api.invariant", "$.spec.access", "controller setup requires access.local: true and no SSH access",
			"set spec.access.local: true and remove spec.access.ssh on "+machine.Identity())
	}
	// The executable's own limits refuse before anything is selected, exactly
	// as the plan reports them, so both name the same field and remedy.
	if shapes := unsupported(machine, catalog); len(shapes) != 0 {
		shape := shapes[0]
		return Selection{}, refuseSelection(shape.Object, "controller.unsupported", shape.Field, shape.Reason, shape.Remediation)
	}
	containerRuntime, err := selectCapabilities(machine)
	if err != nil {
		return Selection{}, err
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
		containerRuntime: containerRuntime, libvirtClient: requiresLibvirtClient(catalog, machine),
		hypervisor: requiresHypervisor(catalog, machine), installerMedia: requiresInstallerMedia(catalog, machine),
		route: route, versions: versions,
	}, nil
}

// requiresHypervisor reports whether a libvirt provider runs its guests on this
// Machine. The closure is the provider host's runtime, so only the Machine that
// hosts one selects it; a provider reached over SSH installs its own.
func requiresHypervisor(catalog api.Catalog, controllerMachine api.Object) bool {
	for _, provider := range catalog.OfKind(api.InfraProvider) {
		if !provider.Spec().Has("libvirt") {
			continue
		}
		if provider.Spec().Get("libvirt", "machineRef").Text() == controllerMachine.Name() {
			return true
		}
	}
	return false
}

// requiresInstallerMedia reports whether an Anaconda installation builds its
// image on this Machine. The tooling runs where the selected artifact server is
// placed, because that is where the installation publishes what it builds.
func requiresInstallerMedia(catalog api.Catalog, controllerMachine api.Object) bool {
	for _, machine := range catalog.OfKind(api.Machine) {
		provided := machine.Spec().Get("os", "provided")
		if provided.Type() != api.Boolean || provided.Bool() {
			continue
		}
		profile, found := catalog.Find(api.MachineInstallProfile, machine.Spec().Get("os", "installProfileRef").Text())
		if !found || !profile.Spec().Has("installer", "anaconda") {
			continue
		}
		endpoint := profile.Spec().Get("installer", "anaconda", "redfishVirtualMedia", "artifactServerEndpoint")
		server, found := catalog.Find(api.ArtifactServer, endpoint.Get("serverRef").Text())
		if found && server.Spec().Get("machineRef").Text() == controllerMachine.Name() {
			return true
		}
	}
	return false
}

// requiresLibvirtClient reports whether this Machine needs virsh. A provider
// hosted here selects it too: its host block proves the client with the
// hypervisor closure, and the daemon packages need not pull the client in.
func requiresLibvirtClient(catalog api.Catalog, controllerMachine api.Object) bool {
	if slices.Contains(controllerMachine.Spec().Get("capabilities").Strings(), "libvirt") || requiresHypervisor(catalog, controllerMachine) {
		return true
	}
	for _, machine := range catalog.OfKind(api.Machine) {
		provider, found := catalog.Find(api.InfraProvider, machine.Spec().Get("substrate", "providerRef").Text())
		if found && provider.Spec().Has("libvirt") {
			return true
		}
	}
	return false
}

// selectCapabilities reads the controller's capabilities. A capability this
// executable does not prepare is one of its unsupported shapes, which Select
// refuses before it gets here.
func selectCapabilities(machine api.Object) (bool, error) {
	capabilities := machine.Spec().Get("capabilities")
	addRuntime := "add container-runtime to spec.capabilities on " + machine.Identity()
	if !capabilities.Present() {
		return false, refuseSelection(machine, "api.invariant", "$.spec.capabilities", "controller requires the container-runtime capability", addRuntime)
	}
	if capabilities.Type() != api.Sequence {
		return false, refuseSelection(machine, "api.value", "$.spec.capabilities", "controller capabilities must be a sequence",
			"write spec.capabilities on "+machine.Identity()+" as a list of capability names")
	}
	containerRuntime := false
	seen := map[string]bool{}
	for index, capability := range capabilities.Items() {
		field := fmt.Sprintf("$.spec.capabilities[%d]", index)
		if capability.Type() != api.String {
			return false, refuseSelection(machine, "api.value", field, "controller capability must be a string",
				"write "+strings.TrimPrefix(field, "$.")+" on "+machine.Identity()+" as a capability name")
		}
		if seen[capability.Text()] {
			return false, refuseSelection(machine, "api.value", field, "controller capabilities must be unique",
				"remove the repeated "+strings.TrimPrefix(field, "$.")+" from "+machine.Identity())
		}
		seen[capability.Text()] = true
		containerRuntime = containerRuntime || capability.Text() == "container-runtime"
	}
	if !containerRuntime {
		return false, refuseSelection(machine, "api.invariant", "$.spec.capabilities", "controller requires the container-runtime capability", addRuntime)
	}
	return containerRuntime, nil
}

// selectRoute builds the controller Machine's route and proves it in the one
// grammar every acquisition route shares, naming the field that breaks it. The
// executable's own limits on that route are its unsupported shapes, which
// Select refuses before it gets here.
func selectRoute(machine api.Object, catalog api.Catalog) (Route, error) {
	choice := machine.Spec().Get("proxy")
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
	if ref.Type() != api.String || !api.ValidLexical("name", ref.Text()) {
		return Route{}, refuseSelection(machine, "api.reference", "$.spec.proxy.proxyRef", "proxy reference requires an explicit scalar Proxy name",
			"set spec.proxy.proxyRef on "+machine.Identity()+" to the name of a Proxy")
	}
	proxy, found := catalog.Find(api.Proxy, ref.Text())
	if !found {
		return Route{}, refuseSelection(machine, "api.reference", "$.spec.proxy.proxyRef", "proxy reference must resolve to exactly one Proxy",
			"declare the Proxy spec.proxy.proxyRef names on "+machine.Identity()+", or name a declared one")
	}
	connection := proxy.Spec().Get("connection")
	route := Route{proxyName: proxy.Name()}
	for _, entry := range []struct {
		field  string
		target *string
	}{{"httpProxy", &route.httpProxy}, {"httpsProxy", &route.httpsProxy}} {
		value := connection.Get(entry.field)
		if !value.Present() {
			continue
		}
		if value.Type() != api.String || !api.ValidLexical("http-url", value.Text()) || !api.ValidLexical("proxy-endpoint", value.Text()) {
			return Route{}, refuseSelection(proxy, "api.value", "$.spec.connection."+entry.field,
				"the controller route requires spec.connection."+entry.field+" to be a bare HTTP or HTTPS proxy endpoint",
				"set spec.connection."+entry.field+" on "+proxy.Identity()+" to a bare http or https endpoint such as http://proxy.example.test:3128, with no userinfo, path, query or fragment")
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
			if !api.ValidLexical("proxy-bypass", value.Text()) {
				return Route{}, refuseSelection(machine, "api.value", field, "the controller route requires each spec.proxy.noProxy entry to be a proxy bypass entry",
					"write "+strings.TrimPrefix(field, "$.")+" on "+machine.Identity()+" as "+bypassForms)
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

// bypassForms names every accepted form of a proxy bypass entry.
const bypassForms = "*, a host name, a .domain or *.domain suffix, an IP address or a CIDR block, each name or address optionally with :port, in at most 1024 bytes"

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
	for index, capability := range machine.Spec().Get("capabilities").Items() {
		if capability.Type() != api.String || capability.Text() == "container-runtime" || capability.Text() == "libvirt" {
			continue
		}
		field := fmt.Sprintf("spec.capabilities[%d]", index)
		shapes = append(shapes, UnsupportedShape{Object: machine, Field: "$." + field,
			Reason:      "this executable's controller stage prepares only the container-runtime and libvirt capabilities, and " + machine.Identity() + " declares another at " + field,
			Remediation: "remove " + field + " from " + machine.Identity()})
	}
	if bypass := machine.Spec().Get("proxy", "noProxy"); bypass.Type() == api.Sequence && bypass.Len() > maxProxyBypassEntries {
		shapes = append(shapes, UnsupportedShape{Object: machine, Field: "$.spec.proxy.noProxy",
			Reason:      "this executable's controller route carries at most " + strconv.Itoa(maxProxyBypassEntries) + " bypass entries, and " + machine.Identity() + " declares more in spec.proxy.noProxy",
			Remediation: "keep at most " + strconv.Itoa(maxProxyBypassEntries) + " entries in spec.proxy.noProxy on " + machine.Identity()})
	}
	ref := machine.Spec().Get("proxy", "proxyRef")
	proxy, found := catalog.Find(api.Proxy, ref.Text())
	if ref.Type() != api.String || !found {
		return shapes
	}
	if proxy.Spec().Get("management").Text() != "external" {
		return append(shapes, UnsupportedShape{Object: machine, Field: "$.spec.proxy.proxyRef",
			Reason:      "this executable's controller stage acquires only directly or through an external Proxy that is already ready, and " + machine.Identity() + " selects the managed " + proxy.Identity(),
			Remediation: "select direct: {} or an external Proxy in spec.proxy on " + machine.Identity()})
	}
	connection := proxy.Spec().Get("connection")
	if connection.Has("auth", "proxyAuthRef") {
		shapes = append(shapes, UnsupportedShape{Object: proxy, Field: "$.spec.connection.auth.proxyAuthRef",
			Reason:      "this executable's controller stage acquires through no authenticated proxy, and " + proxy.Identity() + " sets spec.connection.auth.proxyAuthRef",
			Remediation: "select direct: {} or an external Proxy that needs no authentication in spec.proxy on " + machine.Identity()})
	}
	if connection.Has("trustBundleRef") {
		shapes = append(shapes, UnsupportedShape{Object: proxy, Field: "$.spec.connection.trustBundleRef",
			Reason:      "this executable's controller stage trusts only the host's system trust store, and " + proxy.Identity() + " sets spec.connection.trustBundleRef",
			Remediation: "select direct: {} or an external Proxy without spec.connection.trustBundleRef that the host's system trust store verifies in spec.proxy on " + machine.Identity()})
	}
	if connection.Has("httpProxy") && !connection.Has("httpsProxy") {
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
