package managedos

import (
	"fmt"
	"net/netip"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

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
		return validateImage(o)
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
		issues = add(issues, issue("$.spec.os.family", "machine installation supports the rhel OS family", "set spec.os.family to rhel"))
	}
	version := s.Get("os", "version").Text()
	major := strings.SplitN(version, ".", 2)[0]
	if digits(major) {
		n, e := strconv.ParseUint(major, 10, 64)
		if e == nil && n < 9 {
			issues = add(issues, issue("$.spec.os.version", "numeric RHEL major versions must be at least 9", "set spec.os.version to a RHEL 9 or later version"))
		}
	}
	issues = add(issues, validateCustomizationEntries(custom)...)
	issues = add(issues, validateSubscription(s, c)...)
	issues = add(issues, validateServiceChoices(custom)...)
	issues = add(issues, validateInstallerSources(o, c)...)
	issues = add(issues, validateServedNames(o, c)...)
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
	repositoryGrammar = kickstartGrammar{"",
		"a repository ID is what dnf accepts in one: 1 to 239 ASCII letters, digits, '-', '_', '.' or ':', and neither '.' nor '..'", "extras"}
)

const subscriptionRepositoryRule = "kickstart-token"

var repositoryIDPattern = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,239}$`)

// RepositoryID reports whether s is a repository ID dnf accepts: ASCII letters,
// digits and "-_.:" (dnf's _REPOID_CHARS and libdnf's Repo::verifyId). The ID
// also names /etc/yum.repos.d/bootwright-<id>.repo, whose base name is at most
// 255 bytes, so the ID is at most 239 bytes, and neither "." nor "..".
func RepositoryID(s string) bool {
	return repositoryIDPattern.MatchString(s) && s != "." && s != ".."
}

func validateCustomizationEntries(custom api.Value) []api.Issue {
	issues := validateKickstartValues(custom)
	for i, repo := range custom.Get("repositories", "configure").Items() {
		path := fmt.Sprintf("$.spec.customizations.repositories.configure[%d]", i)
		if !RepositoryID(repo.Get("id").Text()) {
			issues = add(issues, grammarIssue(path+".id", repositoryGrammar))
		}
		entry := strings.TrimPrefix(path, "$.")
		gpg := !repo.Has("gpgCheck") || repo.Get("gpgCheck").Bool()
		if gpg && !repo.Has("gpgKeyURL") {
			issues = add(issues, issue(path+".gpgKeyURL", "GPG checking requires a key URL", "set "+entry+".gpgKeyURL, or set "+entry+".gpgCheck to false"))
		}
		if key := repo.Get("gpgKeyURL"); key.Present() && (!validMediaURL(key.Text(), true) || strings.ContainsAny(key.Text(), "\"'\\#")) {
			issues = add(issues, issue(path+".gpgKeyURL", "GPG key URL must use HTTP(S) or an absolute file URI, with no quote, backslash or '#'",
				"set "+entry+".gpgKeyURL to an https://, http:// or file:/// URL"))
		}
		if name := repo.Get("displayName"); name.Present() && (!utf8.ValidString(name.Text()) || strings.IndexFunc(name.Text(), lineBreak) >= 0) {
			issues = add(issues, issue(path+".displayName", "a repository display name is one line of UTF-8 text with no control character",
				"correct "+entry+".displayName to one line of text"))
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
		issues = add(issues, subscriptionConflict())
	}
	for _, entry := range []struct {
		value api.Value
		path  string
	}{{subscription, "$.spec.subscription"}, {fromSubscription, "$.spec.installer.anaconda.packageSource.fromSubscription"}} {
		if ref := entry.value.Get("entitlementRef"); ref.Present() {
			if entitlement, ok := c.Find(api.Entitlement, ref.Text()); ok && entitlement.Spec().Get("type").Text() != "redhat-rhel" {
				issues = add(issues, reference(entry.path+".entitlementRef", "OS registration requires a redhat-rhel Entitlement",
					"select an Entitlement of type redhat-rhel in "+strings.TrimPrefix(entry.path, "$.")+".entitlementRef"))
			}
		}
	}
	subRepos := s.Get("customizations", "repositories", "subscription")
	if subRepos.Present() {
		enable, disable := subRepos.Get("enable").Strings(), subRepos.Get("disable").Strings()
		if len(enable)+len(disable) == 0 {
			issues = add(issues, issue("$.spec.customizations.repositories.subscription", "subscription repositories require enable or disable entries",
				"add enable or disable entries, or remove spec.customizations.repositories.subscription"))
		}
		if !subscription.Present() && !fromSubscription.Present() {
			issues = add(issues, issue("$.spec.customizations.repositories.subscription", "subscription repositories require a registration entitlement",
				"set spec.subscription, or remove spec.customizations.repositories.subscription"))
		}
		for _, id := range enable {
			if id == "*" || !subscriptionRepositoryID(id) {
				issues = add(issues, issue("$.spec.customizations.repositories.subscription.enable", "enabled repository IDs are not the wildcard and are printable ASCII with no whitespace, quote, slash, backslash, '#' or comma, not starting with '%'",
					"correct that entry of spec.customizations.repositories.subscription.enable"))
			}
			if slices.Contains(disable, id) {
				issues = add(issues, issue("$.spec.customizations.repositories.subscription", "enabled and disabled repository IDs must be disjoint",
					"remove the ID from spec.customizations.repositories.subscription.enable or from its disable list"))
			}
		}
		for _, id := range disable {
			if id != "*" && !subscriptionRepositoryID(id) {
				issues = add(issues, issue("$.spec.customizations.repositories.subscription.disable", "disabled repository IDs must be identifiers or wildcard",
					"correct that entry of spec.customizations.repositories.subscription.disable"))
			}
		}
	}
	return issues
}

// subscriptionConflict refuses two registrations of one installation, which
// partial and complete validation both report.
func subscriptionConflict() api.Issue {
	return issue("$.spec.subscription", "top-level subscription cannot accompany installation fromSubscription",
		"remove spec.subscription or spec.installer.anaconda.packageSource.fromSubscription")
}

// serviceOverlap refuses a service both enabled and disabled, which partial and
// complete validation both report.
func serviceOverlap() api.Issue {
	return issue("$.spec.customizations.services", "enabled and disabled services must be disjoint",
		"remove the service from spec.customizations.services.enabled or from spec.customizations.services.disabled")
}

func validateServiceChoices(custom api.Value) []api.Issue {
	var issues []api.Issue
	enabled, disabled := custom.Get("services", "enabled").Strings(), custom.Get("services", "disabled").Strings()
	for _, service := range enabled {
		if slices.Contains(disabled, service) {
			issues = add(issues, serviceOverlap())
		}
	}
	if custom.Get("security", "firewall", "enabled").Bool() && (!slices.Contains(custom.Get("packages", "install").Strings(), "firewalld") || !slices.Contains(enabled, "firewalld")) {
		issues = add(issues, issue("$.spec.customizations.security.firewall.enabled", "enabled firewall requires the firewalld package and enabled service",
			"add firewalld to spec.customizations.packages.install and spec.customizations.services.enabled, or disable spec.customizations.security.firewall"))
	}
	if tpm := custom.Get("security", "diskEncryption", "unlock", "tpm2"); tpm.Has("pcrBank") && !tpm.Has("pcrIds") {
		issues = add(issues, issue("$.spec.customizations.security.diskEncryption.unlock.tpm2.pcrBank", "PCR bank requires selected PCR IDs",
			"set spec.customizations.security.diskEncryption.unlock.tpm2.pcrIds, or remove its pcrBank"))
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
				if bootwrightInstalled(machine) && server.Spec().Get("machineRef").Text() == machine.Name() {
					issues = add(issues, issue(selection.path+".serverRef",
						"an installation cannot publish through an artifact server placed on the Machine it installs, because that server cannot serve until the installation completes",
						"place "+server.Identity()+" on the controller Machine, or select a server placed there"))
					break
				}
			}
		}
	}
	for i, repo := range anaconda.Get("packageSource", "mirror", "repositories").Items() {
		if !RepositoryID(repo.Get("id").Text()) {
			issues = add(issues, grammarIssue(fmt.Sprintf("$.spec.installer.anaconda.packageSource.mirror.repositories[%d].id", i), repositoryGrammar))
		}
	}
	if hosted := anaconda.Get("packageSource", "hostedTree"); hosted.Present() {
		if !storeMediaReference(hosted.Get("fromMedia").Text()) {
			issues = add(issues, storeMediaIssue("spec.installer.anaconda.packageSource.hostedTree.fromMedia",
				"hosted package content is local-media:<filename.iso> naming a DVD image of the host media store", o))
		}
		if image, ok := c.Find(api.MachineImage, anaconda.Get("imageRef").Text()); ok && image.Spec().Get("bootMedia").Equal(hosted.Get("fromMedia")) {
			issues = add(issues, issue("$.spec.installer.anaconda.packageSource.hostedTree.fromMedia", "hosted content media must differ from the boot image",
				"select the DVD image in spec.installer.anaconda.packageSource.hostedTree.fromMedia on "+o.Identity()+" and the boot image in spec.bootMedia on "+image.Identity()))
		}
	}
	return issues
}

// validateServedNames refuses a profile whose package tree would be published
// in the directory a same-named Machine's installer image is published in:
// both are os/<name>/ on their server, and the Machine's inverse removes that
// directory whole, tree included.
func validateServedNames(o api.Object, c api.Catalog) []api.Issue {
	server, ok := infrastructureservices.ArtifactEndpoint(o.Spec().Get("installer", "anaconda", "packageSource", "hostedTree", "artifactServerEndpoint"), c)
	if !ok || !slices.ContainsFunc(consumers(o, c), bootwrightInstalled) {
		return nil
	}
	machine, ok := c.Find(api.Machine, o.Name())
	if !ok || !bootwrightInstalled(machine) {
		return nil
	}
	profile := o
	if selected := machine.Spec().Get("os", "installProfileRef").Text(); selected != o.Name() {
		if profile, ok = c.Find(api.MachineInstallProfile, selected); !ok {
			return nil
		}
	}
	imageServer, ok := infrastructureservices.ArtifactEndpoint(profile.Spec().Get("installer", "anaconda", "redfishVirtualMedia", "artifactServerEndpoint"), c)
	if !ok || imageServer.Name() != server.Name() {
		return nil
	}
	return []api.Issue{issue("$.spec.installer.anaconda.packageSource.hostedTree.artifactServerEndpoint.serverRef",
		"the package tree of this profile and the installer image of "+machine.Identity()+" would both be published beneath os/"+o.Name()+"/ on "+server.Identity()+", and removing that Machine's installation would remove the tree with it",
		"rename "+machine.Identity()+" or "+o.Identity()+", or publish the tree through another artifact server")}
}

func validateConsumers(o api.Object, c api.Catalog) []api.Issue {
	var issues []api.Issue
	s := o.Spec()
	anaconda := s.Get("installer", "anaconda")
	custom := s.Get("customizations")
	if anaconda.Present() && !anaconda.Has("redfishVirtualMedia", "artifactServerEndpoint") && slices.ContainsFunc(consumers(o, c), bootwrightInstalled) {
		issues = add(issues, issue("$.spec.installer.anaconda.redfishVirtualMedia.artifactServerEndpoint",
			"a Bootwright-installed Machine boots its installer through Redfish virtual media, so the profile it selects names the artifact server endpoint its installer image is published through",
			"set spec.installer.anaconda.redfishVirtualMedia.artifactServerEndpoint on "+o.Identity()))
	}
	for _, machine := range consumers(o, c) {
		provider, ok := c.Find(api.InfraProvider, machine.Spec().Get("substrate", "providerRef").Text())
		if !ok {
			continue
		}
		variant := substrate.Variant(provider)
		profileName := machine.Spec().Get("substrate", "profileRef").Text()
		profile, profileOK := localProfile(provider.Spec().Get(variant, "machineProfiles"), profileName)
		if s.Has("installer", "templateClone") && (variant != substrate.ArmVSphere || profileOK && !profile.Has("template")) {
			issues = add(issues, issue("$.spec.installer.templateClone", "template clone consumers require a vSphere profile with a template",
				"select spec.installer.anaconda, or place each consumer on a vSphere provider profile with a template"))
		}
		if custom.Has("security", "diskEncryption") && variant != substrate.ArmBaremetal && profileOK && !profile.Has("tpm") {
			issues = add(issues, issue("$.spec.customizations.security.diskEncryption", "virtual disk encryption requires TPM support in every consuming provider profile",
				"declare tpm on machine profile "+profileName+" of "+provider.Identity()+", or remove spec.customizations.security.diskEncryption"))
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
		return []api.Issue{issue("$.spec.rhsm", "Red Hat entitlements require RHSM intent", "set spec.rhsm on "+o.Identity())}
	}
	issues := []api.Issue{}
	management := rhsm.Get("management").Text()
	if management == "" {
		management = "managed"
	}
	if management == "external" {
		for _, key := range []string{"organizationRef", "activationKeyRef", "connectToInsights", "satellite"} {
			if rhsm.Has(key) {
				issues = add(issues, externalRHSMField(key))
			}
		}
	} else if management == "managed" {
		for _, key := range []string{"organizationRef", "activationKeyRef"} {
			if !rhsm.Has(key) {
				issues = add(issues, issue("$.spec.rhsm."+key, "managed RHSM requires organization and activation-key references",
					"set spec.rhsm.organizationRef and spec.rhsm.activationKeyRef"))
			}
		}
	}
	return issues
}

// externalRHSMField refuses registration detail on an externally managed
// registration, which partial and complete validation both report.
func externalRHSMField(key string) api.Issue {
	return issue("$.spec.rhsm."+key, "external RHSM permits only management", "remove spec.rhsm."+key)
}

// validateImage admits only an image of the host media store, because that is
// the only boot media the installation reads.
func validateImage(o api.Object) []api.Issue {
	if raw := o.Spec().Get("bootMedia").Text(); raw != "" && !storeMediaReference(raw) {
		return []api.Issue{storeMediaIssue("spec.bootMedia", "boot media is local-media:<filename.iso> naming an image of the host media store", o)}
	}
	return nil
}

// storeMediaReference holds for local-media:<name> naming a valid entry of the
// host media store, the one media source the installation reads.
func storeMediaReference(raw string) bool {
	name, local := strings.CutPrefix(raw, "local-media:")
	return local && ValidMediaName(name)
}

func storeMediaIssue(field, message string, owner api.Object) api.Issue {
	return api.Issue{Code: "api.value", Field: "$." + field, Message: message,
		Remediation: "import the image with bootwright media add --name <filename.iso> and set " + field + " to local-media:<filename.iso> on " + owner.Identity()}
}

func validateCloneCustomizations(o api.Object) []api.Issue {
	if !o.Spec().Has("installer", "templateClone") {
		return nil
	}
	custom := o.Spec().Get("customizations")
	issues := []api.Issue{}
	for _, path := range [][]string{{"localization"}, {"ssh", "initialPassword"}, {"packages"}, {"security", "selinux"}, {"security", "firewall"}, {"security", "fips"}, {"security", "diskEncryption"}} {
		if custom.Get(path...).Present() {
			field := "spec.customizations." + strings.Join(path, ".")
			issues = add(issues, issue("$."+field, "template clone forbids Anaconda-only customization", "remove "+field))
		}
	}
	return issues
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

// bootwrightInstalled holds for a Machine whose operating system this
// installation lays down rather than one the operator provides.
func bootwrightInstalled(machine api.Object) bool {
	return machine.Spec().Has("os", "provided") && !machine.Spec().Get("os", "provided").Bool()
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
func subscriptionRepositoryID(s string) bool {
	return api.ValidLexical(subscriptionRepositoryRule, s) && !strings.Contains(s, "/")
}

// lineBreak is any rune that ends or has no meaning in a line of the .repo file
// the installation writes: every control character and the Unicode line and
// paragraph separators.
func lineBreak(r rune) bool  { return unicode.IsControl(r) || r == '\u2028' || r == '\u2029' }
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

// issue states one refusal with the exact change that clears it, naming the
// field or object to change.
func issue(field, message, remediation string) api.Issue {
	return api.Issue{Code: "api.invariant", Field: field, Message: message, Remediation: remediation}
}
func reference(field, message, remediation string) api.Issue {
	i := issue(field, message, remediation)
	i.Code = "api.reference"
	return i
}
func add(issues []api.Issue, more ...api.Issue) []api.Issue {
	return append(issues, more[:min(len(more), 999-len(issues))]...)
}
