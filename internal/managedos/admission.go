package managedos

import (
	"fmt"
	"net/netip"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"unicode"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/infrastructureservices"
	"github.com/crmarques/bootwright/internal/substrate"
)

func Normalize(o api.Object, c api.Catalog) (api.Object, []api.Issue) {
	if o.Kind() == api.Entitlement && o.Spec().Get("type").Text() == "redhat-rhel" {
		return NormalizeRHSM(o), nil
	}
	if o.Kind() == api.MachineImage {
		s := o.Spec()
		if s.Has("checksum") {
			digest := strings.TrimSpace(s.Get("checksum").Text())
			digest = strings.TrimPrefix(strings.ToLower(digest), "sha256:")
			s = s.With("checksum", api.StringValue(digest))
		}
		return o.WithSpec(s), nil
	}
	if o.Kind() != api.MachineInstallProfile {
		return o, nil
	}
	s := o.Spec()
	if strings.EqualFold(s.Get("os", "family").Text(), "rhel") {
		s = s.WithPath(api.StringValue("rhel"), "os", "family")
	}
	custom := s.Get("customizations")
	if loc := custom.Get("localization"); loc.Present() {
		loc = loc.Default("formats", loc.Get("language"))
		custom = custom.With("localization", loc)
	}
	repos := custom.Get("repositories", "configure").Items()
	for i, repo := range repos {
		repos[i] = repo.Default("displayName", repo.Get("id"))
	}
	if custom.Has("repositories", "configure") {
		custom = custom.WithPath(api.ListValue(repos...), "repositories", "configure")
	}
	if tpm := custom.Get("security", "diskEncryption", "unlock", "tpm2"); tpm.Has("pcrIds") {
		custom = custom.WithPath(tpm.Default("pcrBank", api.StringValue("sha256")), "security", "diskEncryption", "unlock", "tpm2")
	}
	if len(consumers(o, c)) != 0 {
		services := custom.Get("services")
		enabled := services.Get("enabled").Strings()
		if !slices.Contains(enabled, "sshd") {
			enabled = append(enabled, "sshd")
		}
		custom = custom.With("services", services.With("enabled", api.StringList(enabled...)))
	}
	if custom.Present() {
		s = s.With("customizations", custom)
	}
	s = s.With("proxy", infrastructureservices.NormalizeProxy(s.Get("proxy"), c))
	if s.Has("ntp") {
		s = s.With("ntp", infrastructureservices.NormalizeServerSelections(s.Get("ntp"), c, api.NTPServer))
	}
	return o.WithSpec(s), nil
}

func ValidateAuthored(o api.Object, c api.Catalog) []api.Issue {
	if o.Kind() == api.Entitlement && o.Spec().Get("type").Text() == "redhat-rhel" {
		return ValidateRHSM(o, true)
	}
	if o.Kind() != api.MachineInstallProfile {
		return nil
	}
	issues := validateCloneCustomizations(o)
	return add(issues, infrastructureservices.ValidateProxyChoice(o.Spec().Get("proxy"), "$.spec.proxy")...)
}

func Validate(o api.Object, c api.Catalog) []api.Issue {
	if o.Kind() == api.Entitlement && o.Spec().Get("type").Text() == "redhat-rhel" {
		return ValidateRHSM(o, false)
	}
	if o.Kind() == api.MachineImage {
		return validateImage(o, c)
	}
	if o.Kind() != api.MachineInstallProfile {
		return nil
	}
	s := o.Spec()
	custom := s.Get("customizations")
	issues := validateCloneCustomizations(o)
	issues = add(issues, infrastructureservices.ValidateProxy(s.Get("proxy"), c, "$.spec.proxy", true)...)
	issues = add(issues, infrastructureservices.ValidateServerSelections(s.Get("ntp"), c, api.NTPServer, "$.spec.ntp")...)
	if family := s.Get("os", "family"); family.Present() && !strings.EqualFold(family.Text(), "rhel") {
		issues = add(issues, issue("$.spec.os.family", "machine installation supports the rhel OS family"))
	}
	version := s.Get("os", "version").Text()
	major := strings.SplitN(version, ".", 2)[0]
	if digits(major) {
		n, e := strconv.ParseUint(major, 10, 64)
		if e == nil && n < 9 {
			issues = add(issues, issue("$.spec.os.version", "numeric RHEL major versions must be at least 9"))
		}
	}
	issues = add(issues, validateCustomizationEntries(custom)...)
	issues = add(issues, validateSubscription(s, c)...)
	issues = add(issues, validateServiceChoices(custom)...)
	issues = add(issues, validateInstallerSources(o, c)...)
	return add(issues, validateConsumers(o, c)...)
}

