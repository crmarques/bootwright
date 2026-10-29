package installation

import (
	"net/netip"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/crmarques/bootwright/internal/managedos"
	"github.com/crmarques/bootwright/internal/substrate"
)

const kickstartVersion = "kickstart-anaconda-v15"

// guestHostKeys are the keys a guest-agent installation generates here rather
// than at first boot, so the republished copy is the key sshd presents. Each
// names the ssh-keygen arguments that generate it explicitly, none when
// ssh-keygen -A does; a FIPS-mode system refuses Ed25519 and gets ECDSA.
var guestHostKeys = map[string]string{"ed25519": "", "ecdsaP256": "-t ecdsa -b 256"}

func guestHostKeyType(fips bool) string {
	if fips {
		return "ecdsaP256"
	}
	return "ed25519"
}

// agentFilter is the guest agent's RPC filter, and identityRPCs are the
// commands the identity operation reads a bounded guest file through. RHEL
// ships an allow list that omits all three, so an installation that left the
// filter alone would complete and then prove nothing.
const (
	agentFilter  = "/etc/sysconfig/qemu-ga"
	identityRPCs = "guest-file-open,guest-file-close,guest-file-read"
)

// PrivateURLToken and CertificateToken are substituted at execution like the
// two below. The private URL carries the unguessable segment the attempt mints,
// so it is never frozen in the plan, and the certificate is the public half of
// the artifact server's own, which the installer verifies the fetch against.
const (
	PrivateURLToken  = "@@BOOTWRIGHT_PRIVATE_URL@@"
	CertificateToken = "@@BOOTWRIGHT_ARTIFACT_CERTIFICATE@@"
)

// MarkerToken and AuthorizedKeyToken are the two values the adapter substitutes
// into the frozen Kickstart at render time. Both are derived at execution: the
// marker names the request digest, which is the digest of the request that
// carries the Kickstart, and the authorized key comes from a bound Secret. The
// substitution is the adapter's only edit, and neither value is secret.
const (
	MarkerToken        = "@@BOOTWRIGHT_MARKER@@"
	AuthorizedKeyToken = "@@BOOTWRIGHT_AUTHORIZED_KEY@@"
)

// Repository is one repository the installed system configures. The %post
// writes it as a .repo file, so it is never an install source; Proxy is the
// proxy the installed system reaches it through, or empty for direct access.
type Repository struct {
	BaseURL   string
	Enabled   bool
	GPGCheck  bool
	GPGKeyURL string
	ID        string
	Name      string
	Proxy     string
}

// Installation is everything the Kickstart is derived from. Every field comes
// from effective state alone, so two compilations of one revision render the
// same bytes.
type Installation struct {
	Address          string
	AdditionalLocale []string
	// Channel is the identity channel this machine will answer on, which
	// decides what the %post has to establish for it to answer at all.
	Channel string
	// ExpectedMACs and Physical belong to operator-owned hardware: the
	// installer proves it is running on the machine the declaration names
	// before it erases anything.
	ExpectedMACs []string
	Physical     bool
	// BondOptions are mode=<mode> and then each option in name order.
	BondOptions      []string
	BondPorts        []string
	DisabledServices []string
	DiskEncryption   *KickstartEncryption
	EnabledServices  []string
	ExcludeDocs      bool
	Firewall         string
	Formats          string
	Gateway          string
	HostKeyPath      string
	// HostKeyType is the keyType of the delivered or guest-generated key, which
	// decides the path it is installed at and the type it is proved to be.
	HostKeyType   string
	Hostname      string
	Interface     string
	InterfaceType string
	Keyboard      string
	Language      string
	MarkerPath    string
	Nameservers   []string
	NTPServers    []string
	Packages      []string
	PackageSource string
	Prefix        int
	Release       string
	Repositories  []Repository
	RootDevice    string
	RootSerial    string
	RootWWN       string
	SELinux       string
	Timezone      string
	User          string
	VLANBase      string
	VLANID        int
	WeakDeps      string
}

