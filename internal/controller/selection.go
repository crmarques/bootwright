package controller

import (
	"fmt"
	"slices"

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
	route            Route
	versions         DependencyVersions
}

func (s Selection) EnvironmentName() string      { return s.environmentName }
func (s Selection) MachineName() string          { return s.machineName }
func (s Selection) ContainerRuntime() bool       { return s.containerRuntime }
func (s Selection) LibvirtClient() bool          { return s.libvirtClient }
func (s Selection) Route() Route                 { return s.route }
func (s Selection) Versions() DependencyVersions { return s.versions }

// Route contains explicit setup acquisition policy without Secret references or
// material. A route grants no authority to change the host's network settings.
type Route struct {
	direct     bool
	proxyName  string
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

// Select consumes an admitted, normalized effective catalog. Only the selected
// controller's requirements affect setup; unrelated lifecycle kinds add no
// prerequisites and do not establish or prevent their own readiness.
func Select(catalog api.Catalog) (Selection, error) {
	environments := catalog.OfKind(api.Environment)
	if len(environments) != 1 {
		return Selection{}, selectionFailure(api.Object{}, "api.invariant", "", "controller selection requires exactly one effective Environment")
	}
	environment := environments[0]
	if !api.ValidLexical("name", environment.Name()) {
		return Selection{}, selectionFailure(api.Object{}, "api.value", "$.metadata.name", "effective Environment requires a valid name")
	}
	ref := environment.Spec().Get("controller", "machineRef")
	if ref.Type() != api.String || !api.ValidLexical("name", ref.Text()) {
		return Selection{}, selectionFailure(environment, "api.reference", "$.spec.controller.machineRef", "controller requires an explicit scalar Machine name")
	}
	machine, found := catalog.Find(api.Machine, ref.Text())
	if !found {
		return Selection{}, selectionFailure(environment, "api.reference", "$.spec.controller.machineRef", "controller reference must resolve to exactly one Machine")
	}
	if provided := machine.Spec().Get("os", "provided"); provided.Type() != api.Boolean || !provided.Bool() {
		return Selection{}, selectionFailure(machine, "api.invariant", "$.spec.os.provided", "controller setup requires an OS-ready Machine with os.provided: true")
	}
	if local := machine.Spec().Get("access", "local"); local.Type() != api.Boolean || !local.Bool() || machine.Spec().Has("access", "ssh") {
		return Selection{}, selectionFailure(machine, "api.invariant", "$.spec.access", "controller setup requires access.local: true and no SSH access")
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
	return Selection{environmentName: environment.Name(), machineName: machine.Name(), containerRuntime: containerRuntime, libvirtClient: requiresLibvirtClient(catalog, machine), route: route, versions: versions}, nil
}

func requiresLibvirtClient(catalog api.Catalog, bastion api.Object) bool {
	if slices.Contains(bastion.Spec().Get("capabilities").Strings(), "libvirt") {
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

func selectCapabilities(machine api.Object) (bool, error) {
	capabilities := machine.Spec().Get("capabilities")
	if !capabilities.Present() {
		return false, selectionFailure(machine, "api.invariant", "$.spec.capabilities", "controller requires the container-runtime capability")
	}
	if capabilities.Type() != api.Sequence {
		return false, selectionFailure(machine, "api.value", "$.spec.capabilities", "controller capabilities must be a sequence")
	}
	containerRuntime := false
	seen := map[string]bool{}
	for index, capability := range capabilities.Items() {
		field := fmt.Sprintf("$.spec.capabilities[%d]", index)
		if capability.Type() != api.String {
			return false, selectionFailure(machine, "api.value", field, "controller capability must be a string")
		}
		if capability.Text() != "container-runtime" && capability.Text() != "libvirt" {
			return false, selectionFailure(machine, "controller.unsupported", field, "controller setup prepares the container-runtime and libvirt client capabilities")
		}
		if seen[capability.Text()] {
			return false, selectionFailure(machine, "api.value", field, "controller capabilities must be unique")
		}
		seen[capability.Text()] = true
		containerRuntime = containerRuntime || capability.Text() == "container-runtime"
	}
	if !containerRuntime {
		return false, selectionFailure(machine, "api.invariant", "$.spec.capabilities", "controller requires the container-runtime capability")
	}
	return containerRuntime, nil
}

func selectRoute(machine api.Object, catalog api.Catalog) (Route, error) {
	choice := machine.Spec().Get("proxy")
	if choice.Type() != api.Mapping {
		return Route{}, selectionFailure(machine, "api.value", "$.spec.proxy", "controller setup requires a normalized proxy choice")
	}
	if issues := infrastructureservices.ValidateProxy(choice, catalog, "$.spec.proxy", false); len(issues) != 0 {
		issue := issues[0]
		return Route{}, selectionFailure(machine, issue.Code, issue.Field, issue.Message)
	}
	if direct := choice.Get("direct"); direct.Present() {
		if direct.Type() != api.Mapping || direct.Len() != 0 {
			return Route{}, selectionFailure(machine, "api.value", "$.spec.proxy.direct", "direct acquisition requires an empty object")
		}
		return Route{direct: true}, nil
	}
	ref := choice.Get("proxyRef")
	if ref.Type() != api.String || !api.ValidLexical("name", ref.Text()) {
		return Route{}, selectionFailure(machine, "api.reference", "$.spec.proxy.proxyRef", "proxy reference requires an explicit scalar Proxy name")
	}
	proxy, found := catalog.Find(api.Proxy, ref.Text())
	if !found {
		return Route{}, selectionFailure(machine, "api.reference", "$.spec.proxy.proxyRef", "proxy reference must resolve to exactly one Proxy")
	}
	if proxy.Spec().Get("management").Text() != "external" {
		return Route{}, selectionFailure(machine, "controller.unsupported", "$.spec.proxy.proxyRef", "controller setup requires direct access or an external Proxy that is already ready")
	}
	connection := proxy.Spec().Get("connection")
	for _, field := range [][]string{{"auth", "proxyAuthRef"}, {"trustBundleRef"}} {
		if connection.Has(field...) {
			path := "$.spec.connection.trustBundleRef"
			if len(field) == 2 {
				path = "$.spec.connection.auth.proxyAuthRef"
			}
			return Route{}, selectionFailure(proxy, "controller.unsupported", path, "controller setup does not support proxy authentication or private trust")
		}
	}
	route := Route{proxyName: proxy.Name()}
	for _, entry := range []struct {
		field  string
		target *string
	}{{"httpProxy", &route.httpProxy}, {"httpsProxy", &route.httpsProxy}} {
		value := connection.Get(entry.field)
		if !value.Present() {
			continue
		}
		if value.Type() != api.String || !api.ValidLexical("http-url", value.Text()) {
			return Route{}, selectionFailure(proxy, "api.value", "$.spec.connection."+entry.field, "proxy URL must be an absolute HTTP(S) URL without embedded credentials")
		}
		*entry.target = value.Text()
	}
	if route.httpProxy == "" && route.httpsProxy == "" {
		return Route{}, selectionFailure(proxy, "api.value", "$.spec.connection", "external Proxy requires at least one HTTP(S) proxy URL")
	}
	if noProxy := choice.Get("noProxy"); noProxy.Present() {
		if noProxy.Type() != api.Sequence {
			return Route{}, selectionFailure(machine, "api.value", "$.spec.proxy.noProxy", "proxy bypass entries must be a sequence")
		}
		seen := map[string]bool{}
		for index, value := range noProxy.Items() {
			if value.Type() != api.String || value.Text() == "" || seen[value.Text()] {
				return Route{}, selectionFailure(machine, "api.value", fmt.Sprintf("$.spec.proxy.noProxy[%d]", index), "proxy bypass entries must be distinct nonempty strings")
			}
			seen[value.Text()] = true
			route.noProxy = append(route.noProxy, value.Text())
		}
	}
	return route, nil
}

func selectionFailure(object api.Object, code, field, message string) error {
	diagnostic := diagnostics.Diagnostic{Severity: "error", Code: code, Field: field, Message: message}
	if object.Kind() != "" {
		diagnostic.Object = &diagnostics.ObjectIdentity{APIVersion: api.APIVersion, Kind: string(object.Kind()), Name: object.Name()}
	}
	return &diagnostics.Failure{Diagnostics: []diagnostics.Diagnostic{diagnostic}}
}