// kickstartGrammar is the grammar of one class of authored value the Kickstart
// carries, the message that states it and a value it admits.
type kickstartGrammar struct {
	rule, message, example string
}

var (
	localeGrammar = kickstartGrammar{"kickstart-token",
		"a localization value is one Kickstart token: printable ASCII with no whitespace, quote, backslash, '#' or comma, not starting with '%'", "en_US.UTF-8"}
	packageGrammar = kickstartGrammar{"package-spec",
		"a package entry is a package name, glob or @group: letters, digits and _.+*?@:~^/[]-, not starting with '%' or '-'", "chrony"}
	serviceGrammar = kickstartGrammar{"systemd-unit",
		"a service is a systemd unit name: letters, digits and :_.@-, starting with a letter or digit, at most 255 bytes", "chronyd"}
	repositoryGrammar = kickstartGrammar{"kickstart-token",
		"a repository ID is printable ASCII with no whitespace, quote, slash, backslash, '#' or comma, not starting with '%'", "extras"}
)

func validateCustomizationEntries(custom api.Value) []api.Issue {
	issues := validateKickstartValues(custom)
	for i, repo := range custom.Get("repositories", "configure").Items() {
		path := fmt.Sprintf("$.spec.customizations.repositories.configure[%d]", i)
		if !repositoryID(repo.Get("id").Text()) {
			issues = add(issues, grammarIssue(path+".id", repositoryGrammar))
		}
		gpg := !repo.Has("gpgCheck") || repo.Get("gpgCheck").Bool()
		if gpg && !repo.Has("gpgKeyURL") {
			issues = add(issues, issue(path+".gpgKeyURL", "GPG checking requires a key URL"))
		}
		if key := repo.Get("gpgKeyURL"); key.Present() && !validMediaURL(key.Text(), true) {
			issues = add(issues, issue(path+".gpgKeyURL", "GPG key URL must use HTTP(S) or an absolute file URI"))
		}
	}
	return issues
}

// validateKickstartValues gives every localization, package and service value
// its grammar, so each reaches the Kickstart as one token.
func validateKickstartValues(custom api.Value) []api.Issue {
	var issues []api.Issue
	for _, field := range []string{"language", "formats", "keyboard", "timezone"} {
		if v := custom.Get("localization", field); v.Present() && !api.ValidLexical(localeGrammar.rule, v.Text()) {
			issues = add(issues, grammarIssue("$.spec.customizations.localization."+field, localeGrammar))
		}
	}
	for _, group := range []struct {
		path    []string
		grammar kickstartGrammar
	}{
		{[]string{"localization", "additionalLocales"}, localeGrammar},
		{[]string{"packages", "install"}, packageGrammar},
		{[]string{"services", "enabled"}, serviceGrammar},
		{[]string{"services", "disabled"}, serviceGrammar},
	} {
		for i, v := range custom.Get(group.path...).Items() {
			if !api.ValidLexical(group.grammar.rule, v.Text()) {
				issues = add(issues, grammarIssue(fmt.Sprintf("$.spec.customizations.%s[%d]", strings.Join(group.path, "."), i), group.grammar))
			}
		}
	}
	return issues
}

func grammarIssue(field string, grammar kickstartGrammar) api.Issue {
	return api.Issue{Code: "api.value", Field: field, Message: grammar.message,
		Remediation: "correct " + strings.TrimPrefix(field, "$.") + " to a value such as " + grammar.example}
}