// RenderKickstart derives the complete unattended installation. The machine
// powers off when it is done, so the controller ejects the media and boots the
// installed system from disk deliberately rather than racing a reboot. It
// writes no secret: the account is authorized by a public key the adapter substitutes,
// the root account is locked, and the %post removes every retained copy of the
// file so the installed system keeps none.
func RenderKickstart(input Installation) (string, error) {
	if err := guardKickstartValues(input); err != nil {
		return "", err
	}
	// Only a disk its substrate created may be left for the installer to
	// choose; a physical one is named by its by-id WWN path.
	if input.Physical && !durableRootDevice(input.RootDevice) {
		return "", refusal("lifecycle.state", "a physical installation names no root disk by its /dev/disk/by-id/wwn-0x path to erase",
			"set spec.hardware.rootDevice.deviceName to the disk's /dev/disk/by-id/wwn-0x path; a wwn-only selection is not yet supported")
	}
	if (input.RootWWN != "" || input.RootSerial != "") && !input.Physical ||
		input.RootWWN != "" && "/dev/disk/by-id/wwn-"+input.RootWWN != input.RootDevice ||
		input.RootSerial != "" && !rootSerial.MatchString(input.RootSerial) {
		return "", refusal("lifecycle.state", "the root disk WWN or serial number this installation freezes is not one its %pre can compare with the disk its deviceName names",
			"plan the installation again with this release")
	}
	if _, known := deliveredHostKeys[input.HostKeyType]; input.Channel == substrate.ChannelDeliveredKey && !known {
		return "", refusal("lifecycle.state", "the delivered SSH host key type "+input.HostKeyType+" has no installed path",
			"plan the installation again with this release")
	}
	if _, known := guestHostKeys[input.HostKeyType]; input.Channel == substrate.ChannelGuestAgent && !known {
		return "", refusal("lifecycle.state", "the guest-agent SSH host key type "+input.HostKeyType+" has no generated path",
			"plan the installation again with this release")
	}
	network, err := networkLine(input)
	if err != nil {
		return "", err
	}
	lines := []string{
		"# Generated by Bootwright. Do not edit.",
		"text",
		"eula --agreed",
		"poweroff",
		"",
		input.PackageSource,
		network,
		"",
		"lang " + input.Language + languageSuffix(input),
		"keyboard --vckeymap=" + input.Keyboard,
		timezoneLine(input),
		"",
		"rootpw --lock",
		"user --name=" + input.User + " --groups=wheel --lock",
		`sshkey --username=` + input.User + ` "` + AuthorizedKeyToken + `"`,
		"",
	}
	lines = append(lines, targetProofLines(input)...)
	lines = append(lines, encryptionPreLines(input)...)
	lines = append(lines, storageLines(input)...)
	lines = append(lines, "")
	lines = append(lines, securityLines(input)...)
	lines = append(lines, "", servicesLine(input), "")
	lines = append(lines, packagesSection(input)...)
	lines = append(lines, "")
	lines = append(lines, encryptionPostLines(input)...)
	lines = append(lines, postSection(input)...)
	return strings.Join(compact(lines), "\n") + "\n", nil
}

// kickstartShape is how a value is interpolated: as one token, as one element
// of a comma-joined list, as the package-source directive, as one %packages
// entry, as a repository ID that also names a file, or as one whole line.
type kickstartShape int

const (
	kickstartToken kickstartShape = iota
	kickstartListElement
	kickstartPackageSource
	kickstartPackage
	kickstartRepositoryID
	kickstartLine
)

