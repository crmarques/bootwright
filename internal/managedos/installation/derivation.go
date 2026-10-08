package installation

import (
	"net/netip"
	"net/url"
	"slices"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/infrastructureservices/managedservice"
	"github.com/crmarques/bootwright/internal/substrate"
)

// guestAgent is installed on a Machine whose identity channel reads through it,
// because that channel is how completion is proved. A machine proved another
// way does not need it and does not get it.
const guestAgent = "qemu-guest-agent"

// installationFor derives the complete unattended installation from effective
// state alone. It resolves every service the guest uses while installing, and
// records each managed one as a requirement so those blocks complete first.
func installationFor(catalog api.Catalog, machine, profile api.Object, request Request, needs *Requirements) (Installation, error) {
	template, err := substrate.NetworkTemplate(catalog, machine)
	if err != nil {
		return Installation{}, err
	}
	address, prefix, iface, err := installAddress(machine)
	if err != nil {
		return Installation{}, err
	}
	hostname, err := machineHostname(machine)
	if err != nil {
		return Installation{}, err
	}
	nameservers, err := resolverAddresses(catalog, machine, needs)
	if err != nil {
		return Installation{}, err
	}
	timeSources, err := timeAddresses(catalog, machine, profile, needs)
	if err != nil {
		return Installation{}, err
	}
	customizations := profile.Spec().Get("customizations")
	source, err := packageSource(request)
	if err != nil {
		return Installation{}, err
	}
	language := orDefault(customizations.Get("localization", "language").Text(), "en_US.UTF-8")
	formats := customizations.Get("localization", "formats").Text()
	langpacks, err := formatsLangpack(language, formats)
	if err != nil {
		return Installation{}, err
	}
	if reason, remediation := refusedProxy(catalog, machine, profile); reason != "" {
		return Installation{}, refusal("lifecycle.unsupported", reason, remediation)
	}
	repositories := repositoriesFor(customizations)
	route := proxyRouteFor(catalog, machine, profile)
	for index := range repositories {
		repositories[index].Proxy = route.proxyFor(repositories[index].BaseURL, request)
	}
	installation := Installation{
		Address:          address,
		AdditionalLocale: customizations.Get("localization", "additionalLocales").Strings(),
		DisabledServices: SortedUnique(customizations.Get("services", "disabled").Strings()),
		EnabledServices:  SortedUnique(append(customizations.Get("services", "enabled").Strings(), "sshd")),
		ExcludeDocs:      customizations.Get("packages", "excludeDocs").Bool(),
		Firewall:         firewallChoice(customizations),
		Formats:          formats,
		Gateway:          substrate.DefaultGateway(template),
		Hostname:         hostname,
		Interface:        iface,
		Keyboard:         orDefault(customizations.Get("localization", "keyboard").Text(), "us"),
		Language:         language,
		HostKeyPath:      HostKeyPath,
		MarkerPath:       MarkerPath,
		Nameservers:      nameservers,
		NTPServers:       timeSources,
		Packages:         SortedUnique(append(append(customizations.Get("packages", "install").Strings(), identityPackages(request.Target)...), langpacks...)),
		PackageSource:    source,
		Prefix:           prefix,
		Repositories:     repositories,
		RootDevice:       machine.Spec().Get("os", "install", "rootDeviceHints", "deviceName").Text(),
		SELinux:          customizations.Get("security", "selinux", "mode").Text(),
		Timezone:         orDefault(customizations.Get("localization", "timezone").Text(), "UTC"),
		User:             installUser,
		WeakDeps:         weakDepsChoice(customizations),
	}
	installation.Channel, installation.Physical = request.Target.Channel, request.Target.Physical
	installation.HostKeyType = request.Target.HostKeyType
	if request.Target.Hardware != nil {
		installation.InterfaceMAC = installInterfaceMAC(machine, request.Target.Hardware, iface)
		for _, declared := range request.Target.Hardware.Interfaces {
			installation.ExpectedMACs = append(installation.ExpectedMACs, declared.MACAddress)
		}
		installation.ExpectedMACs = SortedUnique(installation.ExpectedMACs)
	}
	return installation, nil
}