func validateSubscription(s api.Value, c api.Catalog) []api.Issue {
	var issues []api.Issue
	anaconda := s.Get("installer", "anaconda")
	subscription := s.Get("subscription")
	fromSubscription := anaconda.Get("packageSource", "fromSubscription")
	if subscription.Present() && fromSubscription.Present() {
		issues = add(issues, issue("$.spec.subscription", "top-level subscription cannot accompany installation fromSubscription"))
	}
	for _, entry := range []struct {
		value api.Value
		path  string
	}{{subscription, "$.spec.subscription"}, {fromSubscription, "$.spec.installer.anaconda.packageSource.fromSubscription"}} {
		if ref := entry.value.Get("entitlementRef"); ref.Present() {
			if entitlement, ok := c.Find(api.Entitlement, ref.Text()); ok && entitlement.Spec().Get("type").Text() != "redhat-rhel" {
				issues = add(issues, reference(entry.path+".entitlementRef", "OS registration requires a redhat-rhel Entitlement"))
			}
		}
	}
	subRepos := s.Get("customizations", "repositories", "subscription")
	if subRepos.Present() {
		enable, disable := subRepos.Get("enable").Strings(), subRepos.Get("disable").Strings()
		if len(enable)+len(disable) == 0 {
			issues = add(issues, issue("$.spec.customizations.repositories.subscription", "subscription repositories require enable or disable entries"))
		}
		if !subscription.Present() && !fromSubscription.Present() {
			issues = add(issues, issue("$.spec.customizations.repositories.subscription", "subscription repositories require a registration entitlement"))
		}
		for _, id := range enable {
			if id == "*" || !repositoryID(id) {
				issues = add(issues, issue("$.spec.customizations.repositories.subscription.enable", "enabled repository IDs are not the wildcard and are printable ASCII with no whitespace, quote, slash, backslash, '#' or comma, not starting with '%'"))
			}
			if slices.Contains(disable, id) {
				issues = add(issues, issue("$.spec.customizations.repositories.subscription", "enabled and disabled repository IDs must be disjoint"))
			}
		}
		for _, id := range disable {
			if id != "*" && !repositoryID(id) {
				issues = add(issues, issue("$.spec.customizations.repositories.subscription.disable", "disabled repository IDs must be identifiers or wildcard"))
			}
		}
	}
	return issues
}

func validateServiceChoices(custom api.Value) []api.Issue {
	var issues []api.Issue
	enabled, disabled := custom.Get("services", "enabled").Strings(), custom.Get("services", "disabled").Strings()
	for _, service := range enabled {
		if slices.Contains(disabled, service) {
			issues = add(issues, issue("$.spec.customizations.services", "enabled and disabled services must be disjoint"))
		}
	}
	if custom.Get("security", "firewall", "enabled").Bool() && (!slices.Contains(custom.Get("packages", "install").Strings(), "firewalld") || !slices.Contains(enabled, "firewalld")) {
		issues = add(issues, issue("$.spec.customizations.security.firewall.enabled", "enabled firewall requires the firewalld package and enabled service"))
	}
	if tpm := custom.Get("security", "diskEncryption", "unlock", "tpm2"); tpm.Has("pcrBank") && !tpm.Has("pcrIds") {
		issues = add(issues, issue("$.spec.customizations.security.diskEncryption.unlock.tpm2.pcrBank", "PCR bank requires selected PCR IDs"))
	}
	return issues
}

func validateInstallerSources(o api.Object, c api.Catalog) []api.Issue {
	var issues []api.Issue
	anaconda := o.Spec().Get("installer", "anaconda")
	for _, selection := range []struct {
		value api.Value
		path  string
		http  bool
	}{{anaconda.Get("redfishVirtualMedia", "artifactServerEndpoint"), "$.spec.installer.anaconda.redfishVirtualMedia.artifactServerEndpoint", false}, {anaconda.Get("packageSource", "hostedTree", "artifactServerEndpoint"), "$.spec.installer.anaconda.packageSource.hostedTree.artifactServerEndpoint", true}} {
		issues = add(issues, infrastructureservices.ValidateArtifactEndpoint(selection.value, c, selection.path, selection.http)...)
		if server, ok := infrastructureservices.ArtifactEndpoint(selection.value, c); ok && server.Spec().Get("management").Text() == "managed" {
			for _, machine := range consumers(o, c) {
				if machine.Spec().Has("os", "provided") && !machine.Spec().Get("os", "provided").Bool() && server.Spec().Get("machineRef").Text() == machine.Name() {
					issues = add(issues, issue(selection.path+".serverRef", "installation requires an artifact server hosted on the same Machine being installed; place the server on an independently available Machine"))
					break
				}
			}
		}
	}
	if hosted := anaconda.Get("packageSource", "hostedTree"); hosted.Present() {
		if !validMedia(hosted.Get("fromMedia").Text(), false) {
			issues = add(issues, issue("$.spec.installer.anaconda.packageSource.hostedTree.fromMedia", "hosted installation content requires local or file DVD media"))
		}
		if image, ok := c.Find(api.MachineImage, anaconda.Get("imageRef").Text()); ok && image.Spec().Get("bootMedia").Equal(hosted.Get("fromMedia")) {
			issues = add(issues, issue("$.spec.installer.anaconda.packageSource.hostedTree.fromMedia", "hosted content media must differ from the boot image"))
		}
	}
	return issues
}