// kickstartFields is every string an Installation carries, the value class a
// refusal names and where that value comes from. Anaconda splits the file on
// every line break Python's str.splitlines knows and tokenizes each command
// line with shlex, so admission's grammars keep each value one token and this
// guard refuses whatever reaches the renderer otherwise.
var kickstartFields = []struct {
	class, source string
	shape         kickstartShape
	read          func(Installation) []string
}{
	{"the install address", "spec.network.addresses of the Machine", kickstartToken, func(i Installation) []string { return []string{i.Address} }},
	{"an additional locale", "spec.customizations.localization.additionalLocales of the install profile", kickstartListElement, func(i Installation) []string { return i.AdditionalLocale }},
	{"the identity channel", "the Machine's substrate", kickstartToken, func(i Installation) []string { return []string{i.Channel} }},
	{"a declared hardware address", "spec.substrate.baremetal.nics of the Machine", kickstartToken, func(i Installation) []string { return i.ExpectedMACs }},
	{"the install interface type", "the type of the install interface in the Machine's NMState network configuration", kickstartToken, func(i Installation) []string { return []string{i.InterfaceType} }},
	{"the VLAN base interface", "vlan.base-iface of the install interface in the Machine's NMState network configuration", kickstartToken, func(i Installation) []string { return []string{i.VLANBase} }},
	{"a bond port", "link-aggregation.port of the install bond in the Machine's NMState network configuration", kickstartListElement, func(i Installation) []string { return i.BondPorts }},
	{"a bond option", "link-aggregation of the install bond in the Machine's NMState network configuration", kickstartToken, func(i Installation) []string { return i.BondOptions }},
	{"the TPM2 PCR bank", "spec.customizations.security.diskEncryption.unlock.tpm2.pcrBank of the install profile", kickstartToken, encryptionValue(func(e KickstartEncryption) string { return e.PCRBank })},
	{"the LUKS key derivation", "spec.customizations.security.fips.enabled of the install profile", kickstartToken, encryptionValue(func(e KickstartEncryption) string { return e.PBKDF })},
	{"a disabled service", "spec.customizations.services.disabled of the install profile", kickstartListElement, func(i Installation) []string { return i.DisabledServices }},
	{"an enabled service", "spec.customizations.services.enabled of the install profile", kickstartListElement, func(i Installation) []string { return i.EnabledServices }},
	{"the firewall choice", "spec.customizations.security.firewall.enabled of the install profile", kickstartToken, func(i Installation) []string { return []string{i.Firewall} }},
	{"the formats locale", "spec.customizations.localization.formats of the install profile", kickstartToken, func(i Installation) []string { return []string{i.Formats} }},
	{"the default gateway", "the default route's next-hop-address in the Machine's NMState network configuration", kickstartToken, func(i Installation) []string { return []string{i.Gateway} }},
	{"the host key path", "the product-owned host key path", kickstartToken, func(i Installation) []string { return []string{i.HostKeyPath} }},
	{"the delivered host key type", "spec.source.generated.keyType of the Secret spec.os.install.hostKeyRef names", kickstartToken, func(i Installation) []string { return []string{i.HostKeyType} }},
	{"the host name", "the fqdn address in spec.network.addresses of the Machine", kickstartToken, func(i Installation) []string { return []string{i.Hostname} }},
	{"the install interface", "the interface of the install address in spec.network.addresses of the Machine", kickstartToken, func(i Installation) []string { return []string{i.Interface} }},
	{"the keyboard layout", "spec.customizations.localization.keyboard of the install profile", kickstartToken, func(i Installation) []string { return []string{i.Keyboard} }},
	{"the language", "spec.customizations.localization.language of the install profile", kickstartToken, func(i Installation) []string { return []string{i.Language} }},
	{"the install marker path", "the product-owned install marker path", kickstartToken, func(i Installation) []string { return []string{i.MarkerPath} }},
	{"a name server", "the DNS server selections of the Machine's network configuration", kickstartListElement, func(i Installation) []string { return i.Nameservers }},
	{"a time source", "the NTP server selections of the install profile or of spec.os.install.ntp on the Machine", kickstartListElement, func(i Installation) []string { return i.NTPServers }},
	{"a package entry", "spec.customizations.packages.install of the install profile", kickstartPackage, func(i Installation) []string { return i.Packages }},
	{"the package source", "spec.installer.anaconda.packageSource of the install profile", kickstartPackageSource, func(i Installation) []string { return []string{i.PackageSource} }},
	{"the release", "spec.os.version of the install profile", kickstartToken, func(i Installation) []string { return []string{i.Release} }},
	{"the root device", "spec.hardware.rootDevice.deviceName of the Machine", kickstartToken, func(i Installation) []string { return []string{i.RootDevice} }},
	{"the root disk serial number", "spec.substrate.baremetal.rootDevice.serialNumber of the Machine", kickstartToken, func(i Installation) []string { return []string{i.RootSerial} }},
	{"the root disk WWN", "spec.substrate.baremetal.rootDevice.wwn of the Machine", kickstartToken, func(i Installation) []string { return []string{i.RootWWN} }},
	{"the SELinux mode", "spec.customizations.security.selinux.mode of the install profile", kickstartToken, func(i Installation) []string { return []string{i.SELinux} }},
	{"the time zone", "spec.customizations.localization.timezone of the install profile", kickstartToken, func(i Installation) []string { return []string{i.Timezone} }},
	{"the install account", "the product-owned install account", kickstartToken, func(i Installation) []string { return []string{i.User} }},
	{"the weak-dependency choice", "spec.customizations.packages.installWeakDeps of the install profile", kickstartToken, func(i Installation) []string { return []string{i.WeakDeps} }},
	{"a repository base URL", "spec.customizations.repositories.configure[].baseURL of the install profile", kickstartToken, func(i Installation) []string {
		return repositoryValues(i, func(r Repository) string { return r.BaseURL })
	}},
	{"a repository ID", "spec.customizations.repositories.configure[].id of the install profile", kickstartRepositoryID, func(i Installation) []string {
		return repositoryValues(i, func(r Repository) string { return r.ID })
	}},
	{"a repository GPG key URL", "spec.customizations.repositories.configure[].gpgKeyURL of the install profile", kickstartToken, func(i Installation) []string {
		return repositoryValues(i, func(r Repository) string { return r.GPGKeyURL })
	}},
	{"a repository name", "spec.customizations.repositories.configure[].displayName of the install profile", kickstartLine, func(i Installation) []string {
		return repositoryValues(i, func(r Repository) string { return r.Name })
	}},
	{"a repository proxy", "spec.connection of the Proxy the Machine's spec.proxy selects", kickstartToken, func(i Installation) []string {
		return repositoryValues(i, func(r Repository) string { return r.Proxy })
	}},
}

// guardKickstartValues refuses any value that is not what its directive
// carries. Every rendering function stays untouched, so a valid installation
// renders the same bytes it always did.
func guardKickstartValues(input Installation) error {
	for _, field := range kickstartFields {
		for _, value := range field.read(input) {
			if value == "" && kickstartRequired(field.shape) {
				return refusal("api.value", field.class+" is empty, which its Kickstart directive cannot carry",
					"correct "+field.source)
			}
			if !kickstartCarries(field.shape, value) {
				return refusal("api.value", field.class+" holds a character a Kickstart directive cannot carry",
					"correct "+field.source)
			}
		}
	}
	return nil
}

// kickstartRequired is a shape whose directive renders nothing, or something
// else, when its value is empty: the package source, a %packages entry and a
// repository ID, which names its section and its file.
func kickstartRequired(shape kickstartShape) bool {
	return shape == kickstartPackageSource || shape == kickstartPackage || shape == kickstartRepositoryID
}