// identityPackages are what the machine needs installed for its own identity
// channel to answer afterwards.
func identityPackages(target Target) []string {
	if target.Channel == substrate.ChannelGuestAgent {
		return []string{guestAgent}
	}
	return nil
}

// installInterfaceMAC resolves the hardware address of the interface the
// installation configures. A physical machine is addressed by that address
// rather than by a kernel interface name, because the name a booted installer
// assigns is not the name the declaration used.
func installInterfaceMAC(machine api.Object, hardware *Hardware, iface string) string {
	nic := iface
	for _, binding := range machine.Spec().Get("network", "interfaceBinding").Items() {
		if binding.Get("interfaceName").Text() == iface {
			nic = binding.Get("nicRef").Text()
			break
		}
	}
	for _, declared := range hardware.Interfaces {
		if declared.Name == nic {
			return declared.MACAddress
		}
	}
	return ""
}

// packageSource is what Anaconda installs from: the boot media itself when the
// profile names no source, or the tree this block publishes.
func packageSource(request Request) (string, error) {
	if request.Tree == nil {
		return "cdrom", nil
	}
	if request.Tree.URL == "" {
		return "", refusal("lifecycle.state", "the hosted package tree has no published URL", "")
	}
	return "url --url=" + request.Tree.URL, nil
}

// installAddress reads the one static assignment the installer applies, from
// the Machine's own selected install address.
func installAddress(machine api.Object) (string, int, string, error) {
	network := machine.Spec().Get("network")
	reference := network.Get("installAddressRef").Text()
	entry, found := findNamed(network.Get("addresses"), "name", reference)
	if !found {
		return "", 0, "", refusal("api.reference", "the Machine's install address does not resolve", "set spec.network.installAddressRef on "+machine.Identity())
	}
	address, prefix, ok := netmaskPrefix(entry.Get("address").Text())
	if !ok {
		return "", 0, "", refusal("api.value", "the Machine's install address carries no prefix", "declare it as an IP with its prefix on "+machine.Identity())
	}
	iface := entry.Get("interface").Text()
	if iface == "" {
		return "", 0, "", refusal("api.value", "the Machine's install address names no interface", "set the interface of that address on "+machine.Identity())
	}
	return address, prefix, iface, nil
}

func machineHostname(machine api.Object) (string, error) {
	entry, found := findNamed(machine.Spec().Get("network", "addresses"), "name", "fqdn")
	if !found || entry.Get("address").Text() == "" {
		return "", refusal("api.required", "the Machine declares no fully qualified name", "declare an fqdn address on "+machine.Identity())
	}
	return entry.Get("address").Text(), nil
}