func validateConsumers(o api.Object, c api.Catalog) []api.Issue {
	var issues []api.Issue
	s := o.Spec()
	anaconda := s.Get("installer", "anaconda")
	custom := s.Get("customizations")
	for _, machine := range consumers(o, c) {
		provider, ok := c.Find(api.InfraProvider, machine.Spec().Get("substrate", "providerRef").Text())
		if !ok {
			continue
		}
		variant := substrate.Variant(provider)
		profile, profileOK := localProfile(provider.Spec().Get(variant, "machineProfiles"), machine.Spec().Get("substrate", "profileRef").Text())
		if s.Has("installer", "templateClone") && (variant != substrate.ArmVSphere || profileOK && !profile.Has("template")) {
			issues = add(issues, issue("$.spec.installer.templateClone", "template clone consumers require a vSphere profile with a template"))
		}
		if anaconda.Present() && variant == substrate.ArmBaremetal && !anaconda.Has("redfishVirtualMedia", "artifactServerEndpoint") {
			issues = add(issues, issue("$.spec.installer.anaconda.redfishVirtualMedia.artifactServerEndpoint", "bare-metal installation requires a complete managed artifact endpoint"))
		}
		if custom.Has("security", "diskEncryption") && variant != substrate.ArmBaremetal && profileOK && !profile.Has("tpm") {
			issues = add(issues, issue("$.spec.customizations.security.diskEncryption", "virtual disk encryption requires TPM support in every consuming provider profile"))
		}
		if custom.Get("hostname", "source").Text() == "machineName" && clusterBound(machine, c) {
			issues = add(issues, issue("$.spec.customizations.hostname.source", "machineName hostname customization is forbidden for cluster-bound Machines"))
		}
	}
	return issues
}

// NormalizeRHSM supplies declared registration defaults for either Red Hat entitlement.
func NormalizeRHSM(o api.Object) api.Object {
	if o.Kind() != api.Entitlement || !slices.Contains([]string{"redhat-rhel", "redhat-ceph"}, o.Spec().Get("type").Text()) || !o.Spec().Has("rhsm") {
		return o
	}
	s := o.Spec()
	rhsm := s.Get("rhsm").Default("management", api.StringValue("managed"))
	if rhsm.Get("management").Text() == "managed" {
		rhsm = rhsm.Default("connectToInsights", api.BoolValue(false))
		satellite := rhsm.Get("satellite")
		if satellite.Present() && satellite.Has("hostname") && !satellite.Has("contentBaseURL") {
			host := satellite.Get("hostname").Text()
			if addr, err := netip.ParseAddr(host); err == nil && addr.Is6() {
				host = "[" + host + "]"
			}
			satellite = satellite.With("contentBaseURL", api.StringValue("https://"+host+"/pulp/content"))
			rhsm = rhsm.With("satellite", satellite)
		}
	}
	return o.WithSpec(s.With("rhsm", rhsm))
}

// ValidateRHSM checks declared RHSM intent without resolving or reading Secret material.
func ValidateRHSM(o api.Object, authored bool) []api.Issue {
	if o.Kind() != api.Entitlement || !slices.Contains([]string{"redhat-rhel", "redhat-ceph"}, o.Spec().Get("type").Text()) {
		return nil
	}
	rhsm := o.Spec().Get("rhsm")
	if !rhsm.Present() {
		return []api.Issue{issue("$.spec.rhsm", "Red Hat entitlements require RHSM intent")}
	}
	issues := []api.Issue{}
	management := rhsm.Get("management").Text()
	if management == "" {
		management = "managed"
	}
	if management == "external" {
		for _, key := range []string{"organizationRef", "activationKeyRef", "connectToInsights", "satellite"} {
			if rhsm.Has(key) {
				issues = add(issues, issue("$.spec.rhsm."+key, "external RHSM permits only management"))
			}
		}
	} else if management == "managed" {
		for _, key := range []string{"organizationRef", "activationKeyRef"} {
			if !rhsm.Has(key) {
				issues = add(issues, issue("$.spec.rhsm."+key, "managed RHSM requires organization and activation-key references"))
			}
		}
	}
	return issues
}