func kickstartCarries(shape kickstartShape, value string) bool {
	if !utf8.ValidString(value) || strings.IndexFunc(value, kickstartLineBreak) >= 0 {
		return false
	}
	if value == "" {
		return !kickstartRequired(shape)
	}
	switch shape {
	case kickstartPackageSource:
		tree, found := strings.CutPrefix(value, "url --url=")
		return value == "cdrom" || found && tree != "" && oneKickstartToken(tree, false)
	case kickstartListElement:
		return oneKickstartToken(value, true)
	case kickstartPackage:
		return !strings.HasPrefix(value, "-") && oneKickstartToken(value, false)
	case kickstartRepositoryID:
		return managedos.RepositoryID(value)
	case kickstartLine:
		return true
	}
	return oneKickstartToken(value, false)
}

// kickstartLineBreak is any rune that ends a line or carries no meaning in
// one: every control character, including NEL, and the Unicode line and
// paragraph separators str.splitlines also splits on.
func kickstartLineBreak(r rune) bool {
	return unicode.IsControl(r) || r == '\u2028' || r == '\u2029'
}

// kickstartQuoted reports whether value can stand between the double quotes
// a directive gives it: one line, with no quote that ends it and no
// backslash that escapes one.
func kickstartQuoted(value string) bool {
	return utf8.ValidString(value) && strings.IndexFunc(value, kickstartLineBreak) < 0 && !strings.ContainsAny(value, `"\`)
}

// oneKickstartToken refuses what shlex reads as a separator, a quote, an
// escape or a comment, and a leading '%', which opens or closes a section.
func oneKickstartToken(value string, listed bool) bool {
	separators := `"'\#`
	if listed {
		separators += ","
	}
	return !strings.HasPrefix(value, "%") && !strings.ContainsAny(value, separators) && strings.IndexFunc(value, unicode.IsSpace) < 0
}

func repositoryValues(input Installation, read func(Repository) string) []string {
	values := make([]string, 0, len(input.Repositories))
	for _, repository := range input.Repositories {
		values = append(values, read(repository))
	}
	return values
}

// networkLine is the one static assignment the installer applies, from the
// Machine's own selected install address and its network's default route.
func networkLine(input Installation) (string, error) {
	address, err := netip.ParseAddr(input.Address)
	if err != nil || !address.Is4() {
		return "", refusal("api.value", "the Machine's install address is not an IPv4 literal", "correct the selected install address")
	}
	if input.Prefix < 1 || input.Prefix > 32 {
		return "", refusal("api.value", "the Machine's install address carries no usable prefix", "correct the selected install address")
	}
	fields, err := interfaceFields(input)
	if err != nil {
		return "", err
	}
	fields = append(fields, "--ip="+input.Address, "--netmask="+netmask(input.Prefix),
		"--hostname="+input.Hostname, "--onboot=on", "--activate")
	if input.Gateway != "" {
		fields = append(fields, "--gateway="+input.Gateway)
	}
	if len(input.Nameservers) != 0 {
		fields = append(fields, "--nameserver="+strings.Join(input.Nameservers, ","))
	}
	return strings.Join(fields, " "), nil
}

// interfaceFields name the install interface: a VLAN by its base device, and
// a bond, or the bond a VLAN is over, with its ports and options.
func interfaceFields(input Installation) ([]string, error) {
	kind, vlan := input.InterfaceType, input.InterfaceType == "vlan"
	if kind != "ethernet" && kind != "bond" && !vlan {
		return nil, refusal("lifecycle.state", "the install interface type "+kind+" is not ethernet, bond or vlan", "plan the installation again with this release")
	}
	bonded := kind == "bond" || vlan && len(input.BondPorts) != 0
	if vlan && (input.VLANBase == "" || input.VLANID < 1 || input.VLANID > 4094) ||
		bonded && (len(input.BondPorts) == 0 || len(input.BondOptions) == 0 || !strings.HasPrefix(input.BondOptions[0], "mode=")) {
		return nil, refusal("lifecycle.state", "the install interface "+input.Interface+" carries an incomplete bond or VLAN", "plan the installation again with this release")
	}
	device := input.Interface
	if vlan {
		device = input.VLANBase
	}
	fields := []string{"network", "--bootproto=static", "--device=" + device}
	if bonded {
		separator := ","
		if slices.ContainsFunc(input.BondOptions, func(option string) bool { return strings.Contains(option, ",") }) {
			separator = ";"
		}
		fields = append(fields, "--bondslaves="+strings.Join(input.BondPorts, ","), "--bondopts="+strings.Join(input.BondOptions, separator))
	}
	if vlan {
		fields = append(fields, "--vlanid="+strconv.Itoa(input.VLANID), "--interfacename="+input.Interface)
	}
	return fields, nil
}

func netmask(prefix int) string {
	bits := ^uint32(0) << (32 - prefix)
	parts := make([]string, 0, 4)
	for shift := 24; shift >= 0; shift -= 8 {
		parts = append(parts, strconv.Itoa(int(bits>>uint(shift)&0xff)))
	}
	return strings.Join(parts, ".")
}

func languageSuffix(input Installation) string {
	if len(input.AdditionalLocale) == 0 {
		return ""
	}
	return " --addsupport=" + strings.Join(input.AdditionalLocale, ",")
}

func timezoneLine(input Installation) string {
	line := "timezone " + input.Timezone + " --utc"
	if len(input.NTPServers) != 0 {
		line += " --ntpservers=" + strings.Join(input.NTPServers, ",")
	}
	return line
}

// storageLines clear and partition exactly the named device, or leave the
// installer to choose when a Machine its substrate created names none.
func storageLines(input Installation) []string {
	autopart := "autopart --type=lvm"
	if input.DiskEncryption != nil {
		autopart = "%include " + storageInclude
	}
	if input.RootDevice == "" {
		return []string{"clearpart --all --initlabel", autopart}
	}
	device := strings.TrimPrefix(input.RootDevice, "/dev/")
	return []string{
		"ignoredisk --only-use=" + device,
		"clearpart --all --initlabel --drives=" + device,
		autopart,
	}
}

func securityLines(input Installation) []string {
	var lines []string
	if input.SELinux != "" {
		lines = append(lines, "selinux --"+input.SELinux)
	}
	if input.Firewall != "" {
		lines = append(lines, "firewall --"+input.Firewall)
	}
	return lines
}

func servicesLine(input Installation) string {
	fields := []string{"services"}
	if len(input.EnabledServices) != 0 {
		fields = append(fields, "--enabled="+strings.Join(input.EnabledServices, ","))
	}
	if len(input.DisabledServices) != 0 {
		fields = append(fields, "--disabled="+strings.Join(input.DisabledServices, ","))
	}
	if len(fields) == 1 {
		return ""
	}
	return strings.Join(fields, " ")
}

func packagesSection(input Installation) []string {
	header := "%packages"
	if input.ExcludeDocs {
		header += " --excludedocs"
	}
	if input.WeakDeps != "" {
		header += " " + input.WeakDeps
	}
	lines := []string{header, "@^minimal-environment"}
	lines = append(lines, input.Packages...)
	return append(lines, "%end")
}

// postSection writes the install marker, the regional formats, the configured
// repositories, the account's passwordless escalation, the daemon policy and
// the guest agent's RPC filter, then removes every retained copy of the
// Kickstart so the installed system keeps none.
func postSection(input Installation) []string {
	lines := []string{
		"%post --erroronfail",
		"set -eu",
		"install -d -m 0755 " + parentOf(input.MarkerPath),
		"cat > " + input.MarkerPath + " <<'BOOTWRIGHT_MARKER_EOF'",
		MarkerToken,
		"BOOTWRIGHT_MARKER_EOF",
		"chmod 0444 " + input.MarkerPath,
	}
	lines = append(lines, identityLines(input)...)
	lines = append(lines, localeLines(input)...)
	for _, repository := range input.Repositories {
		lines = append(lines, repositoryFileLines(repository)...)
	}
	lines = append(lines, releaseVariableLines(input.Release)...)
	lines = append(lines, []string{
		"install -d -m 0750 /etc/sudoers.d",
		"printf '%s\\n' '" + input.User + " ALL=(ALL) NOPASSWD: ALL' > /etc/sudoers.d/60-bootwright",
		"chmod 0440 /etc/sudoers.d/60-bootwright",
		"install -d -m 0755 /etc/ssh/sshd_config.d",
		"printf '%s\\n' 'PasswordAuthentication no' 'PermitRootLogin prohibit-password' > /etc/ssh/sshd_config.d/60-bootwright.conf",
		"chmod 0600 /etc/ssh/sshd_config.d/60-bootwright.conf",
	}...)
	return append(lines,
		"rm -f /root/anaconda-ks.cfg /root/original-ks.cfg /run/install/ks.cfg",
		"%end",
	)
}

// formatCategories are the locale categories the formats locale sets, the set
// the regional-formats choice covers on an installed RHEL system.
var formatCategories = []string{
	"LC_TIME", "LC_NUMERIC", "LC_MONETARY", "LC_PAPER", "LC_MEASUREMENT",
	"LC_ADDRESS", "LC_TELEPHONE", "LC_NAME", "LC_IDENTIFICATION",
}

// localeLines write the regional formats beside the language, because the lang
// directive carries the language alone. Formats equal to the language set
// nothing the language did not already set.
func localeLines(input Installation) []string {
	if input.Formats == "" || input.Formats == input.Language {
		return nil
	}
	lines := []string{"cat > /etc/locale.conf <<'BOOTWRIGHT_LOCALE_EOF'", "LANG=" + input.Language}
	for _, category := range formatCategories {
		lines = append(lines, category+"="+input.Formats)
	}
	return append(lines, "BOOTWRIGHT_LOCALE_EOF", "chmod 0644 /etc/locale.conf")
}

// repositoryFileLines write one configured repository as the installed
// system's own .repo file. A disabled repository is written disabled, so it is
// configured but not used.
func repositoryFileLines(repository Repository) []string {
	path := "'/etc/yum.repos.d/bootwright-" + repository.ID + ".repo'"
	lines := []string{
		"cat > " + path + " <<'BOOTWRIGHT_REPOSITORY_EOF'",
		"[" + repository.ID + "]",
		"name=" + repository.Name,
		"baseurl=" + repository.BaseURL,
		"enabled=" + repoFlag(repository.Enabled),
		"gpgcheck=" + repoFlag(repository.GPGCheck),
	}
	if repository.GPGKeyURL != "" {
		lines = append(lines, "gpgkey="+repository.GPGKeyURL)
	}
	if repository.Proxy != "" {
		lines = append(lines, "proxy="+repository.Proxy)
	}
	return append(lines, "BOOTWRIGHT_REPOSITORY_EOF", "chmod 0644 "+path)
}

func releaseVariableLines(release string) []string {
	if release == "" {
		return nil
	}
	return []string{
		"install -d -m 0755 /etc/dnf/vars",
		"printf '%s\\n' '" + release + "' > " + ReleaseVariablePath,
		"chown root:root " + ReleaseVariablePath,
		"chmod 0644 " + ReleaseVariablePath,
	}
}

func repoFlag(value bool) string {
	if value {
		return "1"
	}
	return "0"
}

// identityLines establish whatever the machine's own identity channel needs in
// order to answer once the installation is over. A guest agent reads files the
// installation wrote, so the key is generated and republished where the agent
// may reach it and the agent is permitted those reads; a delivered key is
// installed as sshd's own at its type's path before any key is generated, so
// the machine presents exactly the key that was frozen with the plan.
func identityLines(input Installation) []string {
	if input.Channel == substrate.ChannelDeliveredKey {
		return deliveredKeyLines(input)
	}
	if input.Channel != substrate.ChannelGuestAgent {
		return nil
	}
	key := deliveredHostKeys[input.HostKeyType]
	var lines []string
	if generation := guestHostKeys[input.HostKeyType]; generation != "" {
		lines = append(lines, "test -e "+key.path+" || /usr/bin/ssh-keygen -q "+generation+" -N '' -f "+key.path)
	}
	lines = append(lines,
		"/usr/bin/ssh-keygen -A",
		"install -m 0444 "+key.path+".pub "+input.HostKeyPath,
		"test -s "+input.HostKeyPath,
	)
	if guestHostKeys[input.HostKeyType] != "" {
		lines = append(lines, `test "$(cut -d' ' -f1 `+input.HostKeyPath+`)" = '`+key.algorithm+`'`)
	}
	return append(lines, agentFilterLines()...)
}

// deliveredHostKey is where sshd reads a host key of one generated type, and
// the algorithm name its public half begins with.
type deliveredHostKey struct {
	path, algorithm string
}

// deliveredHostKeys are the generated SSH key types an installation can
// deliver, each at the path sshd's own generation gives that type.
var deliveredHostKeys = map[string]deliveredHostKey{
	"ed25519":   {path: "/etc/ssh/ssh_host_ed25519_key", algorithm: "ssh-ed25519"},
	"rsa":       {path: "/etc/ssh/ssh_host_rsa_key", algorithm: "ssh-rsa"},
	"ecdsaP256": {path: "/etc/ssh/ssh_host_ecdsa_key", algorithm: "ecdsa-sha2-nistp256"},
	"ecdsaP384": {path: "/etc/ssh/ssh_host_ecdsa_key", algorithm: "ecdsa-sha2-nistp384"},
	"ecdsaP521": {path: "/etc/ssh/ssh_host_ecdsa_key", algorithm: "ecdsa-sha2-nistp521"},
}

// deliveredHostKeyDropIn names the delivered key as sshd's host key. Naming
// any HostKey replaces sshd's default list, so the machine presents the frozen
// key and no other it generated.
const deliveredHostKeyDropIn = "/etc/ssh/sshd_config.d/40-bootwright-host-key.conf"

// deliveredKeyLines install the key pair this installation was given as the
// machine's own, at its type's path and before any key is generated, so
// `ssh-keygen -A` adds only the types it did not receive. The pair is proved
// to be of the frozen type with halves that match, and the drop-in names it,
// so the machine presents exactly the key the plan froze whichever type that
// is. The fetch verifies the server's certificate rather than disabling
// verification, because the confidentiality of the path depends on it, and the
// material is removed from the installer environment as soon as it is placed.
func deliveredKeyLines(input Installation) []string {
	key := deliveredHostKeys[input.HostKeyType]
	return []string{
		"install -d -m 0755 /etc/ssh",
		"cat > /tmp/bootwright-artifact-ca.pem <<'BOOTWRIGHT_ARTIFACT_CA_EOF'",
		CertificateToken,
		"BOOTWRIGHT_ARTIFACT_CA_EOF",
		"curl --fail --silent --show-error --cacert /tmp/bootwright-artifact-ca.pem" +
			" --output " + key.path + " '" + PrivateURLToken + "/" + IdentityFile + "'",
		"curl --fail --silent --show-error --cacert /tmp/bootwright-artifact-ca.pem" +
			" --output " + key.path + ".pub '" + PrivateURLToken + "/" + IdentityFile + ".pub'",
		"chmod 0600 " + key.path,
		"chmod 0644 " + key.path + ".pub",
		"test -s " + key.path,
		`test "$(cut -d' ' -f1 ` + key.path + `.pub)" = '` + key.algorithm + `'`,
		`test "$(/usr/bin/ssh-keygen -y -f ` + key.path + ` | cut -d' ' -f1,2)" = "$(cut -d' ' -f1,2 ` + key.path + `.pub)"`,
		"install -d -m 0755 /etc/ssh/sshd_config.d",
		"printf '%s\\n' 'HostKey " + key.path + "' > " + deliveredHostKeyDropIn,
		"chmod 0600 " + deliveredHostKeyDropIn,
		"/usr/bin/ssh-keygen -A",
		"shred -u /tmp/bootwright-artifact-ca.pem 2>/dev/null || rm -f /tmp/bootwright-artifact-ca.pem",
	}
}

// targetProofLines repeat the controller-side target proof on the machine that
// is actually running the installer: exactly the declared MAC set over the
// device-backed ports, a USB CDC or RNDIS management host interface excepted,
// and a whole root disk. It is the last point at which the wrong server can
// still be stopped, runs before any storage is touched and fails closed; no
// authorization relaxes it, because `data-loss` acknowledges a loss rather
// than selecting what to lose.
func targetProofLines(input Installation) []string {
	if !input.Physical {
		return nil
	}
	lines := []string{
		"%pre --erroronfail --interpreter=/bin/bash",
		"set -euo pipefail",
		`bootwright_observed=$(for bootwright_nic in /sys/class/net/*; do`,
		`if [ ! -e "${bootwright_nic}/device" ]; then continue; fi`,
		`case "$(basename -- "$(readlink -f -- "${bootwright_nic}/device/driver")")" in cdc_ether|cdc_eem|cdc_ncm|rndis_host) continue;; esac`,
		`bootwright_mac="${bootwright_nic}/address"`,
		`if [ -r "${bootwright_nic}/bonding_slave/perm_hwaddr" ]; then bootwright_mac="${bootwright_nic}/bonding_slave/perm_hwaddr"; fi`,
		`tr 'A-Z' 'a-z' < "${bootwright_mac}" || exit 1`,
		`done | sort -u)`,
	}
	if len(input.ExpectedMACs) == 0 {
		lines = append(lines,
			`echo 'Bootwright: this machine declares no hardware address to prove it by; refusing before any disk is touched.' >&2`,
			"exit 1")
	}
	excluded := []string{}
	for _, address := range input.ExpectedMACs {
		excluded = append(excluded, "-e '"+address+"'")
		lines = append(lines,
			`if ! printf '%s\n' "${bootwright_observed}" | grep -Fqx '`+address+`'; then`,
			`echo 'Bootwright: this machine does not report `+address+
				`, which its declaration requires; refusing before any disk is touched.' >&2`,
			"exit 1",
			"fi")
	}
	if len(excluded) != 0 {
		lines = append(lines,
			`bootwright_undeclared=$(printf '%s\n' "${bootwright_observed}" | grep -Fvx `+strings.Join(excluded, " ")+` | grep -c . || true)`,
			`if [ "${bootwright_undeclared}" != 0 ]; then`,
			`echo "Bootwright: this machine reports ${bootwright_undeclared} hardware address(es) its declaration does not name; refusing before any disk is touched." >&2`,
			"exit 1",
			"fi")
	}
	lines = append(lines,
		`bootwright_root=$(readlink -f -- '`+input.RootDevice+`' || true)`,
		`if [ -z "${bootwright_root}" ] || [ ! -b "${bootwright_root}" ]; then`,
		`echo 'Bootwright: the declared root device `+input.RootDevice+
			` is not a block device here; refusing before any disk is touched.' >&2`,
		"exit 1",
		"fi",
		`if [ "$(lsblk -ndo TYPE -- "${bootwright_root}")" != disk ]; then`,
		`echo 'Bootwright: the declared root device `+input.RootDevice+
			` is not a whole disk; refusing before any disk is touched.' >&2`,
		"exit 1",
		"fi")
	lines = append(lines, rootIdentityLines(input)...)
	return append(lines, "%end", "")
}

// rootIdentityLines read sysfs as the physical erase worker does; unreadable is a mismatch.
func rootIdentityLines(input Installation) []string {
	if input.RootWWN == "" && input.RootSerial == "" {
		return nil
	}
	refuse := func(what string) []string {
		return []string{`echo 'Bootwright: the declared root device ` + input.RootDevice + ` ` + what + `; refusing before any disk is touched.' >&2`, "exit 1", "fi"}
	}
	lines := []string{
		`bootwright_identity() {`,
		`local raw value='' byte start=0 end limit=259 form="$2"`,
		`local -a bytes`,
		`if [ "${form}" = vpd ]; then limit=261; fi`,
		`raw=$(od -An -v -tu1 -N "${limit}" -- "$1") || return 1`,
		`read -r -a bytes <<< "${raw//$'\n'/ }"`,
		`set -- "${bytes[@]}"`,
		`if [ "${form}" = vpd ]; then`,
		`if [ "$#" -lt 5 ] || [ "$#" -gt 260 ] || [ "${bytes[1]}" != 128 ] || [ $((bytes[2] * 256 + bytes[3])) != $(($# - 4)) ]; then return 1; fi`,
		`bytes=("${bytes[@]:4}")`,
		`shift 4`,
		`elif [ "$#" -gt 258 ]; then return 1`,
		`fi`,
		`end=$#`,
		`while [ "${start}" -lt "${end}" ] && [[ " 9 10 11 12 13 32 " == *" ${bytes[start]} "* ]]; do start=$((start + 1)); done`,
		`while [ "${end}" -gt "${start}" ] && [[ " 9 10 11 12 13 32 " == *" ${bytes[end - 1]} "* ]]; do end=$((end - 1)); done`,
		`if [ "${end}" -eq "${start}" ] || [ $((end - start)) -gt 256 ]; then return 1; fi`,
		`for byte in "${bytes[@]:start:end - start}"; do`,
		`if [ "${byte}" -lt 32 ] || [ "${byte}" -gt 126 ]; then return 1; fi`,
		`printf -v byte '%03o' "${byte}"`,
		`printf -v byte "\\${byte}"`,
		`value+="${byte}"`,
		`done`,
		`printf '%s\n' "${value}"`,
		`}`,
		`bootwright_disk="/sys/class/block/$(basename -- "${bootwright_root}")"`,
	}
	if input.RootWWN != "" {
		lines = append(lines,
			`bootwright_wwid="${bootwright_disk}/device/wwid"`,
			`if [ -e "${bootwright_disk}/wwid" ]; then bootwright_wwid="${bootwright_disk}/wwid"; fi`,
			`if ! bootwright_wwid=$(bootwright_identity "${bootwright_wwid}" text); then`)
		lines = append(lines, refuse("reports no readable WWN")...)
		lines = append(lines, `if [ "${bootwright_wwid}" != 'naa.`+strings.TrimPrefix(input.RootWWN, "0x")+`' ]; then`)
		lines = append(lines, refuse("reports a WWN other than "+input.RootWWN)...)
	}
	if input.RootSerial != "" {
		lines = append(lines,
			`bootwright_serial="${bootwright_disk}/device/vpd_pg80" bootwright_form=vpd`,
			`if [ -e "${bootwright_disk}/device/serial" ]; then bootwright_serial="${bootwright_disk}/device/serial" bootwright_form=text; fi`,
			`if ! bootwright_serial=$(bootwright_identity "${bootwright_serial}" "${bootwright_form}"); then`)
		lines = append(lines, refuse("reports no readable serial number")...)
		lines = append(lines, `if [ "${bootwright_serial}" != '`+input.RootSerial+`' ]; then`)
		lines = append(lines, refuse("reports a serial number other than "+input.RootSerial)...)
	}
	return lines
}

// agentFilterLines permit exactly the identity operation's reads and prove the
// edit took, so a release that spells the filter differently fails the
// installation rather than leaving a guest that can never prove completion.
func agentFilterLines() []string {
	return []string{
		`if grep -q '^FILTER_RPC_ARGS=.*--allow-rpcs=' ` + agentFilter + `; then`,
		`sed -i '/^FILTER_RPC_ARGS=/s/--allow-rpcs=/--allow-rpcs=` + identityRPCs + `,/' ` + agentFilter,
		`grep -q '^FILTER_RPC_ARGS=.*--allow-rpcs=` + identityRPCs + `,' ` + agentFilter,
		`elif grep -q '^FILTER_RPC_ARGS=.*--block-rpcs=' ` + agentFilter + `; then`,
		`sed -i '/^FILTER_RPC_ARGS=/s/guest-file-open,//;/^FILTER_RPC_ARGS=/s/guest-file-close,//;/^FILTER_RPC_ARGS=/s/guest-file-read,//' ` + agentFilter,
		`! grep -q '^FILTER_RPC_ARGS=.*guest-file-open' ` + agentFilter,
		`fi`,
	}
}

func parentOf(path string) string {
	index := strings.LastIndex(path, "/")
	if index <= 0 {
		return "/"
	}
	return path[:index]
}

// compact removes the empty strings an omitted directive leaves behind, and
// collapses the blank lines around them, so the rendered file is deterministic.
func compact(lines []string) []string {
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		if line == "" && (len(out) == 0 || out[len(out)-1] == "") {
			continue
		}
		out = append(out, line)
	}
	for len(out) != 0 && out[len(out)-1] == "" {
		out = out[:len(out)-1]
	}
	return out
}

// SortedUnique is the canonical order every derived list carries, so one
// revision always renders one file.
func SortedUnique(values []string) []string {
	out := slices.Clone(values)
	slices.Sort(out)
	return slices.Compact(out)
}