// resolverAddresses derives the name servers the guest resolves through while
// installing, from the network configuration's own selections: a managed
// server's endpoint address, or an external server's declared one.
func resolverAddresses(catalog api.Catalog, machine api.Object, needs *Requirements) ([]string, error) {
	spec, err := substrate.NetworkSpec(catalog, machine)
	if err != nil {
		return nil, err
	}
	var addresses []string
	for _, selection := range spec.Get("dns").Items() {
		address, name, err := managedservice.ServiceEndpoint(catalog, api.DNSServer, selection, machine.Identity())
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

// timeAddresses derives the time sources the guest uses, from the Machine's own
// override when it has one and the profile's selections otherwise.
func timeAddresses(catalog api.Catalog, machine, profile api.Object, needs *Requirements) ([]string, error) {
	selections := profile.Spec().Get("ntp")
	if override := machine.Spec().Get("os", "install", "ntp"); override.Present() {
		selections = override
	}
	var addresses []string
	for _, selection := range selections.Items() {
		address, name, err := managedservice.ServiceEndpoint(catalog, api.NTPServer, selection, machine.Identity())
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

// formatsLangpack is the glibc langpack the formats locale needs beyond the
// language's own: none when formats is the language, a C or POSIX locale or a
// locale of the language's own code.
func formatsLangpack(language, formats string) ([]string, error) {
	if formats == "" || formats == language {
		return nil, nil
	}
	code := localeCode(formats)
	if code == "c" || code == "posix" || code == localeCode(language) {
		return nil, nil
	}
	if len(code) < 2 || len(code) > 3 || strings.IndexFunc(code, func(r rune) bool { return r < 'a' || r > 'z' }) >= 0 {
		return nil, refusal("api.value", "the formats locale names no language a glibc langpack provides",
			"correct spec.customizations.localization.formats of the install profile")
	}
	return []string{"glibc-langpack-" + code}, nil
}

// localeCode is a locale's language code: everything before its territory,
// codeset or modifier, lowercased.
func localeCode(locale string) string {
	if index := strings.IndexAny(locale, "_.@"); index >= 0 {
		locale = locale[:index]
	}
	return strings.ToLower(locale)
}

func repositoriesFor(customizations api.Value) []Repository {
	var repositories []Repository
	for _, entry := range customizations.Get("repositories", "configure").Items() {
		enabled, gpgCheck := true, true
		if value := entry.Get("enabled"); value.Type() == api.Boolean {
			enabled = value.Bool()
		}
		if value := entry.Get("gpgCheck"); value.Type() == api.Boolean {
			gpgCheck = value.Bool()
		}
		id := entry.Get("id").Text()
		repositories = append(repositories, Repository{
			BaseURL: entry.Get("baseURL").Text(), Enabled: enabled, GPGCheck: gpgCheck,
			GPGKeyURL: entry.Get("gpgKeyURL").Text(), ID: id, Name: orDefault(entry.Get("displayName").Text(), id),
		})
	}
	slices.SortFunc(repositories, func(x, y Repository) int { return strings.Compare(x.ID, y.ID) })
	return repositories
}

// firewallChoice and weakDepsChoice keep an absent declaration distinct from a
// declared false, because the operating system's own default is not the same
// answer as an explicit one.
func firewallChoice(customizations api.Value) string {
	value := customizations.Get("security", "firewall", "enabled")
	if value.Type() != api.Boolean {
		return ""
	}
	if value.Bool() {
		return "enabled"
	}
	return "disabled"
}

func weakDepsChoice(customizations api.Value) string {
	value := customizations.Get("packages", "installWeakDeps")
	if value.Type() != api.Boolean || value.Bool() {
		return ""
	}
	return "--exclude-weakdeps"
}

func orDefault(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

// effectiveProxy is the proxy choice a Machine's installed system uses: its own
// spec.proxy, or the profile's when it declares none, which is what admission
// normalizes it to.
func effectiveProxy(machine, profile api.Object) api.Value {
	if choice := machine.Spec().Get("proxy"); choice.Present() {
		return choice
	}
	return profile.Spec().Get("proxy")
}

// refusedProxy says why the installed system's repositories cannot be given
// the proxy its Machine selects, or nothing when they can. A .repo file
// carries a proxy URL and nothing else, so a Proxy that needs a credential or
// a private trust anchor, or that declares no URL, is refused while a
// repository would use it; with no repository it carries nothing.
func refusedProxy(catalog api.Catalog, machine, profile api.Object) (reason, remediation string) {
	if len(profile.Spec().Get("customizations", "repositories", "configure").Items()) == 0 {
		return "", ""
	}
	choice := effectiveProxy(machine, profile)
	proxy, found := catalog.Find(api.Proxy, choice.Get("proxyRef").Text())
	if choice.Has("direct") || !found {
		return "", ""
	}
	remediation = "select direct: {} or an external Proxy without spec.connection.auth and spec.connection.trustBundleRef in spec.proxy of " +
		machine.Identity() + " or of " + profile.Identity()
	connection := proxy.Spec().Get("connection")
	if connection.Has("auth", "proxyAuthRef") || connection.Has("trustBundleRef") {
		return "the installed system's repositories would reach " + proxy.Identity() +
			" through a credential or private trust anchor, which an installation cannot carry", remediation
	}
	if connection.Get("httpProxy").Text() == "" && connection.Get("httpsProxy").Text() == "" {
		return "the installed system's repositories would be reached through " + proxy.Identity() +
			", which declares no proxy URL", remediation
	}
	return "", ""
}

// proxyRoute is the credential-free proxy the installed system's repositories
// are reached through, and the entries that bypass it.
type proxyRoute struct {
	httpProxy, httpsProxy string
	noProxy               []string
}

// proxyRouteFor reads the Machine's effective proxy choice: direct access, an
// absent choice or an unresolved Proxy route nothing.
func proxyRouteFor(catalog api.Catalog, machine, profile api.Object) proxyRoute {
	choice := effectiveProxy(machine, profile)
	proxy, found := catalog.Find(api.Proxy, choice.Get("proxyRef").Text())
	if choice.Has("direct") || !found {
		return proxyRoute{}
	}
	connection := proxy.Spec().Get("connection")
	return proxyRoute{
		httpProxy:  connection.Get("httpProxy").Text(),
		httpsProxy: connection.Get("httpsProxy").Text(),
		noProxy:    choice.Get("noProxy").Strings(),
	}
}

// proxyFor is the proxy one repository's base URL is fetched through. The
// installation's own artifact endpoints are exempt, as is every host a
// noProxy entry matches; otherwise the scheme's proxy is used, or the other
// one when the Proxy declares only that.
func (r proxyRoute) proxyFor(baseURL string, request Request) string {
	if r.httpProxy == "" && r.httpsProxy == "" {
		return ""
	}
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return ""
	}
	host := strings.ToLower(parsed.Hostname())
	for _, artifact := range artifactURLs(request) {
		if endpoint, err := url.Parse(artifact); err == nil && strings.ToLower(endpoint.Hostname()) == host {
			return ""
		}
	}
	for _, entry := range r.noProxy {
		if noProxyMatches(entry, host) {
			return ""
		}
	}
	if parsed.Scheme == "https" {
		return orDefault(r.httpsProxy, r.httpProxy)
	}
	return orDefault(r.httpProxy, r.httpsProxy)
}

func artifactURLs(request Request) []string {
	urls := []string{request.installerImage().URL}
	if request.Tree != nil {
		urls = append(urls, request.Tree.URL)
	}
	return urls
}

// noProxyMatches reports whether one bypass entry covers a lowercase host
// without brackets: '*' covers every host, a '.domain' or '*.domain' suffix
// the domain and its subdomains, an IP or CIDR the IP literals it equals or
// contains, and any other entry the host it names, its port ignored.
func noProxyMatches(entry, host string) bool {
	entry = strings.ToLower(strings.TrimSpace(entry))
	if entry == "*" {
		return true
	}
	if prefix, err := netip.ParsePrefix(strings.Trim(entry, "[]")); err == nil {
		address, err := netip.ParseAddr(host)
		return err == nil && prefix.Contains(address.Unmap())
	}
	if address, err := netip.ParseAddr(strings.Trim(entry, "[]")); err == nil {
		literal, err := netip.ParseAddr(host)
		return err == nil && literal.Unmap() == address.Unmap()
	}
	if strings.HasPrefix(entry, "[") {
		if end := strings.Index(entry, "]"); end > 0 {
			entry = entry[1:end]
		}
	} else if strings.Count(entry, ":") == 1 {
		entry, _, _ = strings.Cut(entry, ":")
	}
	if address, err := netip.ParseAddr(entry); err == nil {
		literal, err := netip.ParseAddr(host)
		return err == nil && literal.Unmap() == address.Unmap()
	}
	entry = strings.TrimPrefix(entry, "*")
	if suffix, found := strings.CutPrefix(entry, "."); found {
		return host == suffix || strings.HasSuffix(host, entry)
	}
	return host == entry
}