func validateImage(o api.Object, c api.Catalog) []api.Issue {
	issues := []api.Issue{}
	raw := o.Spec().Get("bootMedia").Text()
	if raw != "" && !validMedia(raw, true) {
		issues = add(issues, issue("$.spec.bootMedia", "boot media must be a local-media ISO basename, an absolute file URI, or HTTP(S) URL"))
	}
	remote := strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://")
	used := false
	for _, profile := range c.OfKind(api.MachineInstallProfile) {
		used = used || profile.Spec().Get("installer", "anaconda", "imageRef").Text() == o.Name()
	}
	for _, env := range c.OfKind(api.Environment) {
		used = used || containsReference(env.Spec().Get("lifecycle", "rescue"), "imageRef", o.Name())
	}
	if remote && used && !o.Spec().Has("checksum") {
		issues = add(issues, issue("$.spec.checksum", "remote lifecycle media requires a SHA-256 content pin"))
	}
	return issues
}
func validateCloneCustomizations(o api.Object) []api.Issue {
	if !o.Spec().Has("installer", "templateClone") {
		return nil
	}
	custom := o.Spec().Get("customizations")
	issues := []api.Issue{}
	for _, path := range [][]string{{"localization"}, {"ssh", "initialPassword"}, {"storage"}, {"packages"}, {"security", "selinux"}, {"security", "firewall"}, {"security", "fips"}, {"security", "diskEncryption"}} {
		if custom.Get(path...).Present() {
			issues = add(issues, issue("$.spec.customizations."+strings.Join(path, "."), "template clone forbids Anaconda-only customization"))
		}
	}
	return issues
}
func validMedia(raw string, remote bool) bool {
	if strings.HasPrefix(raw, "local-media:") {
		name := strings.TrimPrefix(raw, "local-media:")
		return len(name) > 4 && strings.HasSuffix(name, ".iso") && !strings.ContainsAny(name, "/\\\x00\r\n") && name != ".iso" && !hasSpace(name)
	}
	if strings.HasPrefix(raw, "file:") {
		return validMediaURL(raw, true) && strings.HasPrefix(raw, "file:///")
	}
	return remote && validMediaURL(raw, false)
}
func validMediaURL(raw string, file bool) bool {
	if hasSpace(raw) {
		return false
	}
	u, e := url.Parse(raw)
	if e != nil || u.User != nil || u.Fragment != "" || u.Opaque != "" {
		return false
	}
	if u.Scheme == "file" {
		return file && u.Host == "" && strings.HasPrefix(raw, "file:///") && len(u.Path) > 1 && u.RawQuery == ""
	}
	return (u.Scheme == "http" || u.Scheme == "https") && u.Hostname() != ""
}
func consumers(o api.Object, c api.Catalog) []api.Object {
	out := []api.Object{}
	for _, machine := range c.OfKind(api.Machine) {
		if machine.Spec().Get("os", "installProfileRef").Text() == o.Name() {
			out = append(out, machine)
		}
	}
	return out
}
func localProfile(values api.Value, name string) (api.Value, bool) {
	var found api.Value
	n := 0
	for _, value := range values.Items() {
		if value.Get("name").Text() == name {
			found = value
			n++
		}
	}
	return found, n == 1
}
func clusterBound(machine api.Object, c api.Catalog) bool {
	for _, kind := range []api.Kind{api.ContainerCluster, api.StorageCluster} {
		for _, cluster := range c.OfKind(kind) {
			if containsReference(cluster.Spec(), "machineRef", machine.Name()) {
				return true
			}
		}
	}
	return false
}
func containsReference(value api.Value, key, name string) bool {
	if value.Get(key).Text() == name {
		return true
	}
	for _, f := range value.Fields() {
		if containsReference(f.Value, key, name) {
			return true
		}
	}
	for _, item := range value.Items() {
		if containsReference(item, key, name) {
			return true
		}
	}
	return false
}
func repositoryID(s string) bool {
	return api.ValidLexical(repositoryGrammar.rule, s) && !strings.Contains(s, "/")
}
func hasSpace(s string) bool { return strings.IndexFunc(s, unicode.IsSpace) >= 0 }
func digits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
func issue(field, message string) api.Issue {
	return api.Issue{Code: "api.invariant", Field: field, Message: message, Remediation: "make OS installation intent consistent with its consumers and references"}
}
func reference(field, message string) api.Issue {
	i := issue(field, message)
	i.Code = "api.reference"
	return i
}
func add(issues []api.Issue, more ...api.Issue) []api.Issue {
	return append(issues, more[:min(len(more), 999-len(issues))]...)
}
